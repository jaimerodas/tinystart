# syntax=docker/dockerfile:1
# check=error=true

# The Go image. It is go.Dockerfile while Rails still owns Dockerfile; at
# cutover it is renamed to Dockerfile and the Rails one is deleted.
#
# Not "Dockerfile.go": anything ending in .go is a Go source file to the
# toolchain, and gofmt, go vet and go build all choke on the first '#'.
#
#   docker build -f go.Dockerfile -t tinystart-go .
#   docker run -d -p 3996:80 --name tinystart-go tinystart-go

# 1.26 rather than a patch release: go.mod's toolchain directive pins the exact
# compiler, and the go command in this image downloads it if the image is
# behind. That keeps the pin in one place, next to the code.
ARG GO_VERSION=1.26
FROM docker.io/library/golang:${GO_VERSION}-bookworm AS build

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
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" \
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
USER 1000:1000

COPY --from=build /usr/local/bin/tinystart /usr/local/bin/tinystart

# Port 80 because that is where kamal-proxy connects, and binding it as a
# non-root user works because Docker sets net.ipv4.ip_unprivileged_port_start=0
# inside containers. The Rails image relied on the same thing.
ENV TINYSTART_ADDR=:80
EXPOSE 80

CMD ["/usr/local/bin/tinystart"]
