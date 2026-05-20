// Package backends unifies the local (uv) and Modal compute backends behind a
// single interface, so design tools never branch on which one is in use.
package backends

import (
	"context"
	"fmt"
	"io"

	"github.com/alvarogonjim/fova/internal/backends/local"
	"github.com/alvarogonjim/fova/internal/backends/modal"
)

// Backend runs a protein tool with a JSON request and returns its JSON output.
// The same input yields the same output schema regardless of implementation
// (the backend-symmetry guarantee, SPECS §13.2). It also streams the tool's
// live stdout+stderr to log (the job's log file, or io.Discard) and emits
// fractional 0..1 progress ticks via progress as it crosses stage boundaries.
// log and progress may be nil; implementations default to io.Discard and a
// no-op respectively.
type Backend interface {
	Name() string
	Run(ctx context.Context, tool string, input []byte, log io.Writer, progress func(float64)) ([]byte, error)
}

// localBackend runs design tools via per-tool adapters (local.RunDesign).
type localBackend struct{ registry *local.Registry }

func (b *localBackend) Name() string { return "local" }

func (b *localBackend) Run(ctx context.Context, tool string, input []byte, log io.Writer, progress func(float64)) ([]byte, error) {
	if log == nil {
		log = io.Discard
	}
	// RunDesign streams live stdout+stderr to log and emits per-stage
	// progress ticks; no end-of-call write is needed.
	return local.RunDesign(ctx, b.registry, tool, input, log, progress)
}

// modalBackend runs tools via the SP4 Modal client.
type modalBackend struct{ client *modal.Client }

func (b *modalBackend) Name() string { return "modal" }

func (b *modalBackend) Run(ctx context.Context, tool string, input []byte, log io.Writer, progress func(float64)) ([]byte, error) {
	if log == nil {
		log = io.Discard
	}
	tick := progress
	if tick == nil {
		tick = func(float64) {}
	}
	fmt.Fprintf(log, "modal: dispatching %s\n", tool)
	tick(0.05)
	out, err := b.client.Run(ctx, tool, input)
	if err != nil {
		fmt.Fprintf(log, "modal: %s failed: %v\n", tool, err)
		return nil, err
	}
	tick(0.9)
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
