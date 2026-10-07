module github.com/jaimerodas/tinystart

go 1.27.0

// Pinned to a patch release, not just 1.27, because govulncheck in
// script/test reports every standard library CVE the toolchain is behind on.
// Bumping this line is how those get fixed.
toolchain go1.27.1

// Development tools, pinned here so `go run` uses a known version and nothing
// has to be installed globally. Neither one is linked into the binary.
tool (
	golang.org/x/vuln/cmd/govulncheck
	honnef.co/go/tools/cmd/staticcheck
)

// The app links three dependencies. chromedp and cdproto are test-only.
//
// Since Go 1.27, `go mod tidy` keeps one block for direct dependencies and one
// for indirect ones. A comment on a line stays with that line, so each reason
// is on its own line.
require (
	// Test-only. chromedp drives the browser suite in
	// internal/web/browser_*_test.go, which is behind //go:build browser. The
	// app does not import it, so the binary does not link it.
	github.com/chromedp/cdproto v0.157.9
	github.com/chromedp/chromedp v0.20.1
	// The maintained continuation of gopkg.in/yaml.v3. It reads and writes the
	// start page interchange format.
	go.yaml.in/yaml/v3 v3.0.5
	// For bcrypt, which verifies the $2a$ digests that Rails wrote, unchanged.
	golang.org/x/crypto v0.57.0
	// The pure-Go SQLite. It is why the image can be a static binary with no
	// libc.
	modernc.org/sqlite v1.60.1
)

// The dependencies of the modules above and of the two tools. To find which
// module pulls one in, run `go mod why -m <module>`.
require (
	github.com/BurntSushi/toml v1.6.0 // indirect
	github.com/dustin/go-humanize v1.1.0 // indirect
	github.com/go-json-experiment/json v0.0.0-20260820222146-c27c302e5fc3 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/mattn/go-isatty v0.0.24 // indirect
	github.com/ncruces/go-strftime v1.1.0 // indirect
	github.com/remyoudompheng/bigfft v0.0.0-20230129092748-24d4a6f8daec // indirect
	golang.org/x/exp/typeparams v0.0.0-20261007192929-f45ad48fbe92 // indirect
	golang.org/x/mod v0.41.0 // indirect
	golang.org/x/sync v0.23.0 // indirect
	golang.org/x/sys v0.48.0 // indirect
	golang.org/x/telemetry v0.0.0-20260924152758-ed294f943157 // indirect
	golang.org/x/tools v0.51.0 // indirect
	golang.org/x/vuln v1.8.0 // indirect
	honnef.co/go/tools v0.8.1 // indirect
	modernc.org/libc v1.77.1 // indirect
	modernc.org/mathutil v1.7.1 // indirect
	modernc.org/memory v1.12.1 // indirect
)
