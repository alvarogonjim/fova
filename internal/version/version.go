// Package version exposes the fova build version.
package version

// version is the fova build version. Override it at link time:
//
//	go build -ldflags="-X github.com/alvarogonjim/fova/internal/version.version=v1.2.3"
var version = "0.5.0"

// String returns the current fova version.
func String() string { return version }
