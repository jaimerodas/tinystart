module github.com/jaimerodas/tinystart

go 1.26.0

// Pinned to a patch release, not just 1.26, because govulncheck in
// script/test reports every standard library CVE the toolchain is behind on.
// Bumping this line is how those get fixed.
toolchain go1.26.7

// Development tools, pinned here so `go run` uses a known version and nothing
// has to be installed globally. Neither one is linked into the binary.
tool (
	golang.org/x/vuln/cmd/govulncheck
	honnef.co/go/tools/cmd/staticcheck
)

// The tools' own dependencies.
require (
	github.com/BurntSushi/toml v1.6.0 // indirect
	golang.org/x/exp/typeparams v0.0.0-20260820142414-ca536658362e // indirect
	golang.org/x/mod v0.40.0 // indirect
	golang.org/x/sync v0.22.0 // indirect
	golang.org/x/sys v0.47.0 // indirect
	golang.org/x/telemetry v0.0.0-20260821201142-16c97dc83219 // indirect
	golang.org/x/tools v0.49.0 // indirect
	golang.org/x/vuln v1.7.0 // indirect
	honnef.co/go/tools v0.8.1 // indirect
)

// The app's dependencies. modernc.org/sqlite is the pure-Go SQLite, which is
// what lets the image be a static binary with no libc; bcrypt is in
// golang.org/x/crypto and verifies the $2a$ digests Rails wrote, unchanged;
// go.yaml.in/yaml/v3 is the maintained continuation of gopkg.in/yaml.v3 and
// reads and writes the start page interchange format.
require (
	go.yaml.in/yaml/v3 v3.0.5
	golang.org/x/crypto v0.55.0
	modernc.org/sqlite v1.57.0
)

// Pulled in by modernc.org/sqlite.
require (
	github.com/dustin/go-humanize v1.0.1 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/mattn/go-isatty v0.0.24 // indirect
	github.com/ncruces/go-strftime v1.0.0 // indirect
	github.com/remyoudompheng/bigfft v0.0.0-20230129092748-24d4a6f8daec // indirect
	modernc.org/libc v1.75.4 // indirect
	modernc.org/mathutil v1.7.1 // indirect
	modernc.org/memory v1.12.1 // indirect
)

// Test-only, and the only dependency that is: chromedp drives the browser
// suite in internal/web/browser_*_test.go, which is behind //go:build browser.
// Nothing here is imported by the app, so the binary still links exactly the
// three dependencies above.
require (
	github.com/chromedp/cdproto v0.0.0-20260804232424-e85f50dbfd32
	github.com/chromedp/chromedp v0.16.0
)

// Pulled in by chromedp, and test-only for the same reason.
require (
	github.com/chromedp/sysutil v1.1.0 // indirect
	github.com/go-json-experiment/json v0.0.0-20260820222146-c27c302e5fc3 // indirect
	github.com/gobwas/httphead v0.1.0 // indirect
	github.com/gobwas/pool v0.2.1 // indirect
	github.com/gobwas/ws v1.4.0 // indirect
)
