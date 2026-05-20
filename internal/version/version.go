// Package version exposes the Proteus build version.
package version

// version is the Proteus build version. Override it at link time:
//
//	go build -ldflags="-X github.com/alvarogonjim/proteus/internal/version.version=v1.2.3"
var version = "0.1.0-dev"

// String returns the current Proteus version.
func String() string { return version }
