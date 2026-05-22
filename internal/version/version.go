// Package version exposes the build-time version constants.
// Set via -ldflags at build time; defaults useful for `go run` during development.
package version

var (
	Version   = "dev"
	Commit    = "unknown"
	BuildDate = "unknown"
)

// String formats the version line shown by `mm --version` / `mm version`.
func String() string {
	if Commit == "unknown" && BuildDate == "unknown" {
		return "mm " + Version
	}
	return "mm " + Version + " (" + Commit + ", " + BuildDate + ")"
}
