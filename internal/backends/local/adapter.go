package local

import (
	"context"
	"fmt"
	"os"
)

// ToolAdapter runs one design tool on the local backend: it turns an agent
// design request into a real tool invocation and the tool's native output into
// the {"designs":[...]} JSON the design tools expect back.
type ToolAdapter interface {
	AgentTool() string // e.g. "design.proteinmpnn"
	Recipe() string    // e.g. "proteinmpnn" — the tools.toml recipe name
	Invoke(ctx context.Context, env AdapterEnv, request []byte) ([]byte, error)
}

// AdapterEnv is everything an adapter needs to run. It is injected so adapters
// are unit-testable with a stub Run and a temporary WorkDir.
type AdapterEnv struct {
	Recipe   ToolRecipe // resolved recipe — InstallDir and VenvDir are expanded
	Run      CmdRunner  // command runner (production: bashRunner; tests: a stub)
	WorkDir  string     // a fresh temp directory the adapter may write into
	Registry *Registry  // for DataAsset lookups and Home()
}

// designOut is one design in the {"designs":[...]} envelope adapters return;
// it mirrors the schema internal/tools/design expects back from a backend.
type designOut struct {
	Sequence      map[string]string  `json:"sequence"`
	StructureFile string             `json:"structure_file"`
	Scores        map[string]float64 `json:"scores"`
}

// designsEnvelope is the top-level {"designs":[...]} JSON adapters return.
type designsEnvelope struct {
	Designs []designOut `json:"designs"`
}

// adapterRegistry maps agent tool name -> adapter. Adapters register themselves
// via registerAdapter from an init function in their own file.
var adapterRegistry = map[string]ToolAdapter{}

// registerAdapter adds an adapter to the registry.
func registerAdapter(a ToolAdapter) { adapterRegistry[a.AgentTool()] = a }

// RunDesign runs the local adapter for a design tool. It looks up the adapter,
// resolves its recipe, creates a temp WorkDir (removed on return), and invokes
// it. A design tool with no registered adapter yields a clear error.
func RunDesign(ctx context.Context, reg *Registry, agentTool string, request []byte) ([]byte, error) {
	adapter, ok := adapterRegistry[agentTool]
	if !ok {
		return nil, fmt.Errorf("%s: no local adapter on this backend yet", agentTool)
	}
	rec, ok := reg.Tool(adapter.Recipe())
	if !ok {
		return nil, fmt.Errorf("%s: recipe %q is not in the tool registry", agentTool, adapter.Recipe())
	}
	workDir, err := os.MkdirTemp("", "proteus-design-*")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(workDir)
	return adapter.Invoke(ctx, AdapterEnv{Recipe: rec, Run: bashRunner, WorkDir: workDir, Registry: reg}, request)
}
