module github.com/jaimerodas/tinystart

go 1.26

// Pinned to a patch release, not just 1.26, because govulncheck in
// script/test reports every standard library CVE the toolchain is behind on.
// Bumping this line is how those get fixed.
toolchain go1.26.6

// Development tools, pinned here so `go run` uses a known version and nothing
// has to be installed globally. Neither one is linked into the binary.
tool (
	golang.org/x/vuln/cmd/govulncheck
	honnef.co/go/tools/cmd/staticcheck
)

// All indirect, all pulled in by the two tools above: the app itself has no
// dependencies yet.
require (
	github.com/BurntSushi/toml v1.4.1-0.20240526193622-a339e1f7089c // indirect
	golang.org/x/exp/typeparams v0.0.0-20231108232855-2478ac86f678 // indirect
	golang.org/x/mod v0.39.0 // indirect
	golang.org/x/sync v0.22.0 // indirect
	golang.org/x/sys v0.47.0 // indirect
	golang.org/x/telemetry v0.0.0-20260811182544-a038080d80e5 // indirect
	golang.org/x/tools v0.49.0 // indirect
	golang.org/x/vuln v1.7.0 // indirect
	honnef.co/go/tools v0.7.0 // indirect
)
