// Package backends unifies the local (uv) and Modal compute backends behind a
// single interface, so design tools never branch on which one is in use.
package backends

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/alvarogonjim/proteus/internal/backends/local"
	"github.com/alvarogonjim/proteus/internal/backends/modal"
)

// Backend runs a protein tool with a JSON request and returns its JSON output.
// The same input yields the same output schema regardless of implementation
// (the backend-symmetry guarantee, SPECS §13.2).
type Backend interface {
	Name() string
	Run(ctx context.Context, tool string, input []byte) ([]byte, error)
}

// localBackend runs tools via the SP3 uv-managed local runner.
type localBackend struct{ runner *local.Runner }

func (b *localBackend) Name() string { return "local" }

func (b *localBackend) Run(ctx context.Context, tool string, input []byte) ([]byte, error) {
	dir, err := os.MkdirTemp("", "proteus-run-*")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)
	reqPath := filepath.Join(dir, "request.json")
	if err := os.WriteFile(reqPath, input, 0o644); err != nil {
		return nil, err
	}
	// The recipes use different placeholder names for the request file; fill
	// every common one to the same path (unused names are simply ignored).
	out, err := b.runner.Run(ctx, tool, map[string]string{
		"input_json":  reqPath,
		"args_file":   reqPath,
		"input_yaml":  reqPath,
		"input_fasta": reqPath,
		"out_dir":     dir,
	})
	return []byte(out), err
}

// modalBackend runs tools via the SP4 Modal client.
type modalBackend struct{ client *modal.Client }

func (b *modalBackend) Name() string { return "modal" }

func (b *modalBackend) Run(ctx context.Context, tool string, input []byte) ([]byte, error) {
	out, err := b.client.Run(ctx, tool, input)
	return []byte(out), err
}

// Select returns the backend named by computeBackend. "" and "local" select
// the local backend; "modal" selects the Modal backend.
func Select(computeBackend, home string) (Backend, error) {
	switch computeBackend {
	case "", "local":
		reg, err := local.LoadRegistry(home)
		if err != nil {
			return nil, err
		}
		return &localBackend{runner: local.NewRunner(reg)}, nil
	case "modal":
		return &modalBackend{client: modal.NewClientFromEnv()}, nil
	default:
		return nil, fmt.Errorf("unknown compute backend %q", computeBackend)
	}
}
