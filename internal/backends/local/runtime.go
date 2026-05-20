package local

import (
	"os/exec"
)

// RuntimeKind identifies which container runtime is in use.
type RuntimeKind int

const (
	// RuntimeNone means neither Podman nor Docker was found.
	RuntimeNone RuntimeKind = iota
	// RuntimePodman is preferred (rootless, daemonless).
	RuntimePodman
	// RuntimeDocker is the fallback.
	RuntimeDocker
)

// String returns "podman", "docker", or "none".
func (k RuntimeKind) String() string {
	switch k {
	case RuntimePodman:
		return "podman"
	case RuntimeDocker:
		return "docker"
	default:
		return "none"
	}
}

// Runtime describes the container runtime fova will use to install and run
// tools. Detect populates it at startup.
type Runtime struct {
	Bin  string      // absolute path to the runtime binary, "" when Kind == RuntimeNone
	Kind RuntimeKind // which runtime was selected
}

// lookPath is the exec.LookPath seam — tests override it to simulate which
// binaries are on PATH.
var lookPath = exec.LookPath

// Detect probes the host for a container runtime. Podman wins when both are
// present (rootless + daemonless is a better default for a personal box).
// Returns a zero Runtime with Kind == RuntimeNone when neither is available;
// the caller decides how to surface that (typically via /doctor).
func Detect() Runtime {
	if path, err := lookPath("podman"); err == nil {
		return Runtime{Bin: path, Kind: RuntimePodman}
	}
	if path, err := lookPath("docker"); err == nil {
		return Runtime{Bin: path, Kind: RuntimeDocker}
	}
	return Runtime{Kind: RuntimeNone}
}

// Available reports whether some runtime was detected.
func (r Runtime) Available() bool { return r.Kind != RuntimeNone }
