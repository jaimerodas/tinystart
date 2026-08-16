# syntax=docker/dockerfile:1
# check=error=true

# The production image: a static Go binary on a slim Debian, deployed by Kamal
# (config/deploy.yml). Build and run it by hand with
#
#   docker build -t tinystart .
#   docker run -d -p 3996:80 -e TINYSTART_SECRET_KEY=$(openssl rand -hex 32) \
#     -v tinystart_dev:/data --name tinystart tinystart

# 1.26 rather than a patch release: go.mod's toolchain directive pins the exact
# compiler, and the go command in this image downloads it if the image is
# behind. That keeps the pin in one place, next to the code.
# --platform=$BUILDPLATFORM: the build stage runs natively on whatever builds
# the image (an arm64 laptop, through Kamal) and cross-compiles for the target
# (the amd64 droplet). Go cross-compiles for free; without this line buildx
# would run the whole Go toolchain under QEMU emulation instead, and a build
# that takes seconds takes minutes.
ARG GO_VERSION=1.26
FROM --platform=$BUILDPLATFORM docker.io/library/golang:${GO_VERSION}-bookworm AS build
ARG TARGETOS TARGETARCH

WORKDIR /src

# go.mod and go.sum on their own first: this layer is then only rebuilt when
# the dependencies change, not on every edit to the source. It also pulls the
# staticcheck and govulncheck tool dependencies, which the binary does not
# need — a few seconds in a throw-away stage, in exchange for one obvious line.
COPY go.mod go.sum ./
RUN go mod download

COPY cmd/ cmd/
COPY internal/ internal/

# CGO_ENABLED=0 is the decision the whole image rests on: a static binary needs
# no libc at runtime, which is why the final stage can be a bare slim image and
# why the SQLite driver has to be the pure-Go one.
# -trimpath keeps build machine paths out of the binary; -s -w drop the symbol
# table and DWARF, which is most of its size and none of its behaviour.
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath -ldflags="-s -w" \
    -o /usr/local/bin/tinystart ./cmd/tinystart


# Final stage: the binary and the two things it shells out to or trusts.
FROM docker.io/library/debian:bookworm-slim

# ca-certificates: the app makes HTTPS calls to Postmark and to tinylinks, and
# a static Go binary has no system trust store to fall back on without it.
# sqlite3: bin/backup_db shells out to it for the weekly backup to B2.
RUN apt-get update -qq && \
    apt-get install --no-install-recommends -y ca-certificates sqlite3 && \
    rm -rf /var/lib/apt/lists /var/cache/apt/archives

# Same uid and gid the Rails image used, so the files already on the
# tinystart_storage volume keep an owner both images recognise — that is what
# makes `kamal rollback` after the cutover still able to open the database.
RUN groupadd --system --gid 1000 tinystart && \
    useradd tinystart --uid 1000 --gid 1000 --create-home --shell /bin/bash

# /data is where the Kamal volume lands (TINYSTART_DB=/data/production.sqlite3
# in config/deploy.yml). It has to exist in the image, owned by the user the
# binary runs as: a *new* named volume copies its mount point's ownership from
# the image, and without this line a first boot on a fresh volume finds a
# root-owned directory it cannot create the database in. The volume that is
# already in production is unaffected either way — its files and its root are
# uid 1000 from the Rails image, which is why the uid above must not change.
RUN mkdir /data && chown tinystart:tinystart /data
USER 1000:1000

COPY --from=build /usr/local/bin/tinystart /usr/local/bin/tinystart

# Port 80 because that is where kamal-proxy connects, and binding it as a
# non-root user works because Docker sets net.ipv4.ip_unprivileged_port_start=0
# inside containers. The Rails image relied on the same thing.
ENV TINYSTART_ADDR=:80
EXPOSE 80

CMD ["/usr/local/bin/tinystart"]
