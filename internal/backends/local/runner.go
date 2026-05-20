package local

import (
	"context"
	"fmt"
	"strings"
)

// Runner invokes an installed tool's run_command with runtime placeholders
// (e.g. {{ input_json }}, {{ args_file }}, {{ out_dir }}) filled in.
type Runner struct {
	registry *Registry
	run      CmdRunner
}

// NewRunner builds a runner using the production command runner.
func NewRunner(reg *Registry) *Runner {
	return &Runner{registry: reg, run: bashRunner}
}

// Run fills the recipe's run_command with the given placeholder values and
// executes it in the tool's install directory, returning its combined output.
func (r *Runner) Run(ctx context.Context, name string, placeholders map[string]string) (string, error) {
	rec, ok := r.registry.Tool(name)
	if !ok {
		return "", fmt.Errorf("unknown tool %q", name)
	}
	command := expandPlaceholders(rec.RunCommand, placeholders)
	if i := strings.Index(command, "{{"); i >= 0 {
		return "", fmt.Errorf("%s run_command has an unfilled placeholder: %s",
			name, command[i:])
	}
	return r.run(ctx, rec.InstallDir, command)
}
