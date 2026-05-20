// Package backends unifies the local (uv) and Modal compute backends behind a
// single interface, so design tools never branch on which one is in use.
package backends

import (
	"context"
	"fmt"
	"io"

	"github.com/alvarogonjim/proteus/internal/backends/local"
	"github.com/alvarogonjim/proteus/internal/backends/modal"
)

// Backend runs a protein tool with a JSON request and returns its JSON output.
// The same input yields the same output schema regardless of implementation
// (the backend-symmetry guarantee, SPECS §13.2). It also tees the tool's output
// to log (the job's log file, or io.Discard).
type Backend interface {
	Name() string
	Run(ctx context.Context, tool string, input []byte, log io.Writer) ([]byte, error)
}

// localBackend runs design tools via per-tool adapters (local.RunDesign).
type localBackend struct{ registry *local.Registry }

func (b *localBackend) Name() string { return "local" }

func (b *localBackend) Run(ctx context.Context, tool string, input []byte, log io.Writer) ([]byte, error) {
	out, err := local.RunDesign(ctx, b.registry, tool, input)
	// Tee the tool's output to the job log.
	_, _ = log.Write(out)
	return out, err
}

// modalBackend runs tools via the SP4 Modal client.
type modalBackend struct{ client *modal.Client }

func (b *modalBackend) Name() string { return "modal" }

func (b *modalBackend) Run(ctx context.Context, tool string, input []byte, log io.Writer) ([]byte, error) {
	out, err := b.client.Run(ctx, tool, input)
	// Write the returned payload to the job log.
	_, _ = log.Write([]byte(out))
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
		return &localBackend{registry: reg}, nil
	case "modal":
		return &modalBackend{client: modal.NewClientFromEnv()}, nil
	default:
		return nil, fmt.Errorf("unknown compute backend %q", computeBackend)
	}
}
