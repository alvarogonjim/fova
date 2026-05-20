package local

import (
	"context"
	"fmt"
	"os/exec"
)

// uvInstallScript is the official Astral uv installer (run via sh).
const uvInstallScript = "curl -LsSf https://astral.sh/uv/install.sh | sh"

// UVPath returns the path to the uv binary and whether it was found on PATH.
func UVPath() (string, bool) {
	path, err := exec.LookPath("uv")
	if err != nil {
		return "", false
	}
	return path, true
}

// ensureUV installs uv via the Astral installer if it is not already on PATH.
// run is the command runner (injected so tests can stub it).
func ensureUV(ctx context.Context, run CmdRunner) error {
	if _, ok := UVPath(); ok {
		return nil
	}
	if _, err := run(ctx, "", uvInstallScript); err != nil {
		return fmt.Errorf("install uv: %w", err)
	}
	if _, ok := UVPath(); !ok {
		return fmt.Errorf("uv still not on PATH after running the installer")
	}
	return nil
}
