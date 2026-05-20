# Proteus — Protein Design Agent TUI

**Spec version:** 0.3
**Status:** Implementation-ready
**Target implementer:** Claude Code (vibe coding)
**Author:** Alvaro
**Date:** May 2026

## Changelog

- **v0.3** — ipSAE added as the primary interface ranking metric; local backend redesigned around uv with a Proteus-managed installer (`proteus install <tool>`); replaced conda assumptions throughout.
- **v0.2** — Free-by-default knowledge stack (Europe PMC, OpenAlex, S2, bioRxiv, Crossref); per-project corpus; Paperclip moved to optional. Implementation-ready structure.
- **v0.1** — Initial draft.

---

## How to read this spec

This document is the **authoritative source of truth** for building Proteus. It is written to be implementable by Claude Code without further design decisions. Where a choice has been made, the spec says so and doesn't relitigate it. Where a choice is deferred, it is in §22 (Open Questions) and the v1 path picks a default.

**For Claude Code specifically:**
- Every file path is given relative to the repo root.
- Every external dependency is pinned to a known-good version.
- Every API endpoint is given as a literal URL.
- Every Go type is declared in the spec; copy them verbatim.
- Each milestone in §20 has explicit acceptance criteria. Don't move on until they pass.
- When unsure, prefer the simpler implementation and add a `// TODO(spec):` comment referencing the spec section.

---

## 1. Project Summary

Proteus is a terminal user interface (TUI) agent for de novo protein design. It orchestrates LLM-driven planning, GPU-bound design and prediction tools, free literature retrieval, and wet-lab validation via Adaptyv Bio's Foundry API.

### 1.1 Goals

- A single, polished, distributable Go binary.
- First-class support for **binders**, **antibodies**, and **enzymes**, using methods with documented wet-lab success per Listov et al., *Curr. Opin. Struct. Biol.* (2026).
- **Free by default**: works fully without any paid account. Paid LLMs and Paperclip are optional upgrades.
- Local LLMs (Ollama / vLLM / LM Studio) and paid LLMs (Anthropic / OpenAI / Google) interchangeably.
- Knowledge-grounded planning from free public APIs (Europe PMC, OpenAlex, Semantic Scholar, bioRxiv, Crossref, UniProt, RCSB PDB).
- Wet-lab integration via Adaptyv API to close the design–test–learn loop.

### 1.2 Non-goals (v1)

- No web UI. TUI only.
- No multi-user / team mode.
- No retraining of ML models. Proteus consumes them.
- No proprietary cloud backend. Modal is BYO; users run their own.

### 1.3 Target users

- Computational protein designers wanting an LLM-orchestrated workflow.
- Wet-lab biologists with light scripting experience.
- Researchers running rapid design–test cycles.

---

## 2. Design Principles

1. **Free by default.** Every feature works without an account. Paid features are opt-in.
2. **Experimentally validated tools only.** Built-in design tools have documented wet-lab success.
3. **Small filtered shortlists over large unfiltered libraries.** Modern pipelines ship ≤100 designs to the bench.
4. **Type-safe tool I/O.** The LLM sees structured outputs, never raw stdout.
5. **Human-in-the-loop checkpoints.** Confirmation before expensive operations (>5 min compute, >$1 cost, any wet-lab submission).
6. **Local-first, cloud-elastic.** Works offline with local Ollama; scales out to Modal for GPU.
7. **Provenance everywhere.** Every design carries full lineage from intent to wet-lab result.
8. **Beautiful by default.** Terminal output is a product surface.

---

## 3. Technology Stack

### 3.1 Language and runtime

- **Go 1.22+**
- Single static binary distribution.

### 3.2 Dependencies (pinned)

```go
// go.mod (excerpt)
require (
    github.com/charmbracelet/bubbletea v0.27.0
    github.com/charmbracelet/bubbles v0.20.0
    github.com/charmbracelet/lipgloss v0.13.0
    github.com/charmbracelet/glamour v0.8.0
    github.com/anthropics/anthropic-sdk-go v0.3.0
    github.com/openai/openai-go v0.1.0-beta.6
    github.com/go-resty/resty/v2 v2.14.0
    github.com/go-chi/chi/v5 v5.1.0
    github.com/spf13/cobra v1.8.1
    github.com/knadh/koanf/v2 v2.1.1
    github.com/rs/zerolog v1.33.0
    modernc.org/sqlite v1.32.0
    github.com/blevesearch/bleve/v2 v2.4.2
    github.com/99designs/keyring v1.2.2
    github.com/biogo/biogo v1.0.4
    github.com/google/uuid v1.6.0
)
```

**Notes for Claude Code:**
- `modernc.org/sqlite` is pure-Go (no CGo) — keeps the binary truly static.
- `bleve` is the Go-native full-text search engine; we use it for the local literature corpus.
- Anthropic and OpenAI SDKs both support tool use natively. Local LLMs (Ollama / vLLM / LM Studio) speak OpenAI-compatible APIs, so we reuse `openai-go` with a custom `BaseURL` for them.

### 3.3 Repository layout

```
proteus/
├── go.mod
├── go.sum
├── README.md
├── LICENSE                       # Apache-2.0
├── Makefile
├── cmd/
│   └── proteus/
│       └── main.go               # entrypoint; cobra root command
├── internal/
│   ├── tui/                      # Bubble Tea
│   │   ├── app.go                # main model
│   │   ├── chat.go               # chat pane
│   │   ├── jobs.go               # jobs panel
│   │   ├── designs.go            # designs panel
│   │   ├── lab.go                # wet-lab panel
│   │   ├── statusbar.go
│   │   ├── commandbar.go
│   │   ├── modal.go              # confirmation modals
│   │   ├── theme.go              # lipgloss styles
│   │   └── graphics.go           # Kitty/Sixel inline images
│   ├── agent/
│   │   ├── loop.go               # ReAct loop
│   │   ├── session.go            # message history + compaction
│   │   ├── steering.go           # mid-turn user input
│   │   └── prompts/
│   │       ├── system.md         # base system prompt
│   │       └── compaction.md     # compaction prompt
│   ├── llm/
│   │   ├── provider.go           # Provider interface
│   │   ├── anthropic.go
│   │   ├── openai.go             # also covers Ollama/vLLM/LM Studio via BaseURL
│   │   ├── google.go
│   │   └── registry.go           # active model registry
│   ├── tools/
│   │   ├── registry.go           # tool registration + dispatch
│   │   ├── schema.go             # JSON schema generation
│   │   ├── fs.go                 # read, write, edit, bash
│   │   ├── fold/
│   │   │   ├── esmfold.go
│   │   │   ├── colabfold.go
│   │   │   ├── boltz2.go
│   │   │   └── chai1.go
│   │   ├── design/
│   │   │   ├── bindcraft.go
│   │   │   ├── rfdiffusion.go
│   │   │   ├── proteinmpnn.go
│   │   │   ├── rfantibody.go
│   │   │   ├── rfdiffusion2.go
│   │   │   └── ligandmpnn.go
│   │   ├── score/
│   │   │   ├── metrics.go
│   │   │   ├── ipsae.go          # primary interface metric (calls vendored ipsae.py via uv)
│   │   │   ├── filter.go
│   │   │   ├── rosetta.go
│   │   │   ├── foldx.go
│   │   │   ├── esm_perplexity.go
│   │   │   └── solubility.go
│   │   ├── knowledge/
│   │   │   ├── europepmc.go
│   │   │   ├── openalex.go
│   │   │   ├── s2.go             # Semantic Scholar
│   │   │   ├── biorxiv.go
│   │   │   ├── crossref.go
│   │   │   ├── uniprot.go
│   │   │   ├── pdb.go
│   │   │   ├── interpro.go
│   │   │   ├── blast.go
│   │   │   ├── web_search.go
│   │   │   ├── web_fetch.go
│   │   │   ├── corpus.go         # per-project local corpus (bleve)
│   │   │   ├── local_pdfs.go     # opt-in user PDF folder
│   │   │   └── paperclip.go      # OPTIONAL: MCP integration
│   │   ├── lab/
│   │   │   ├── adaptyv.go        # Foundry API client
│   │   │   └── webhook.go        # webhook receiver
│   │   ├── viz/
│   │   │   ├── pymol.go
│   │   │   ├── contact_map.go
│   │   │   ├── metric_plot.go
│   │   │   └── ascii_structure.go
│   │   └── jobs/
│   │       ├── list.go
│   │       ├── status.go
│   │       ├── cancel.go
│   │       └── result.go
│   ├── backends/
│   │   ├── local/
│   │   │   ├── installer.go      # uv-based tool installer
│   │   │   ├── registry.go       # parses tools.toml
│   │   │   ├── tools.toml        # embedded; install recipes for each tool
│   │   │   ├── runner.go         # invokes installed tools (uv run)
│   │   │   └── uv.go             # ensure uv is installed
│   │   ├── modal/
│   │   │   ├── client.go
│   │   │   └── functions.py      # deployed by user via `proteus modal deploy`
│   │   └── hosted/
│   │       ├── esm_atlas.go
│   │       └── bionemo.go
│   ├── store/
│   │   ├── schema.sql            # SQLite DDL
│   │   ├── store.go              # connection + migrations
│   │   ├── designs.go
│   │   ├── jobs.go
│   │   ├── sessions.go
│   │   ├── plans.go
│   │   └── experiments.go
│   ├── domain/
│   │   ├── types.go              # core domain types
│   │   ├── sequence.go           # sequence validation
│   │   ├── structure.go          # PDB/mmCIF helpers
│   │   └── scores.go             # score schemas
│   ├── skills/
│   │   ├── loader.go             # discovers and loads skills
│   │   ├── builtin/              # embedded built-in skills
│   │   │   ├── design-binder.md
│   │   │   ├── design-antibody.md
│   │   │   ├── design-enzyme.md
│   │   │   ├── redesign-stability.md
│   │   │   ├── filter-thresholds.md
│   │   │   ├── plan-from-target.md
│   │   │   ├── submit-to-adaptyv.md
│   │   │   ├── interpret-wetlab.md
│   │   │   ├── close-the-loop.md
│   │   │   └── biosecurity.md
│   ├── config/
│   │   ├── config.go             # koanf-based loader
│   │   └── defaults.go
│   ├── credentials/
│   │   └── keyring.go            # OS keychain wrapper
│   ├── cost/
│   │   └── tracker.go            # per-session cost accounting
│   └── version/
│       └── version.go
├── pkg/                          # publicly importable packages
│   └── proteinio/
│       ├── fasta.go
│       ├── pdb.go
│       └── mmcif.go
├── scripts/
│   ├── install.sh
│   └── release.sh
├── eval/
│   ├── biodesignbench/           # eval harness (later milestone)
│   └── README.md
└── docs/
    ├── architecture.md
    ├── tools.md
    ├── skills.md
    └── adaptyv.md
```

### 3.4 Build and run

```makefile
# Makefile
.PHONY: build test run install lint

build:
	go build -ldflags='-s -w' -o bin/proteus ./cmd/proteus

run: build
	./bin/proteus

test:
	go test ./...

lint:
	golangci-lint run

install: build
	install -m 0755 bin/proteus /usr/local/bin/proteus
```

---

## 4. Domain Types

Implement these verbatim in `internal/domain/types.go`. All other code depends on them.

```go
package domain

import "time"

// --- Identifiers ---

type DesignID string
type PlanID string
type JobID string
type SessionID string
type ProjectID string
type ExperimentID string

// --- Application areas ---

type Application string

const (
    AppBinder   Application = "binder"
    AppAntibody Application = "antibody"
    AppEnzyme   Application = "enzyme"
    AppRedesign Application = "redesign"
)

// --- Tool / job kinds ---

type JobKind string

const (
    JobCompute JobKind = "compute"
    JobLab     JobKind = "lab"
)

type JobStatus string

const (
    JobQueued    JobStatus = "queued"
    JobRunning   JobStatus = "running"
    JobSucceeded JobStatus = "succeeded"
    JobFailed    JobStatus = "failed"
    JobCancelled JobStatus = "cancelled"
)

// --- Sequence and structure ---

type Sequence struct {
    Chains map[string]string `json:"chains"` // chain ID → AA sequence
}

type ResidueRef struct {
    Chain   string `json:"chain"`
    Position int   `json:"position"`
    AA      string `json:"aa,omitempty"`
}

type PDBReference struct {
    PDBID    string       `json:"pdb_id,omitempty"`
    FilePath string       `json:"file_path,omitempty"`
    URL      string       `json:"url,omitempty"`
    Chain    string       `json:"chain,omitempty"`
    Epitope  []ResidueRef `json:"epitope,omitempty"`
}

// --- Design ---

type Design struct {
    ID            DesignID            `json:"id"`
    ProjectID     ProjectID           `json:"project_id"`
    PlanID        PlanID              `json:"plan_id"`
    Created       time.Time           `json:"created"`
    Origin        DesignOrigin        `json:"origin"`
    Application   Application         `json:"application"`
    Sequence      Sequence            `json:"sequence"`
    StructureFile string              `json:"structure_file,omitempty"`
    Scores        map[string]float64  `json:"scores"`
    LabResults    []ExperimentResult  `json:"lab_results,omitempty"`
    Provenance    []ToolCallRef       `json:"provenance"`
    Tags          []string            `json:"tags,omitempty"`
    Notes         string              `json:"notes,omitempty"`
}

type DesignOrigin string

const (
    OriginBindCraft     DesignOrigin = "bindcraft"
    OriginRFDiffMPNN    DesignOrigin = "rfdiff_mpnn"
    OriginRFAntibody    DesignOrigin = "rfantibody"
    OriginChai2         DesignOrigin = "chai2"
    OriginRFDiff2MPNN   DesignOrigin = "rfdiff2_ligandmpnn"
    OriginManual        DesignOrigin = "manual"
)

type ToolCallRef struct {
    CallID     string    `json:"call_id"`
    Tool       string    `json:"tool"`
    InputHash  string    `json:"input_hash"`
    Version    string    `json:"version"`
    Timestamp  time.Time `json:"timestamp"`
}

// --- Scoring ---

type FilterConfig struct {
    // Confidence (per-residue / global)
    MinPLDDT         float64 `json:"min_plddt,omitempty"`         // default 80 (binder chain mean)
    MinPLDDTMin      float64 `json:"min_plddt_min,omitempty"`     // default 60 (per-residue floor)

    // Interface metrics — ipSAE is preferred per recent literature
    MinIPSAE         float64 `json:"min_ipsae,omitempty"`         // default 0.50 (ipSAE_min between chains)
    MaxPAEInterface  float64 `json:"max_pae_interface,omitempty"` // default 10  (legacy, kept for AF2 compat)
    MinIPTM          float64 `json:"min_iptm,omitempty"`          // default 0.8 (legacy, kept for AF2 compat)
    MinPDockQ        float64 `json:"min_pdockq,omitempty"`        // optional

    // Geometry
    MaxRMSDtoModel   float64 `json:"max_rmsd_to_model,omitempty"` // default 2.0 Å (self-consistency)
    MaxMotifRMSD     float64 `json:"max_motif_rmsd,omitempty"`    // default 1.0 Å for enzymes

    // Physics / sequence quality
    MinRosettaScore  float64 `json:"min_rosetta_score,omitempty"` // ΔΔG REU (more negative = better)
    MaxESMPerplexity float64 `json:"max_esm_perplexity,omitempty"`// default 5.0
}

// Recommended scoring order for ranking shortlists (highest to lowest priority):
//   1. ipSAE  (best correlation with experimental binding per Dunbrack 2025)
//   2. pAE_interaction  (when ipSAE unavailable, e.g. older AF2 outputs)
//   3. ipTM  (legacy; biased by chain length)
//   4. pDockQ / pDockQ2
//   5. Rosetta ΔΔG (when physics check is desired)

type DesignScore struct {
    DesignID   DesignID            `json:"design_id"`
    Metrics    map[string]float64  `json:"metrics"`
}

// --- Plan ---

type DesignPlan struct {
    ID              PlanID         `json:"id"`
    ProjectID       ProjectID      `json:"project_id"`
    Created         time.Time      `json:"created"`
    Target          PDBReference   `json:"target"`
    Application     Application    `json:"application"`
    Method          string         `json:"method"`           // primary tool name
    FallbackMethod  string         `json:"fallback_method,omitempty"`
    Filters         FilterConfig   `json:"filters"`
    ShortlistSize   int            `json:"shortlist_size"`
    ComputeBackend  string         `json:"compute_backend"`
    EstimatedCost   float64        `json:"estimated_cost_usd"`
    EstimatedTime   string         `json:"estimated_time"`
    Rationale       string         `json:"rationale"`        // why this plan
    EvidencePapers  []PaperRef     `json:"evidence_papers,omitempty"`
    Approved        bool           `json:"approved"`
    ApprovedAt      *time.Time     `json:"approved_at,omitempty"`
}

type PaperRef struct {
    DOI    string `json:"doi,omitempty"`
    PMCID  string `json:"pmcid,omitempty"`
    Title  string `json:"title"`
    Year   int    `json:"year"`
    URL    string `json:"url,omitempty"`
}

// --- Job ---

type Job struct {
    ID         JobID       `json:"id"`
    Kind       JobKind     `json:"kind"`
    Tool       string      `json:"tool"`
    Status     JobStatus   `json:"status"`
    Created    time.Time   `json:"created"`
    Started    *time.Time  `json:"started,omitempty"`
    Finished   *time.Time  `json:"finished,omitempty"`
    Progress   float64     `json:"progress"`              // 0..1
    Backend    string      `json:"backend"`               // local | modal | hosted | adaptyv
    CostUSD    float64     `json:"cost_usd"`
    Input      []byte      `json:"input"`                 // JSON-encoded request
    Output     []byte      `json:"output,omitempty"`      // JSON-encoded result
    Error      string      `json:"error,omitempty"`
    ProducedDesigns []DesignID `json:"produced_designs,omitempty"`
}

// --- Experiment (wet-lab) ---

type Experiment struct {
    ID            ExperimentID        `json:"id"`
    ProjectID     ProjectID           `json:"project_id"`
    Backend       string              `json:"backend"`         // "adaptyv"
    ExternalID    string              `json:"external_id"`     // Adaptyv experiment_id
    AssayType     string              `json:"assay_type"`      // binding | thermostability | expression
    TargetID      string              `json:"target_id"`
    TargetName    string              `json:"target_name"`
    Designs       []DesignID          `json:"designs"`
    SubmittedAt   time.Time           `json:"submitted_at"`
    Status        string              `json:"status"`
    CostUSD       float64             `json:"cost_usd"`
    Results       []ExperimentResult  `json:"results,omitempty"`
}

type ExperimentResult struct {
    DesignID        DesignID  `json:"design_id"`
    Kd              *float64  `json:"kd,omitempty"`           // molar
    KdUnits         string    `json:"kd_units,omitempty"`
    Kon             *float64  `json:"kon,omitempty"`
    Koff            *float64  `json:"koff,omitempty"`
    BindingStrength string    `json:"binding_strength,omitempty"` // strong | weak | no_binding
    RSquared        *float64  `json:"r_squared,omitempty"`
    NReplicates     int       `json:"n_replicates,omitempty"`
    IsControl       bool      `json:"is_control"`
}

// --- Session and messages ---

type Session struct {
    ID         SessionID    `json:"id"`
    ProjectID  ProjectID    `json:"project_id"`
    Created    time.Time    `json:"created"`
    Updated    time.Time    `json:"updated"`
    Model      string       `json:"model"`
    Provider   string       `json:"provider"`
}

type Message struct {
    ID         string          `json:"id"`
    SessionID  SessionID       `json:"session_id"`
    Role       string          `json:"role"`         // user | assistant | tool | system
    Content    string          `json:"content"`
    ToolCalls  []ToolCall      `json:"tool_calls,omitempty"`
    ToolCallID string          `json:"tool_call_id,omitempty"`  // for role=tool
    Created    time.Time       `json:"created"`
    Tokens     int             `json:"tokens"`
    CostUSD    float64         `json:"cost_usd"`
}

type ToolCall struct {
    ID    string          `json:"id"`
    Name  string          `json:"name"`
    Input json.RawMessage `json:"input"`
}
```

---

## 5. SQLite Schema

`internal/store/schema.sql` — applied at startup via embedded migrations.

```sql
CREATE TABLE IF NOT EXISTS projects (
    id          TEXT PRIMARY KEY,
    name        TEXT NOT NULL,
    created     TEXT NOT NULL,
    workspace   TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS sessions (
    id          TEXT PRIMARY KEY,
    project_id  TEXT NOT NULL REFERENCES projects(id),
    created     TEXT NOT NULL,
    updated     TEXT NOT NULL,
    model       TEXT NOT NULL,
    provider    TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS messages (
    id            TEXT PRIMARY KEY,
    session_id    TEXT NOT NULL REFERENCES sessions(id),
    role          TEXT NOT NULL,
    content       TEXT NOT NULL,
    tool_calls    TEXT,                -- JSON array
    tool_call_id  TEXT,
    created       TEXT NOT NULL,
    tokens        INTEGER NOT NULL DEFAULT 0,
    cost_usd      REAL NOT NULL DEFAULT 0
);

CREATE INDEX IF NOT EXISTS idx_messages_session ON messages(session_id, created);

CREATE TABLE IF NOT EXISTS plans (
    id          TEXT PRIMARY KEY,
    project_id  TEXT NOT NULL REFERENCES projects(id),
    created     TEXT NOT NULL,
    body        TEXT NOT NULL,         -- JSON DesignPlan
    approved    INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE IF NOT EXISTS designs (
    id          TEXT PRIMARY KEY,
    project_id  TEXT NOT NULL REFERENCES projects(id),
    plan_id     TEXT REFERENCES plans(id),
    created     TEXT NOT NULL,
    body        TEXT NOT NULL          -- JSON Design
);

CREATE INDEX IF NOT EXISTS idx_designs_project ON designs(project_id, created);

CREATE TABLE IF NOT EXISTS jobs (
    id          TEXT PRIMARY KEY,
    project_id  TEXT NOT NULL REFERENCES projects(id),
    kind        TEXT NOT NULL,
    tool        TEXT NOT NULL,
    status      TEXT NOT NULL,
    created     TEXT NOT NULL,
    started     TEXT,
    finished    TEXT,
    progress    REAL NOT NULL DEFAULT 0,
    backend     TEXT NOT NULL,
    cost_usd    REAL NOT NULL DEFAULT 0,
    input       TEXT NOT NULL,         -- JSON
    output      TEXT,                  -- JSON
    error       TEXT
);

CREATE INDEX IF NOT EXISTS idx_jobs_status ON jobs(status, created);

CREATE TABLE IF NOT EXISTS experiments (
    id           TEXT PRIMARY KEY,
    project_id   TEXT NOT NULL REFERENCES projects(id),
    backend      TEXT NOT NULL,
    external_id  TEXT NOT NULL,
    assay_type   TEXT NOT NULL,
    target_id    TEXT NOT NULL,
    target_name  TEXT NOT NULL,
    submitted    TEXT NOT NULL,
    status       TEXT NOT NULL,
    cost_usd     REAL NOT NULL DEFAULT 0,
    body         TEXT NOT NULL         -- JSON Experiment
);

CREATE TABLE IF NOT EXISTS webhook_events (
    id           TEXT PRIMARY KEY,
    received     TEXT NOT NULL,
    source       TEXT NOT NULL,
    signature    TEXT,
    payload      TEXT NOT NULL,
    processed    INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE IF NOT EXISTS corpus_papers (
    id           TEXT PRIMARY KEY,     -- doi or pmcid
    project_id   TEXT NOT NULL REFERENCES projects(id),
    title        TEXT NOT NULL,
    authors      TEXT,
    year         INTEGER,
    source       TEXT NOT NULL,        -- europepmc | openalex | s2 | biorxiv | local
    full_text    TEXT,                 -- if available
    metadata     TEXT NOT NULL,        -- JSON
    added        TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_corpus_project ON corpus_papers(project_id);
```

The bleve full-text index is stored separately under `<project_workspace>/corpus.bleve/`.

---

## 6. LLM Providers

### 6.1 Interface

`internal/llm/provider.go`:

```go
package llm

import "context"

type Provider interface {
    Name() string
    Models(ctx context.Context) ([]ModelDescriptor, error)
    Chat(ctx context.Context, req ChatRequest) (*ChatResponse, error)
    StreamChat(ctx context.Context, req ChatRequest) (<-chan ChatEvent, error)
    EstimateCost(req ChatRequest, resp *ChatResponse) float64
}

type ModelDescriptor struct {
    ID            string
    DisplayName   string
    ContextTokens int
    SupportsTools bool
    InputPricePer1M  float64
    OutputPricePer1M float64
}

type ChatRequest struct {
    Model       string
    System      string
    Messages    []Message
    Tools       []ToolSpec
    Temperature float32
    MaxTokens   int
}

type Message struct {
    Role       string       // user | assistant | tool
    Content    string
    ToolCalls  []ToolCall
    ToolCallID string
}

type ToolCall struct {
    ID    string
    Name  string
    Input map[string]any
}

type ToolSpec struct {
    Name        string
    Description string
    InputSchema map[string]any   // JSON Schema
}

type ChatResponse struct {
    Text       string
    ToolCalls  []ToolCall
    Usage      Usage
    StopReason string
}

type Usage struct {
    InputTokens  int
    OutputTokens int
}

type ChatEvent struct {
    Kind    string  // "text_delta" | "tool_call" | "done" | "error"
    Delta   string
    Call    *ToolCall
    Err     error
}
```

### 6.2 Concrete providers

| File | Provider | Notes |
|---|---|---|
| `anthropic.go` | Anthropic | Native tool blocks via `anthropic-sdk-go`. Default. |
| `openai.go` | OpenAI / Ollama / vLLM / LM Studio | Same `openai-go` client; `BaseURL` parameter selects backend. |
| `google.go` | Google Gemini | Via Google's Go SDK or direct REST. |

### 6.3 Local LLM configuration

A local backend is just an OpenAI-compatible endpoint. Example `models.toml`:

```toml
[[providers]]
name = "anthropic"
type = "anthropic"
api_key_env = "ANTHROPIC_API_KEY"

[[providers]]
name = "openai"
type = "openai"
api_key_env = "OPENAI_API_KEY"

[[providers]]
name = "ollama"
type = "openai_compatible"
base_url = "http://localhost:11434/v1"
api_key = "ollama"  # placeholder, Ollama ignores it

[[providers]]
name = "vllm"
type = "openai_compatible"
base_url = "http://localhost:8000/v1"

[[models]]
provider = "anthropic"
id = "claude-opus-4-7"
display = "Claude Opus 4.7"
input_price_per_1m = 15.0
output_price_per_1m = 75.0

[[models]]
provider = "anthropic"
id = "claude-sonnet-4-6"
display = "Claude Sonnet 4.6"
input_price_per_1m = 3.0
output_price_per_1m = 15.0

[[models]]
provider = "ollama"
id = "llama3.3:70b"
display = "Llama 3.3 70B (local)"
input_price_per_1m = 0
output_price_per_1m = 0
```

### 6.4 Default model selection

- Default provider: `anthropic` if `ANTHROPIC_API_KEY` is set, else `ollama` if local server is reachable, else prompt user on first run.
- Default model: `claude-opus-4-7` for paid; `llama3.3:70b` or whatever's listed first for local.
- Model is switchable mid-session via `/model`.

---

## 7. Tool Registry

### 7.1 Interface

`internal/tools/registry.go`:

```go
package tools

import (
    "context"
    "encoding/json"
)

type Tool interface {
    Name() string
    Description() string
    InputSchema() map[string]any        // JSON Schema
    Execute(ctx context.Context, input json.RawMessage) (Result, error)
    RequiresConfirmation(input json.RawMessage) bool
    EstimatedCostUSD(input json.RawMessage) float64
    EstimatedDuration(input json.RawMessage) time.Duration
}

type Result struct {
    Output     json.RawMessage   // JSON-serializable output
    Display    string            // human-readable summary for the LLM
    JobID      JobID             // set if this kicked off an async job
    Cost       float64
    Provenance ToolCallRef
}

type Registry struct {
    tools map[string]Tool
}

func (r *Registry) Register(t Tool) { ... }
func (r *Registry) Get(name string) (Tool, bool) { ... }
func (r *Registry) Specs() []llm.ToolSpec { ... }
func (r *Registry) Execute(ctx, name, input) (Result, error) { ... }
```

### 7.2 Tools required for v1

All tools listed below are mandatory unless marked `[v0.x]` (deferred to a later milestone).

#### 7.2.1 Filesystem and shell

| Name | Purpose | Confirmation? |
|---|---|---|
| `fs.read` | Read a file within the project workspace | No |
| `fs.write` | Create/overwrite a file in the project workspace | No |
| `fs.edit` | Targeted string replacement | No |
| `fs.bash` | Execute a shell command, captured output, 60s default timeout | Yes if matches denylist |

`fs.bash` runs in `$PROJECT_WORKSPACE`, with `PATH` restricted to allowlisted binaries (`ls, cat, grep, sed, awk, jq, python3, conda, git, curl, wget`). Network access allowed by default.

#### 7.2.2 Structure prediction (`fold.*`)

| Name | Backend(s) | Default backend |
|---|---|---|
| `fold.esmfold` | ESM Atlas API or local | `hosted` |
| `fold.colabfold` [v0.2] | Local or Modal | `modal` |
| `fold.boltz2` [v0.4] | Modal | `modal` |
| `fold.chai1` [v0.4] | Modal | `modal` |

**`fold.esmfold` API contract:**

```json
// Input schema
{
  "type": "object",
  "properties": {
    "sequence": { "type": "string", "pattern": "^[ACDEFGHIKLMNPQRSTVWY]+$" },
    "save_as": { "type": "string", "description": "Path within workspace" }
  },
  "required": ["sequence"]
}

// Output
{
  "design_id": "d_0001",
  "structure_file": "designs/d_0001.pdb",
  "metrics": { "plddt_mean": 87.3, "plddt_min": 42.1 },
  "elapsed_s": 12.4
}
```

**ESM Atlas endpoint:** `https://api.esmatlas.com/foldSequence/v1/pdb/` — POST raw sequence as body, returns PDB text. No auth.

#### 7.2.3 Design (`design.*`)

| Name | Application | Backend | Milestone |
|---|---|---|---|
| `design.bindcraft` | binder | Modal | v0.2 |
| `design.rfdiffusion` | binder, scaffold | Modal | v0.2 |
| `design.proteinmpnn` | sequence-from-structure | Modal | v0.2 |
| `design.rfantibody` | antibody (VHH, scFv) | Modal | v0.4 |
| `design.chai2` | antibody | Modal/hosted | v0.4 |
| `design.rfdiffusion2` | enzyme | Modal | v0.4 |
| `design.ligandmpnn` | enzyme | Modal | v0.4 |

**`design.bindcraft` input schema:**

```json
{
  "type": "object",
  "properties": {
    "target": {
      "type": "object",
      "properties": {
        "pdb_id": {"type": "string"},
        "file_path": {"type": "string"},
        "chain": {"type": "string"}
      }
    },
    "hotspots": {
      "type": "array",
      "items": {
        "type": "object",
        "properties": {
          "chain": {"type": "string"},
          "position": {"type": "integer"}
        }
      }
    },
    "length_range": {"type": "array", "items": {"type": "integer"}, "minItems": 2, "maxItems": 2},
    "num_designs": {"type": "integer", "minimum": 1, "maximum": 1000},
    "filters": { "$ref": "#/$defs/FilterConfig" }
  },
  "required": ["target", "num_designs"]
}
```

Output: a list of `Design` records, plus an async `JobID` for the Modal job. The LLM sees a summary; details are fetched via `jobs.result(JobID)`.

#### 7.2.4 Scoring (`score.*`)

| Name | Purpose | Milestone |
|---|---|---|
| `score.metrics` | Extract pLDDT / pAE / ipTM / RMSD from prediction outputs | v0.2 |
| `score.ipsae` | Compute ipSAE (interprotein Score from Aligned Errors) | v0.2 |
| `score.filter` | Apply `FilterConfig` to designs; rank by ipSAE by default | v0.2 |
| `score.rosetta` | Rosetta interface scoring (PyRosetta subprocess) | v0.3 |
| `score.foldx` | FoldX ΔΔG | v0.3 |
| `score.esm_perplexity` | ESM-2 naturalness | v0.3 |
| `score.solubility` | NetSolP / SoluProt pre-Adaptyv check | v0.4 |

**Why ipSAE.** ipSAE (Dunbrack 2025, "Rēs ipSAE loquuntur") is the modern interface
metric. Unlike ipTM, which is computed over whole chains and biased by chain length /
disorder, ipSAE focuses on the actual inter-chain interface. In a meta-analysis of 3,766
experimentally tested binders across 15 targets, ipSAE outperformed ipTM and pAE_interaction
for binder ranking. It works on AF2, AF3, Boltz-1/2, and Chai-1 outputs.

**`score.ipsae` contract:**

```json
// Input
{
  "structure_file": "designs/d_0001.pdb",
  "scores_json":    "designs/d_0001.scores.json",  // AF/Boltz/Chai scores file
  "chain_a":        "A",
  "chain_b":        "B",
  "pae_cutoff":     10,
  "plddt_cutoff":   10
}

// Output
{
  "ipsae_min":       0.72,
  "ipsae_max":       0.81,
  "ipsae_d0chn":     0.65,
  "pdockq":          0.62,
  "pdockq2":         0.55,
  "lis":             0.41,
  "n_contacts":      42,
  "interface_dist":  8.5,
  "per_residue_csv": "designs/d_0001.ipsae.residues.csv"
}
```

Implementation: vendor the reference `ipsae.py` script from the Dunbrack lab repo
under `internal/tools/score/ipsae/` and invoke via `uv run`. It is ~600 lines of
Python and has no GPU requirement, only numpy/biopython.

#### 7.2.5 Knowledge (`knowledge.*`)

**All knowledge tools are free, no-auth by default.** Paperclip is optional in v1.

| Name | Source | Auth | Milestone |
|---|---|---|---|
| `knowledge.europepmc` | Europe PMC REST | None | v0.3 |
| `knowledge.openalex` | OpenAlex API | None (email recommended) | v0.3 |
| `knowledge.s2` | Semantic Scholar Graph API | None (optional key for higher RL) | v0.3 |
| `knowledge.biorxiv` | bioRxiv/medRxiv API | None | v0.3 |
| `knowledge.crossref` | Crossref REST | None | v0.3 |
| `knowledge.uniprot` | UniProt REST | None | v0.3 |
| `knowledge.pdb` | RCSB Data API | None | v0.3 |
| `knowledge.interpro` | InterPro API | None | v0.3 |
| `knowledge.blast` | NCBI BLAST URL API | None | v0.5 |
| `knowledge.web_search` | Tavily or Brave (user-provided key) | Optional | v0.3 |
| `knowledge.web_fetch` | Direct HTTP | None | v0.3 |
| `knowledge.corpus` | Per-project bleve index | None | v0.3 |
| `knowledge.local_pdfs` | User folder of PDFs | None | v0.5 |
| `knowledge.paperclip` | Paperclip MCP | User account required | v0.5 (optional) |

**Free-tier API endpoints:**

```
Europe PMC search:    GET https://www.ebi.ac.uk/europepmc/webservices/rest/search?query=...&format=json
Europe PMC fulltext:  GET https://www.ebi.ac.uk/europepmc/webservices/rest/{source}/{id}/fullTextXML
OpenAlex works:       GET https://api.openalex.org/works?search=...&mailto=user@example.com
Semantic Scholar:     GET https://api.semanticscholar.org/graph/v1/paper/search?query=...
bioRxiv recent:       GET https://api.biorxiv.org/details/biorxiv/{date_from}/{date_to}
Crossref works:       GET https://api.crossref.org/works?query=...
UniProt entry:        GET https://rest.uniprot.org/uniprotkb/{accession}.json
RCSB entry:           GET https://data.rcsb.org/rest/v1/core/entry/{pdb_id}
InterPro entry:       GET https://www.ebi.ac.uk/interpro/api/entry/InterPro/protein/uniprot/{accession}
```

**Set User-Agent on every request:** `Proteus/0.1 (https://github.com/<user>/proteus; <email>)`.

#### 7.2.6 The per-project corpus pattern

This is the key replacement for Paperclip. Implement carefully — it's what gives Proteus stateful planning.

`knowledge.corpus` exposes these sub-commands:

```
corpus.add        # add papers (by ID list or from a search result) to the project corpus
corpus.search     # full-text search within the corpus (bleve)
corpus.grep       # regex search within the corpus
corpus.map        # run an LLM prompt over each paper, return per-paper results
corpus.reduce     # synthesize across map results
corpus.list       # list papers currently in the corpus
corpus.read       # read full text of one paper
corpus.remove     # remove paper(s)
```

Implementation:
- Index lives at `<project_workspace>/corpus.bleve/`.
- Papers stored in `corpus_papers` SQLite table; full text in the `full_text` column.
- `corpus.add` accepts `paper_ids` (DOIs or PMCIDs), fetches full text via Europe PMC first, OpenAlex second, S2 third (whichever finds it).
- `corpus.map` does parallel LLM calls (configurable concurrency, default 5).
- `corpus.search` results return a stable `results_id` that subsequent `grep`/`map` can scope to via a `--from` parameter (modeled after Paperclip's `from`).

Schema:

```go
type CorpusAddInput struct {
    PaperIDs []string `json:"paper_ids"`       // DOIs, PMCIDs, or arXiv IDs
    FromSearch string `json:"from_search,omitempty"`  // alternatively, "all results from search S1"
    MaxPapers int    `json:"max_papers,omitempty"`    // default 30
}

type CorpusMapInput struct {
    Prompt    string `json:"prompt"`            // per-paper question
    From      string `json:"from,omitempty"`    // results_id from a prior search
    PaperIDs  []string `json:"paper_ids,omitempty"`
    Concurrency int  `json:"concurrency,omitempty"`  // default 5
}

type CorpusMapResult struct {
    PerPaper []struct {
        PaperID string `json:"paper_id"`
        Answer  string `json:"answer"`
    } `json:"per_paper"`
}
```

#### 7.2.7 Visualization (`viz.*`)

| Name | Output | Milestone |
|---|---|---|
| `viz.pymol_render` | PNG of a structure | v0.5 (requires PyMOL installed) |
| `viz.contact_map` | PNG of inter-chain contacts | v0.5 |
| `viz.metric_plot` | PNG of score distributions | v0.3 |
| `viz.ascii_structure` | ASCII secondary structure | v0.5 |

Images embedded inline via Kitty/Sixel; fallback to a file path the user can open.

#### 7.2.8 Wet-lab (`lab.*`)

| Name | Purpose | Confirmation? |
|---|---|---|
| `lab.targets_search` | Browse Adaptyv target catalog | No |
| `lab.cost_estimate` | Pre-flight cost | No |
| `lab.submit_experiment` | Submit sequences | **Yes (mandatory modal)** |
| `lab.experiment_status` | Poll status | No |
| `lab.results` | Retrieve kinetic data | No |

**Adaptyv API base:** `https://foundry-api-public.adaptyvbio.com/api/v1`
**Auth:** `Authorization: Bearer ${ADAPTYV_API_TOKEN}` (stored in OS keychain).
**OpenAPI spec:** `https://foundry-api-public.adaptyvbio.com/api/v1/openapi.json` — fetch and generate Go types from it at build time using `oapi-codegen`.

**Submission flow:**

1. Agent calls `lab.cost_estimate({target_id, assay_type, sequences})`.
2. Agent shows the user a confirmation modal with: target name, assay type, sequence count + first 3 sequence previews, cost, expected turnaround (~21 days), webhook URL.
3. User confirms (`y`).
4. Agent calls `lab.submit_experiment` which POSTs to Adaptyv with the webhook URL set to `http://<proteus-host>:9876/webhooks/adaptyv` (or user-provided public URL).
5. Returns `experiment_id`; persisted in `experiments` table.

**Webhook receiver** (always running while Proteus is active):

```go
// internal/tools/lab/webhook.go
func StartReceiver(ctx context.Context, port int, store *store.Store) error {
    r := chi.NewRouter()
    r.Post("/webhooks/adaptyv", handleAdaptyv(store))
    srv := &http.Server{Addr: fmt.Sprintf(":%d", port), Handler: r}
    return srv.ListenAndServe()
}
```

The handler verifies HMAC signature (per Adaptyv docs — confirm field name in their OpenAPI spec), persists the event to `webhook_events`, updates the experiment record, and emits a Bubble Tea message that surfaces a notification in the TUI.

#### 7.2.9 Job control (`jobs.*`)

| Name | Purpose |
|---|---|
| `jobs.list` | Active and recent jobs |
| `jobs.status` | One job by ID |
| `jobs.cancel` | Graceful cancel |
| `jobs.result` | Fetch final result |

---

## 8. Skills System

### 8.1 Loading

Skills are embedded markdown files. The agent reads a skill before performing a non-trivial task. Discovery flow:

1. On every user turn, the system prompt reminds the agent: "*Before any design, scoring, or wet-lab task, list available skills with `skills.list` and read the relevant SKILL.md with `skills.read` before writing any code or running any design tool.*"
2. Built-in skills are embedded via `go:embed` from `internal/skills/builtin/`.
3. User skills are loaded from `~/.config/proteus/skills/`.

Implement `skills.list` and `skills.read` as tools (not as anything fancier).

### 8.2 Built-in skills (v1 set)

Each file is markdown. Authoritative templates below.

#### `design-binder.md`

```markdown
# Skill: Designing protein binders

## When to use
The user wants a de novo protein binder against a non-antibody target (cell-surface
receptor, viral protein, soluble protein). For antibodies, use `design-antibody.md`.

## Primary method: BindCraft
Use `design.bindcraft` first. BindCraft has experimental success rates of 10–100%
across diverse targets and typically requires ≤10 designs to be screened to find
high-affinity binders.

## Fallback: RFdiffusion + ProteinMPNN
If BindCraft is unavailable or yields no hits passing filters:
1. `design.rfdiffusion` with target structure and hotspots
2. `design.proteinmpnn` over the generated backbones (8 sequences per scaffold)
3. `fold.esmfold` or `fold.colabfold` to validate
4. Filter on pAE_interaction < 10, pLDDT > 85, ipTM > 0.8

## Required inputs
- Target structure (PDB ID or file path)
- Target chain
- Hotspots (residues defining the desired binding site)

## Standard parameters
- `num_designs`: 100 for BindCraft, 5000 for RFdiffusion campaigns
- `length_range`: [60, 120] residues for mini-binders
- Shortlist 10–24 top designs by ipTM + Rosetta ΔΔG for wet-lab submission

## Stop conditions
- If <5 designs pass filters, increase `num_designs` 2× and retry once
- If still <5, escalate to the user with a summary of what failed
```

#### `design-antibody.md`

```markdown
# Skill: Designing antibodies

## When to use
The user wants a VHH (nanobody) or scFv against a specific epitope.

## Primary method: RFantibody
Use `design.rfantibody` (RFdiffusion fine-tuned for antibody loops) followed by
`fold.rosettafold2_ab` filter. AlphaFold2 is NOT a reliable filter for
antibody-antigen complexes; do not use it.

## Fallback: Chai-2
For challenging targets where RFantibody yields <10 high-confidence hits,
try `design.chai2` which achieves binding for ~50% of targets including
sub-nanomolar affinities.

## Required inputs
- Target structure
- Epitope hotspots (CDR loops will be designed to contact these)
- Framework selection (default: humanized hu4D5 / trastuzumab)

## Standard parameters
- Generate 5,000 backbones via RFantibody
- 8 ProteinMPNN sequences per backbone
- Filter: RF2-AB pAE_interaction < 10, pLDDT > 85
- Shortlist top 24 by ipTM

## Wet-lab notes
- Adaptyv supports binding assays for antibody designs
- Note that ~21 day turnaround applies
```

#### `design-enzyme.md`

```markdown
# Skill: Designing enzymes

## When to use
The user wants a de novo enzyme for a specific reaction.

## Primary method: RFdiffusion2 + LigandMPNN
Use `design.rfdiffusion2` (atom-level active site scaffolding) followed by
`design.ligandmpnn` (ligand-conditioned sequence design). Validate with
`fold.chai1` rather than AlphaFold2 — Chai-1's side-chain interaction distances
align better with native enzyme active sites.

## Theozyme requirement
A theozyme (idealized arrangement of catalytic functional groups around the
transition state) is required input. Ask the user for it; if not provided,
search the literature with `knowledge.corpus` for related transition states.

## Standard parameters
- Generate 1000 backbones via RFdiffusion2
- 8 LigandMPNN sequences per backbone with catalytic residues fixed
- Filter: pLDDT > 80, motif RMSD < 1 Å
- For published successes (serine hydrolases), <96 sequences tested per case

## Workflow
1. Validate theozyme geometry
2. RFdiffusion2 generates backbones around the theozyme
3. LigandMPNN designs sequences (fix catalytic residues; design rest)
4. Chai-1 predicts the complex; filter by motif RMSD
5. Optional: PLACER for dynamic ensemble check on top designs
```

#### `filter-thresholds.md`

```markdown
# Skill: Filter thresholds

Standard cutoffs for shortlisting designs. Apply via `score.filter`.
Rank candidates by **ipSAE first** (best experimental correlation per Dunbrack 2025
and the Adaptyv Nipah G binder competition). Fall back to ipTM / pAE_interaction
only when ipSAE is unavailable.

| Metric | Threshold | Priority | Notes |
|---|---|---|---|
| ipSAE_min | > 0.50 | **primary** | inter-chain score; AF2/AF3/Boltz/Chai-compatible |
| pLDDT (binder mean) | > 80 | required | per-residue confidence on designed chain |
| pLDDT (min) | > 60 | required | catches local disorder |
| pAE_interaction | < 10 | secondary | use only if ipSAE unavailable |
| ipTM | > 0.8 | legacy | chain-length-biased; deprioritize in favor of ipSAE |
| RMSD (model vs prediction) | < 2.0 Å | required | self-consistency |
| pDockQ / pDockQ2 | > 0.23 / > 0.20 | optional | additional interface quality |
| Rosetta ΔΔG (binding) | < -20 REU | optional | physics check (if PyRosetta available) |
| ESM-2 perplexity | < 5 | optional | sequence naturalness |

## For enzymes specifically
- Motif RMSD < 1 Å (catalytic geometry preservation)
- Catalytic residue side-chain RMSD < 0.5 Å
- ipSAE not relevant (no inter-chain interface unless cofactor is a separate chain)

## Ranking
When producing a shortlist for wet-lab submission:
1. Filter by required thresholds above
2. Rank by ipSAE_min (descending)
3. Break ties with pLDDT (descending), then -Rosetta_ΔΔG (descending)
4. Take top N (default 24 for Adaptyv binding assays)

## When to relax thresholds
Only if fewer than 10 designs pass. Document the relaxation in plan notes and
prefer relaxing pLDDT_min before ipSAE_min.
```

#### `plan-from-target.md`

```markdown
# Skill: Planning from a target

## Procedure
Given a user-provided target, produce a `DesignPlan` for approval.

1. Identify the entity:
   - If PDB ID: `knowledge.pdb` for structure
   - If UniProt accession: `knowledge.uniprot` for sequence + features
   - If natural language: search for canonical identifier first
2. Characterize:
   - Domains (`knowledge.interpro`)
   - Known interactions, active sites, hotspots
3. Search literature:
   - `knowledge.europepmc.search` for "<target> de novo binder/antibody/enzyme"
   - Take top 30 results
   - `knowledge.corpus.add --from <search_id> --max 30`
4. Map over corpus:
   - `knowledge.corpus.map "What design methods were used and what was the experimental success rate?"`
   - `knowledge.corpus.grep -i "success rate"` to confirm specific claims
5. Decide application area (binder | antibody | enzyme) — usually obvious from the user prompt.
6. Select method from the relevant `design-*.md` skill.
7. Set filter thresholds from `filter-thresholds.md`.
8. Estimate compute cost and time.
9. Produce a `DesignPlan` object and present to the user for approval.

## Output format
Always emit the plan as a structured object (the TUI renders it as a checklist).
Plain prose summaries are not enough — they cannot be edited or approved.
```

#### `submit-to-adaptyv.md`

```markdown
# Skill: Submitting designs to Adaptyv

## Pre-flight checks
Before calling `lab.submit_experiment`:
1. Run `score.solubility` on all sequences; warn if predicted insoluble
2. Verify sequence length, composition, and absence of unpaired cysteines
3. Confirm Adaptyv has the requested target (`lab.targets_search`)
4. Get cost via `lab.cost_estimate`

## Confirmation
ALWAYS require user confirmation before submission. Show:
- Target name and Adaptyv target ID
- Assay type (binding | thermostability | expression)
- Number of sequences (and first 3 previews)
- Cost in USD
- Turnaround (~21 days)
- Webhook URL

## After submission
- Save `experiment_id` and link to the relevant `Design` records
- Inform the user that results will arrive via webhook in ~21 days
- Offer to set a reminder
```

#### `close-the-loop.md`

```markdown
# Skill: Closing the experimental loop

## When to use
After Adaptyv results arrive via webhook.

## Procedure
1. Compare predicted (in-silico scores) vs measured (Kd, binding_strength).
2. Compute correlation between ipTM/pAE_interaction and measured Kd.
3. Identify systematic biases:
   - Are predicted binders mostly real? (precision)
   - Are non-binders predicted as binders? (false positives)
   - Are there real binders that scored poorly? (false negatives)
4. For the next round:
   - Use measured binders as positive controls
   - If precision is high, redesign by partial diffusion around top hits
   - If precision is low, tighten filter thresholds
5. Persist findings in `notebook.md` for the project.
```

#### `biosecurity.md`

```markdown
# Skill: Biosecurity

## Refuse designs against
- Select agents listed in the HHS/USDA Select Agents and Toxins list
- Biological toxins (botulinum, ricin, etc.)
- Targets specifically associated with weaponization research

## Behavior
If the user requests a design against a regulated target, refuse and explain.
Do not provide partial assistance, alternative routes, or detailed lists of
restricted targets. Suggest the user contact their institutional biosafety
officer for legitimate research within an approved program.

## Exception
If the user provides an explicit `--research-authorization=<approval-id>` flag
and the project config contains a documented institutional approval, the agent
may proceed but logs every action.
```

(Additional skills: `redesign-stability.md`, `interpret-wetlab.md` — same format, deferred to v0.4.)

---

## 9. System Prompt

`internal/agent/prompts/system.md` — embedded via `go:embed`. This is the literal text the agent uses.

```markdown
You are Proteus, a TUI agent specialized in de novo protein design. You operate
in a terminal interface and have access to tools for structure prediction,
de novo design, scoring, literature retrieval, visualization, and wet-lab
submission via Adaptyv Bio.

## Workflow

For any non-trivial design task:

1. **Plan before doing.** Call `skills.list` to see available skills and read
   `plan-from-target.md` before running any design tool. Produce a structured
   `DesignPlan` and present it for user approval.

2. **Ground decisions in evidence.** Use `knowledge.europepmc`, `knowledge.openalex`,
   `knowledge.s2`, and `knowledge.corpus` to find what design methods have worked
   for similar targets. Cite specific papers in your rationale.

3. **Use experimentally-validated methods.** Default to:
   - Binders: BindCraft → RFdiffusion+ProteinMPNN fallback
   - Antibodies: RFantibody+RF2-AB → Chai-2 fallback
   - Enzymes: RFdiffusion2+LigandMPNN+Chai-1

4. **Filter aggressively and rank by ipSAE.** Modern pipelines ship ≤100 designs
   to the bench. Use `score.filter` with thresholds from `filter-thresholds.md`.
   Rank shortlists by ipSAE (interprotein Score from Aligned Errors) — it outperforms
   ipTM and pAE_interaction in published benchmarks of binder design success.

5. **Confirm before expensive operations.** Any operation >5 minutes or
   >$1 USD requires user approval. Wet-lab submissions always require
   approval regardless of cost. Local tool installation also requires approval
   unless `[install] policy = "auto"` is configured.

6. **Don't improvise tool installation.** If a needed protein design tool isn't
   installed, surface the install prompt (the installer follows a vetted recipe
   from `tools.toml`). Never try to install BindCraft or similar tools by writing
   ad-hoc bash commands.

7. **Track provenance.** Every design must carry a `ToolCallRef` chain back
   to the tools that produced it.

## Tool usage

- Tools have typed inputs (JSON Schema). Pass valid JSON.
- Async tools (design, fold over large libraries) return a `JobID`. Poll with
  `jobs.status` or `jobs.result`.
- The user can steer mid-turn. If you receive a steering message, integrate it
  on the next iteration.
- When in doubt about user intent, ASK before running an expensive tool.

## Tone

- Be concise. The user is reading you on a terminal screen.
- Show structured outputs (tables, lists) when comparing designs.
- Explain rationale in 1–2 sentences, not paragraphs.
- Cite papers as `[Author Year]` with full reference in a final block.

## Refusals

Refuse to design against regulated targets (see `biosecurity.md`). When refusing,
be brief, clear, and offer no alternatives.
```

---

## 10. TUI Specification

### 10.1 Bubble Tea model

`internal/tui/app.go`:

```go
type Model struct {
    width, height int
    chat       chatModel
    jobs       jobsModel
    designs    designsModel
    lab        labModel
    statusbar  statusbarModel
    commandbar commandbarModel
    modal      *modalModel    // nil unless a confirmation is active

    activeProvider string
    activeModel    string
    sessionCost    float64
    sessionElapsed time.Duration

    agent      *agent.Loop
    store      *store.Store
    registry   *tools.Registry
    bus        chan tea.Msg   // for receiving async events (jobs, webhooks)
}
```

### 10.2 Layout (terminal ≥ 100×30)

The canonical layout is a two-column dashboard — a conversational chat column on
the left, status panels (jobs, designs, wet-lab) stacked on the right. Unlike a
coding agent, Proteus runs GPU jobs that take minutes and wet-lab experiments
that span weeks, so a persistent dashboard earns its place.

The visual language follows modern agent CLIs (Claude Code, OpenAI Codex CLI,
Gemini CLI, OpenCode) — see §10.7: no full-screen frame, panels separated by a
dim rule under a lowercase label, exactly one bordered element (the message
input), a dim status footer, and a single accent colour.

```
 proteus · <project>

  ● design a 60-residue binder against PD-L1       jobs ───────────────────────
                                                    ⟳ rfdiffusion  c_8f2a  1m20s
  ● I'll generate the backbone, then run             ▓▓▓▓▓░░░  ~4m
    sequence design.                                ✓ proteinmpnn  c_8e10  2m04s

    ⏺ rfdiffusion.generate(PD-L1, len 60)           designs · 3 ─────────────────
    ⎿ submitted job c_8f2a · ~4 min                  d_001  plddt 91.3  ipsae 0.78
                                                     d_002  plddt 88.1  ipsae 0.71
  ✻ Designing the backbone… (12s · esc)
                                                    wet-lab ──────────────────────
                                                     expt_4 · day 3 of ~21

 ╭─ message ──────────────────────────────────────────────────────────────────╮
 │ ›                                                                           │
 ╰─────────────────────────────────────────────────────────────────────────────╯
   /model  /jobs  /designs  /doctor          claude-opus-4-7 · $0.42 · 14% ctx
```

For terminals < 100 columns, collapse to the chat column alone with `Tab`
cycling the jobs / designs / wet-lab panels; the message input, footer, and
overlays are unchanged in the narrow layout.

> **Versioning.** v0.1–v0.3 render this layout minimally (plain status bar,
> static hint line, no spinner). v0.4 delivers the modern visual design
> specified in §10.7; the mockup above is the v0.4 target.

### 10.3 Slash commands

| Command | Function |
|---|---|
| `/model` | Fuzzy-pick a model (switches the provider too) |
| `/skills` | List/inspect skills |
| `/jobs` | Full jobs view |
| `/designs` | Full designs browser |
| `/plan` | View/edit current plan |
| `/lab` | Wet-lab view |
| `/export <format>` | Export designs (`fasta`, `csv`, `pdb-bundle`, `json`) |
| `/project [name]` | Switch or create project |
| `/cost` | Cost breakdown |
| `/clear` | Compact context |
| `/help` | Help overlay |
| `/quit` | Save and exit |

### 10.4 Keybindings

| Key | Action |
|---|---|
| `Ctrl+C` | Cancel current operation (cooperative) |
| `Ctrl+D` | Quit |
| `Tab` | Cycle focus between panels |
| `Esc` | Close modal / dismiss overlay |
| `↑/↓` | Navigate within focused panel |
| `Enter` | Send message / activate |
| `Shift+Enter` | Newline in input |

### 10.5 Theme and design tokens

`internal/tui/theme.go` defines a **token palette** — semantic colour roles, not
ad-hoc per-widget styles. All colours are `lipgloss.AdaptiveColor` so the TUI
renders correctly on light and dark terminals (auto-detected from the
background); when `$NO_COLOR` is set the palette collapses to the terminal
default. No view code hard-codes a hex value — every style is derived from a
token.

```go
// Palette is the v0.4 token set. The status colours keep their v0.2 hex
// values; the foreground roles (Fg/FgMuted/FgSubtle/Border) are new in v0.4.
type Palette struct {
    Fg        lipgloss.AdaptiveColor // primary text
    FgMuted   lipgloss.AdaptiveColor // tool output, hints, footer
    FgSubtle  lipgloss.AdaptiveColor // section rules, placeholders, unfocused border
    Accent    lipgloss.AdaptiveColor // the single brand colour (focused border, user)
    Border    lipgloss.AdaptiveColor // input border when unfocused

    Queued    lipgloss.AdaptiveColor
    Running   lipgloss.AdaptiveColor
    Succeeded lipgloss.AdaptiveColor
    Failed    lipgloss.AdaptiveColor
    Warning   lipgloss.AdaptiveColor
}

var DefaultPalette = Palette{
    Fg:        lipgloss.AdaptiveColor{Light: "#1F2937", Dark: "#E5E7EB"},
    FgMuted:   lipgloss.AdaptiveColor{Light: "#6B7280", Dark: "#9CA3AF"},
    FgSubtle:  lipgloss.AdaptiveColor{Light: "#9CA3AF", Dark: "#4B5563"},
    Accent:    lipgloss.AdaptiveColor{Light: "#7C3AED", Dark: "#A78BFA"},
    Border:    lipgloss.AdaptiveColor{Light: "#D1D5DB", Dark: "#374151"},
    Queued:    lipgloss.AdaptiveColor{Light: "#9CA3AF", Dark: "#6B7280"},
    Running:   lipgloss.AdaptiveColor{Light: "#2563EB", Dark: "#60A5FA"},
    Succeeded: lipgloss.AdaptiveColor{Light: "#059669", Dark: "#34D399"},
    Failed:    lipgloss.AdaptiveColor{Light: "#DC2626", Dark: "#F87171"},
    Warning:   lipgloss.AdaptiveColor{Light: "#D97706", Dark: "#FBBF24"},
}
```

The `Theme` struct (rendered `lipgloss.Style` values) is rebuilt from the
palette. A single multi-theme picker is **out of scope until v0.5** (§20) — v0.4
ships one adaptive palette. See §10.7 for the components that consume the
tokens.

### 10.6 Inline graphics

`internal/tui/graphics.go`:

1. On startup, detect terminal capability:
   - Kitty graphics: `$TERM` contains `kitty` or query response to `\x1b_Gi=1,a=q;\x1b\\`
   - iTerm2: `$TERM_PROGRAM == iTerm.app`
   - Sixel: `\x1b[c` device-attributes response includes `4`
2. Wrap a PNG path with the appropriate escape sequence when rendering in the chat pane.
3. Fallback: show file path with a hint (`press 'o' to open`).

### 10.7 Modern TUI design (v0.4)

v0.1–v0.3 ship a functional but minimal TUI. v0.4 raises the visual language to
the standard set by modern agent CLIs (Claude Code, OpenAI Codex CLI, Gemini
CLI, OpenCode) while keeping the dashboard layout of §10.2. Every item below is
**additive rendering or input affordance** — no change to the agent loop, tool
behaviour, slash-command semantics, or persistence.

Each component is a self-contained Bubble Tea sub-model in its own file so the
work parallelises cleanly; `internal/tui/app.go` only wires them together.

#### 10.7.1 Visual language

- **No full-screen frame.** The terminal background is the canvas; the v0.1–v0.3
  outer box is removed.
- **Panels are separated by a dim rule, not a box.** A lowercase label followed
  by a horizontal run of `─` in `FgSubtle` (`jobs ─────────`). The nested box
  around the designs table is removed.
- **Exactly one bordered element:** the message input (§10.7.2).
- **One accent colour.** Everything else is `Fg` / `FgMuted` / `FgSubtle`.
- **Spacing over rules:** a blank line between chat entries; a one-space left
  gutter on every line.

#### 10.7.2 Message input — `internal/tui/commandbar.go`

- The `textarea` is wrapped in a `lipgloss.RoundedBorder()` with the label
  `message`.
- Border colour: `Accent` when the input is focused and idle, `FgSubtle` when a
  turn is running (the agent has the floor), `Border` otherwise.
- Prompt glyph `›` in `FgMuted`; placeholder `Type a message, or / for commands`.
- The static `slashCommandHints` line is **removed** — its role is taken by the
  autocomplete popup (§10.7.3) and the footer (§10.7.6).

#### 10.7.3 Slash-command autocomplete — `internal/tui/slashmenu.go` (new)

- Typing `/` as the first character of the input opens a popup **above** the
  input listing every slash command whose name has the typed text as a prefix,
  each with a one-line description.
- Keys: `↑/↓` move the selection, `Tab` completes the highlighted command into
  the input, `Esc` dismisses the popup, `Enter` submits the line as typed;
  typing narrows the list.
- The popup is a `slashMenuModel`; it reuses the `pickerModel` row styling.
- The command catalogue (name + description) is a single source-of-truth slice
  in `internal/tui/commands.go`, also consumed by `/help` and the footer hint.

#### 10.7.4 Thinking indicator — `internal/tui/spinner.go` (new)

- While a turn is running (`Model.running`) or a tool is mid-call, a status line
  renders directly above the message input:
  `<spinner> <verb>… (<elapsed>s · esc to interrupt)`.
- `<spinner>` cycles the Braille frames `⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏` on an ~80 ms `tea.Tick`.
- `<verb>` is drawn from a small set (`Designing`, `Folding`, `Scoring`,
  `Searching`, `Thinking`) selected by the active tool, defaulting to `Thinking`.
- `<elapsed>` counts whole seconds since the turn began.
- The indicator clears on `TurnDoneMsg` / `TurnErrorMsg`.

#### 10.7.5 Tool-call traces — `internal/tui/chat.go`

- A tool call renders as a header line `⏺ <tool>(<args>)` in `Fg`, followed by
  dim `FgMuted` result lines indented under a `⎿` connector.
- While running the header shows the spinner glyph; on completion it shows `⏺`
  (ok) or `✗` (error) and appends ` (<duration>)`.
- Result output is truncated to 6 lines; a truncated trace ends with
  `… +N lines` in `FgSubtle`.
- This replaces the v0.1 single-line `⚙ name → firstline` rendering.

#### 10.7.6 Status footer & context meter — `internal/tui/statusbar.go`

- The top bar is reduced to ` proteus · <project> ` in `Accent`.
- A new **footer** renders below the input in `FgMuted`:
  `<slash hints>   <model> · $<cost> · <NN>% context`.
- The context meter is the running token estimate as a percentage of the active
  model's context window; it turns `Warning` above 80%.
- The model has a `headerView()` (top) and a `footerView()` (bottom); the v0.1
  combined status bar is split accordingly.

#### 10.7.7 Startup welcome — `internal/tui/chat.go` + `app.go`

- On first render the chat pane shows a compact welcome block (≤4 lines) — not a
  large ASCII banner:

  ```
  proteus v0.4 · de novo protein design
  cwd: <dir>   model: <model>
  Type a message, or / for commands.  ? for help.
  ```

- It is a chat entry of a new `entryWelcome` kind, cleared by `/clear` like any
  other history.

#### 10.7.8 Panel polish — `internal/tui/jobs.go`, `designs.go`

- Section headers use the dim-label-plus-rule style of §10.7.1.
- Empty states are actionable: `no jobs yet · /install a tool or ask the agent
  to design`; `no designs yet · ask the agent to design binders`.
- A running job with a known ETA shows a unicode progress bar (`▓▓▓▓▓░░░`) sized
  to the elapsed-over-ETA ratio.
- Glyph set — single source of truth in `theme.go`:

  | State | Glyph | Token |
  |---|---|---|
  | queued | `·` | `Queued` |
  | running | spinner / `⟳` | `Running` |
  | succeeded | `✓` | `Succeeded` |
  | failed | `✗` | `Failed` |
  | cancelled | `⊘` | `FgMuted` |

#### 10.7.9 Out of scope for v0.4

- No multi-theme picker (full theming stays v0.5, §20) — one adaptive palette.
- No mouse support.
- No change to slash-command behaviour, the agent loop, tools, or persistence.

---

## 11. Agent Loop

`internal/agent/loop.go`:

```go
type Loop struct {
    provider llm.Provider
    model    string
    registry *tools.Registry
    session  *Session
    store    *store.Store
    bus      chan<- tea.Msg
}

func (l *Loop) Run(ctx context.Context, userInput string) error {
    l.session.AddUserMessage(userInput)

    for {
        if ctx.Err() != nil { return ctx.Err() }

        req := llm.ChatRequest{
            Model:    l.model,
            System:   l.session.SystemPrompt(),
            Messages: l.session.Messages(),
            Tools:    l.registry.Specs(),
        }

        events, err := l.provider.StreamChat(ctx, req)
        if err != nil { return err }

        var resp llm.ChatResponse
        for ev := range events {
            switch ev.Kind {
            case "text_delta":
                l.bus <- tui.TextDeltaMsg{Delta: ev.Delta}
                resp.Text += ev.Delta
            case "tool_call":
                resp.ToolCalls = append(resp.ToolCalls, *ev.Call)
            case "done":
                resp.Usage = ev.Usage; resp.StopReason = ev.StopReason
            case "error":
                return ev.Err
            }
        }

        l.session.AddAssistantMessage(resp)

        if len(resp.ToolCalls) == 0 { return nil }   // turn complete

        // Execute tool calls in parallel where safe
        results := l.executeTools(ctx, resp.ToolCalls)
        for _, r := range results {
            l.session.AddToolResult(r)
        }
        // loop back for next iteration
    }
}
```

**Steering:** the TUI can send a `SteerMsg` while the loop is running. The loop checks `select` on `ctx` and a steering channel between iterations; steering input is appended to the messages before the next LLM call.

**Compaction:** if `tokensUsed > 0.7 * contextWindow`, call `Session.Compact()` which summarizes prior tool calls into a `Lab Notebook` system message and truncates older messages.

---

## 12. Adaptyv Integration

### 12.1 Client

`internal/tools/lab/adaptyv.go`:

```go
type Client struct {
    baseURL string  // https://foundry-api-public.adaptyvbio.com/api/v1
    token   string
    http    *resty.Client
}

func NewClient(token string) *Client { ... }

// Endpoints (per OpenAPI spec; verify field names against the live spec)
func (c *Client) ListTargets(ctx, opts) ([]Target, error)
func (c *Client) EstimateCost(ctx, req CostEstimateRequest) (*CostEstimate, error)
func (c *Client) SubmitExperiment(ctx, req SubmitRequest) (*Experiment, error)
func (c *Client) GetExperiment(ctx, id string) (*Experiment, error)
func (c *Client) GetResults(ctx, id string) ([]Result, error)
```

Generate the request/response types from Adaptyv's OpenAPI spec at build time:

```bash
# Makefile target
gen-adaptyv:
	curl -s https://foundry-api-public.adaptyvbio.com/api/v1/openapi.json \
	  -o internal/tools/lab/openapi.json
	oapi-codegen -package adaptyv -generate types,client \
	  internal/tools/lab/openapi.json > internal/tools/lab/generated.go
```

### 12.2 Confirmation modal

Before `lab.submit_experiment` executes, the registry checks `RequiresConfirmation()` and the TUI shows:

```
┌─ Submit to Adaptyv Bio ───────────────────────────────────┐
│                                                           │
│  Target:        HER2 / ERBB2 (comp-her2-human)            │
│  Assay:         binding                                   │
│  Sequences:     24                                        │
│    1. MAQVQLVESG... (124 aa)                              │
│    2. MAQVQLQESG... (122 aa)                              │
│    3. MAQVQLVDSG... (125 aa)                              │
│    ...                                                    │
│  Estimated cost:  $3,600 USD                              │
│  Turnaround:      ~21 days                                │
│  Webhook URL:     http://localhost:9876/webhooks/adaptyv  │
│                                                           │
│  [Submit]  [Cancel]                                       │
└───────────────────────────────────────────────────────────┘
```

### 12.3 Webhook receiver

Always running on a goroutine while Proteus is active. Default port `9876`. If the user has a public URL (ngrok / Tailscale Funnel / cloudflared) configured in `config.toml`, the public URL is registered with Adaptyv; otherwise the local URL is used (works if the user runs Proteus during the full 21-day window; otherwise the agent polls via `lab.experiment_status` on startup to backfill).

---

## 13. Compute Backends

### 13.1 Local backend (uv-based, Proteus-managed installs)

Proteus manages Python tool installations itself using **uv** (Astral). The user is
not expected to know how to install BindCraft, RFdiffusion, ProteinMPNN, etc.
`proteus install <tool>` does it for them.

**Why uv (not conda):**
- 10–100× faster than pip/conda for cold installs.
- Native PyTorch+CUDA support via `--torch-backend=auto` which auto-detects the
  installed CUDA driver and picks the right PyTorch wheel index.
- Per-tool isolated environments via `uv venv`, no shared base env to corrupt.
- Lockfiles (`uv.lock`) give reproducible installs.
- No `conda activate` ritual; uv runs the command in the right env via `uv run`.

#### 13.1.1 Tool registry

`internal/backends/local/tools.toml` — describes how each Python tool is installed
and invoked. Embedded into the binary via `go:embed`.

```toml
# internal/backends/local/tools.toml

[bindcraft]
display_name = "BindCraft"
version      = "1.5.0"
python       = "3.10"
repo         = "https://github.com/martinpacesa/BindCraft"
git_ref      = "main"
install_dir  = "${PROTEUS_HOME}/tools/bindcraft"
venv_dir     = "${PROTEUS_HOME}/tools/bindcraft/.venv"
requires_gpu = true
disk_gb      = 8.5
extra_data   = ["alphafold_params"]   # see [data] section

# Install steps run in order. Each is a literal shell command executed in
# install_dir. The agent never improvises; it follows this exact recipe.
install_steps = [
  "git clone {{ repo }} . && git checkout {{ git_ref }}",
  "uv venv --python {{ python }} {{ venv_dir }}",
  "uv pip install --python {{ venv_dir }}/bin/python --torch-backend=auto torch==2.4.1",
  "uv pip install --python {{ venv_dir }}/bin/python -r requirements.txt",
  "uv pip install --python {{ venv_dir }}/bin/python git+https://github.com/sokrypton/ColabDesign.git",
  "uv pip install --python {{ venv_dir }}/bin/python pyrosetta-installer",
  "{{ venv_dir }}/bin/python -c 'import pyrosetta_installer; pyrosetta_installer.install_pyrosetta(serialization=True)'",
]

# How to invoke. Proteus substitutes {{ input_json }} with the path to a
# generated JSON file containing the tool's request payload.
run_command = "{{ venv_dir }}/bin/python {{ install_dir }}/bindcraft.py --settings {{ input_json }}"

[rfdiffusion]
display_name = "RFdiffusion"
version      = "1.1.0"
python       = "3.10"
repo         = "https://github.com/RosettaCommons/RFdiffusion"
git_ref      = "main"
install_dir  = "${PROTEUS_HOME}/tools/rfdiffusion"
venv_dir     = "${PROTEUS_HOME}/tools/rfdiffusion/.venv"
requires_gpu = true
disk_gb      = 5.2
extra_data   = ["rfdiffusion_weights"]

install_steps = [
  "git clone {{ repo }} . && git checkout {{ git_ref }}",
  "uv venv --python {{ python }} {{ venv_dir }}",
  "uv pip install --python {{ venv_dir }}/bin/python --torch-backend=auto torch==2.4.1",
  "uv pip install --python {{ venv_dir }}/bin/python -e .",
  "uv pip install --python {{ venv_dir }}/bin/python -e env/SE3Transformer",
]

run_command = "{{ venv_dir }}/bin/python {{ install_dir }}/scripts/run_inference.py @{{ args_file }}"

[proteinmpnn]
display_name = "ProteinMPNN"
version      = "1.0.1"
python       = "3.10"
repo         = "https://github.com/dauparas/ProteinMPNN"
git_ref      = "main"
install_dir  = "${PROTEUS_HOME}/tools/proteinmpnn"
venv_dir     = "${PROTEUS_HOME}/tools/proteinmpnn/.venv"
requires_gpu = true   # GPU recommended; CPU works but slow
disk_gb      = 0.5

install_steps = [
  "git clone {{ repo }} .",
  "uv venv --python {{ python }} {{ venv_dir }}",
  "uv pip install --python {{ venv_dir }}/bin/python --torch-backend=auto torch==2.4.1",
  "uv pip install --python {{ venv_dir }}/bin/python numpy",
]

run_command = "{{ venv_dir }}/bin/python {{ install_dir }}/protein_mpnn_run.py @{{ args_file }}"

[ligandmpnn]
display_name = "LigandMPNN"
python       = "3.10"
repo         = "https://github.com/dauparas/LigandMPNN"
venv_dir     = "${PROTEUS_HOME}/tools/ligandmpnn/.venv"
requires_gpu = true
disk_gb      = 1.2
install_steps = [
  "git clone {{ repo }} .",
  "uv venv --python {{ python }} {{ venv_dir }}",
  "uv pip install --python {{ venv_dir }}/bin/python --torch-backend=auto torch==2.4.1",
  "uv pip install --python {{ venv_dir }}/bin/python -r requirements.txt",
]
run_command = "{{ venv_dir }}/bin/python {{ install_dir }}/run.py @{{ args_file }}"

[rfantibody]
display_name = "RFantibody"
python       = "3.10"
repo         = "https://github.com/RosettaCommons/RFantibody"
venv_dir     = "${PROTEUS_HOME}/tools/rfantibody/.venv"
requires_gpu = true
disk_gb      = 6.0
install_steps = [
  "git clone {{ repo }} .",
  "uv venv --python {{ python }} {{ venv_dir }}",
  "uv pip install --python {{ venv_dir }}/bin/python --torch-backend=auto torch==2.4.1",
  "uv pip install --python {{ venv_dir }}/bin/python -r requirements.txt",
]
run_command = "{{ venv_dir }}/bin/python {{ install_dir }}/scripts/rfantibody.py @{{ args_file }}"

[rfdiffusion2]
display_name = "RFdiffusion2"
python       = "3.10"
repo         = "https://github.com/RosettaCommons/RFdiffusion2"  # update if upstream URL differs
venv_dir     = "${PROTEUS_HOME}/tools/rfdiffusion2/.venv"
requires_gpu = true
disk_gb      = 6.5
install_steps = [
  "git clone {{ repo }} .",
  "uv venv --python {{ python }} {{ venv_dir }}",
  "uv pip install --python {{ venv_dir }}/bin/python --torch-backend=auto torch==2.4.1",
  "uv pip install --python {{ venv_dir }}/bin/python -e .",
]
run_command = "{{ venv_dir }}/bin/python {{ install_dir }}/scripts/run.py @{{ args_file }}"

[boltz2]
display_name = "Boltz-2"
python       = "3.11"
venv_dir     = "${PROTEUS_HOME}/tools/boltz2/.venv"
requires_gpu = true
disk_gb      = 12.0
install_steps = [
  "uv venv --python {{ python }} {{ venv_dir }}",
  "uv pip install --python {{ venv_dir }}/bin/python --torch-backend=auto boltz",
]
run_command = "{{ venv_dir }}/bin/boltz predict {{ input_yaml }} --out_dir {{ out_dir }}"

[chai1]
display_name = "Chai-1"
python       = "3.11"
venv_dir     = "${PROTEUS_HOME}/tools/chai1/.venv"
requires_gpu = true
disk_gb      = 8.0
install_steps = [
  "uv venv --python {{ python }} {{ venv_dir }}",
  "uv pip install --python {{ venv_dir }}/bin/python --torch-backend=auto chai_lab",
]
run_command = "{{ venv_dir }}/bin/python -m chai_lab.main fold {{ input_fasta }} {{ out_dir }}"

[ipsae]
display_name = "ipSAE"
python       = "3.11"
repo         = "https://github.com/DunbrackLab/IPSAE"  # adjust if upstream differs
venv_dir     = "${PROTEUS_HOME}/tools/ipsae/.venv"
requires_gpu = false
disk_gb      = 0.1
install_steps = [
  "git clone {{ repo }} .",
  "uv venv --python {{ python }} {{ venv_dir }}",
  "uv pip install --python {{ venv_dir }}/bin/python numpy biopython",
]
run_command = "{{ venv_dir }}/bin/python {{ install_dir }}/ipsae.py {{ scores_json }} {{ structure_file }} {{ pae_cutoff }} {{ plddt_cutoff }}"

# --- Data assets (downloaded once, shared across tools) ---

[data.alphafold_params]
display_name = "AlphaFold2 weights"
url          = "https://storage.googleapis.com/alphafold/alphafold_params_2022-12-06.tar"
sha256       = "<filled-in-at-spec-time>"
extract_to   = "${PROTEUS_HOME}/data/alphafold_params"
size_gb      = 5.3

[data.rfdiffusion_weights]
display_name = "RFdiffusion weights"
urls         = [
  "http://files.ipd.uw.edu/pub/RFdiffusion/6f5902ac237024bdd0c176cb93063dc4/Base_ckpt.pt",
  "http://files.ipd.uw.edu/pub/RFdiffusion/e29311f6f1bf1af907f9ef9f44b8328b/Complex_base_ckpt.pt",
  # ... full list per RFdiffusion README
]
target_dir   = "${PROTEUS_HOME}/data/rfdiffusion_weights"
size_gb      = 4.0
```

#### 13.1.2 Installer

`internal/backends/local/installer.go` implements:

```go
type Installer struct {
    home      string                 // PROTEUS_HOME
    registry  ToolRegistry           // parsed tools.toml
    bus       chan<- tea.Msg         // for TUI progress events
}

func (i *Installer) Install(ctx context.Context, name string) error
func (i *Installer) Remove(ctx context.Context, name string) error
func (i *Installer) Verify(ctx context.Context, name string) error
func (i *Installer) Status(name string) ToolStatus
func (i *Installer) EnsureUV(ctx context.Context) error  // installs uv itself if missing
```

`Install` flow:

1. Check prerequisites: GPU (if `requires_gpu`), free disk space (> `disk_gb` × 1.5).
2. Ensure `uv` is installed (`EnsureUV` runs the Astral installer script on first use).
3. Create `install_dir` (default `${PROTEUS_HOME}/tools/<name>`).
4. Execute `install_steps` sequentially, streaming stdout/stderr to the TUI.
5. Download `extra_data` assets (with resume support via Range headers).
6. Verify by running `tool --version` or a trivial smoke test (e.g., `proteinmpnn --help`).
7. Write `${install_dir}/.proteus.lock` with the resolved version, install timestamp,
   uv lockfile hash, and GPU/CUDA at install time. Used by `proteus doctor`.

Errors are wrapped with the step name so failures are diagnosable. A failed install
leaves the partial directory; `proteus install <name> --force` wipes and retries.

#### 13.1.3 Doctor

`proteus doctor` runs a full diagnostic without performing any installs:

```
$ proteus doctor
Proteus 0.5.0

System
  ✓ Operating system:   Linux 6.5 (x86_64)
  ✓ uv:                 0.5.7 at /home/alvaro/.local/bin/uv
  ✓ Python (uv-managed): 3.10.14, 3.11.10
  ✓ NVIDIA driver:      550.90.07 (CUDA 12.4)
  ✓ GPU:                RTX 4090 (24 GB free)
  ✓ Disk free at $PROTEUS_HOME: 412 GB

LLM providers
  ✓ Anthropic API:      reachable (token in keychain)
  ✓ Ollama:             reachable at localhost:11434 (3 models)
  ⚠ OpenAI API:         no key configured

Knowledge sources
  ✓ Europe PMC:         reachable
  ✓ OpenAlex:           reachable
  ✓ UniProt:            reachable
  ✓ RCSB PDB:           reachable

Local protein tools
  ✓ ipsae               v1.2 (84 MB)
  ✓ proteinmpnn         v1.0.1 (462 MB)
  ✓ rfdiffusion         v1.1.0 (5.4 GB) — last verified 2 days ago
  ✗ bindcraft           not installed   (run: proteus install bindcraft)
  ✗ rfantibody          not installed   (run: proteus install rfantibody)
  - boltz2              not installed (optional)
  - chai1               not installed (optional)

Cloud backends
  ✓ Modal:              configured (token in env)
  ⚠ ESM Atlas:          reachable (no auth required)

Wet-lab
  ✓ Adaptyv API:        reachable (token in keychain)
  ✓ Webhook receiver:   listening on :9876
```

#### 13.1.4 Installer UX in the TUI

When the agent decides it needs a tool that isn't installed, it does not improvise.
It surfaces a modal:

```
┌─ Install required tool ──────────────────────────────────┐
│                                                          │
│  Tool:         BindCraft 1.5.0                           │
│  Disk space:   ~8.5 GB                                   │
│  Data assets:  AlphaFold2 weights (5.3 GB)               │
│  Time:         ~10–15 minutes (depends on bandwidth)     │
│                                                          │
│  Source:       github.com/martinpacesa/BindCraft         │
│  Install path: ~/proteus/tools/bindcraft/                │
│                                                          │
│  [Install]   [Cancel]   [Use Modal instead]              │
└──────────────────────────────────────────────────────────┘
```

If the user accepts, the installer runs with a progress view (step-by-step,
streaming output collapsed by default, expand on `Tab`). On success the agent
resumes the original task. On failure, the agent surfaces the error and offers
the Modal fallback (if `--compute=modal` is configured) or to retry.

#### 13.1.5 Agent install permission

By default the agent must ask before installing. Two policies, controlled by
`config.toml`:

```toml
[install]
policy = "ask"        # ask | auto | never
auto_for_small = true # auto-install tools < 1 GB without prompting (ipsae, proteinmpnn)
```

`never` forces the user to install manually via `proteus install <name>` before
the agent can use the tool.

### 13.2 Modal backend

`internal/backends/modal/functions.py` — Python file the user deploys via
`proteus modal deploy`. Contains `@modal.function` definitions for each protein tool.
Modal images use the same install recipes from `tools.toml`, translated to Modal
image builders (also `uv`-based for parity).

Example:

```python
import modal

app = modal.App("proteus-tools")

rfdiff_image = (
    modal.Image.debian_slim(python_version="3.10")
    .apt_install("git", "wget")
    .run_commands([
        "pip install uv",
        "git clone https://github.com/RosettaCommons/RFdiffusion /opt/rfdiffusion",
        "uv venv --python 3.10 /opt/rfdiffusion/.venv",
        "uv pip install --python /opt/rfdiffusion/.venv/bin/python --torch-backend=auto torch==2.4.1",
        "uv pip install --python /opt/rfdiffusion/.venv/bin/python -e /opt/rfdiffusion",
    ])
)

@app.function(image=rfdiff_image, gpu="A10G", timeout=3600)
def run_rfdiffusion(spec: dict) -> dict:
    # ... invoke RFdiffusion via /opt/rfdiffusion/.venv/bin/python
    return {"job_id": ..., "designs": [...]}
```

The Go client calls Modal via the Modal API (HTTP endpoints exposed for each function).

**Backend symmetry guarantee:** the same input JSON yields the same output schema
whether a tool runs locally or on Modal. Backend selection is purely an
infrastructure choice; the agent's reasoning code never branches on it.

### 13.3 Hosted backend

`internal/backends/hosted/esm_atlas.go` — direct REST client for `https://api.esmatlas.com`.
No auth, rate-limited. Used as fallback for `fold.esmfold` when no GPU is available
and the user hasn't deployed to Modal.

---

## 14. Configuration

### 14.1 Files

```
~/.config/proteus/
├── config.toml
├── models.toml
└── skills/                   # user skills

~/proteus/projects/<name>/
├── proteus.toml              # project config
├── plan.json                 # latest plan
├── workspace.db              # SQLite
├── corpus.bleve/             # full-text index
├── designs/                  # PDB/FASTA files
├── experiments/              # wet-lab submissions
└── notebook.md
```

### 14.2 Sample `config.toml`

```toml
[ui]
theme = "auto"               # auto | light | dark
inline_graphics = "auto"     # auto | kitty | sixel | iterm2 | off

[defaults]
provider = "auto"            # auto-detects anthropic > ollama > error
model = ""                   # empty → use provider's default
compute_backend = "modal"    # local | modal | hosted

[webhook]
enabled = true
port = 9876
public_url = ""              # optional ngrok/Tailscale URL

[budget]
session_soft_limit_usd = 5.0
wetlab_requires_confirmation = true  # never disable

[knowledge]
mailto = "user@example.com"  # for OpenAlex polite pool; recommended
biorxiv_recent_days = 30
corpus_default_max_papers = 30
```

### 14.3 Environment variables

| Variable | Purpose |
|---|---|
| `ANTHROPIC_API_KEY` | Anthropic provider |
| `OPENAI_API_KEY` | OpenAI provider |
| `GOOGLE_API_KEY` | Google provider |
| `ADAPTYV_API_TOKEN` | Adaptyv Foundry API |
| `MODAL_TOKEN_ID` / `MODAL_TOKEN_SECRET` | Modal |
| `S2_API_KEY` | Optional, for higher Semantic Scholar rate limit |
| `BRAVE_API_KEY` or `TAVILY_API_KEY` | Optional, for web search |
| `PROTEUS_HOME` | Override `~/proteus` |
| `PROTEUS_CONFIG_DIR` | Override `~/.config/proteus` |

All secrets read from env first, then OS keychain (`99designs/keyring`), never written to disk in plaintext.

---

## 15. CLI Subcommands

`proteus` is both the TUI and a CLI. Cobra commands:

```
proteus                              # launches TUI (default)
proteus tui                          # explicit
proteus doctor                       # diagnostic check
proteus auth <provider>              # store API key in keychain
proteus project new <name>           # create project
proteus project list
proteus project switch <name>
proteus install <tool>               # install a local protein tool via uv
proteus install --all                # install all GPU-eligible tools
proteus install <tool> --force       # wipe and reinstall
proteus uninstall <tool>             # remove a local tool
proteus list tools                   # list installable tools + status
proteus design submit <ids>...       # submit designs to Adaptyv (CLI flow)
proteus experiment status <id>
proteus modal deploy                 # deploy Modal functions
proteus replay <session.json>        # replay a session
proteus export <project> --format <fmt>
proteus version
```

### 15.1 Install behaviors

- `proteus install bindcraft` — same recipe as the TUI install modal, but headless with stdout progress.
- `proteus install --all` — installs ipsae, proteinmpnn, rfdiffusion, bindcraft, rfantibody, rfdiffusion2, ligandmpnn (skips boltz2/chai1 unless `--include-folders` is passed).
- `proteus install --dry-run <tool>` — prints the commands that would run without executing.
- `PROTEUS_INSTALL_POLICY=never` — agent will not auto-install during a session; surfaces an error pointing the user at `proteus install`.

---

## 16. Security and Safety

### 16.1 Credential handling

- API keys from OS keychain via `99designs/keyring`. Linux: Secret Service; macOS: Keychain; Windows: Credential Manager.
- Never logged. Redact from any captured logs/traces.
- Webhook signatures verified via HMAC.

### 16.2 Sandboxed shell

- `fs.bash` runs in `$PROJECT_WORKSPACE` only.
- Binary allowlist (configurable). Default: `ls cat grep sed awk jq python3 conda git curl wget`.
- Denylist: `rm -rf`, `dd`, `mkfs`, anything with `sudo`.
- Network: allowed by default; configurable per project.

### 16.3 Cost guards

- Session soft-limit (default $5). Prompt before further paid calls.
- Wet-lab submission ALWAYS requires modal confirmation regardless of cost.
- `--dry-run` flag: tools return mock responses; no external calls or GPU runs.

### 16.4 Biosecurity

- Hardcoded refusal list in `internal/safety/restricted_targets.go`.
- Refer to `biosecurity.md` skill for behavior.
- Authorized research exception requires `--research-authorization=<id>` flag AND a documented approval in project config.

---

## 17. Observability

- Structured logs (zerolog JSON) at `~/.local/share/proteus/logs/proteus.log`. Rotation: 100 MB, keep 5.
- Per-session replay: `sessions/<id>/session.json` captures all messages, tool calls, and outputs (with input hashes).
- Optional OpenTelemetry traces (opt-in via `OTEL_EXPORTER_OTLP_ENDPOINT`).
- `proteus replay <session.json>` re-runs deterministically against cached tool outputs.

---

## 18. Testing

### 18.1 Layers

| Layer | Tests | Tooling |
|---|---|---|
| Domain types | Unit + property | `testing` + `testing/quick` |
| Tool wrappers | Recorded fixtures | `go-vcr` |
| LLM providers | Mock + live (env-gated) | custom mock; `LIVE_LLM_TESTS=1` |
| Agent loop | Replay regressions | session replay |
| TUI | Snapshot | `teatest` |
| End-to-end | "Hello sequence" smoke | runs against ESM Atlas |

### 18.2 Smoke test (must pass on every PR)

```go
// internal/agent/smoke_test.go
func TestSmoke_FoldAndScore(t *testing.T) {
    // 1. Start agent with mock LLM that calls fold.esmfold once
    // 2. Verify a Design record is created with pLDDT
    // 3. Verify session is persisted
}
```

### 18.3 Eval (later milestone)

`eval/biodesignbench/` — implements a subset of BioDesignBench tasks. Run via `proteus eval bench --subset small`.

---

## 19. Distribution

- `go build -ldflags='-s -w' -o bin/proteus`
- Cross-compile in `scripts/release.sh` for: darwin/arm64, darwin/amd64, linux/amd64, linux/arm64, windows/amd64.
- GitHub Releases with checksums.
- Install script:

```bash
# scripts/install.sh
#!/usr/bin/env bash
set -euo pipefail
OS=$(uname -s | tr '[:upper:]' '[:lower:]')
ARCH=$(uname -m); [[ "$ARCH" == "x86_64" ]] && ARCH=amd64
[[ "$ARCH" == "aarch64" ]] && ARCH=arm64
URL="https://github.com/<user>/proteus/releases/latest/download/proteus_${OS}_${ARCH}"
curl -fsSL "$URL" -o /usr/local/bin/proteus
chmod +x /usr/local/bin/proteus
echo "Installed proteus. Run 'proteus' to start."
```

Homebrew tap to follow.

---

## 20. Milestones and Acceptance Criteria

Each milestone is a tagged release. Acceptance criteria are explicit and testable.

### v0.1 — "Hello, sequence" (Week 1–2)

**Scope:** TUI shell, agent loop, one provider, one tool, no persistence.

**Implements:**
- `cmd/proteus/main.go`
- `internal/tui/` chat-only layout
- `internal/agent/loop.go`
- `internal/llm/provider.go` + `anthropic.go` + `openai.go` (OpenAI-compatible covers Ollama)
- `internal/tools/registry.go`, `fs.go`, `fold/esmfold.go`
- `internal/skills/builtin/` (loaded but minimal: just `filter-thresholds.md`)
- `internal/agent/prompts/system.md`

**Acceptance criteria:**
1. `proteus` launches a TUI with chat pane.
2. User can type "fold MAQVQLVESGGGLVQAGGSLRLSCAASGFTFSSYAMSWVRQAPGKGLEW" and the agent calls `fold.esmfold`, returns a PDB file path and pLDDT.
3. Switching to a local Ollama model via `/model` works.
4. `Ctrl+C` cancels mid-tool-call cleanly.
5. Smoke test passes in CI.

### v0.2 — "Real designs" (Week 3–4)

**Scope:** SQLite persistence, jobs system, **uv-based installer**, BindCraft + RFdiffusion + ProteinMPNN + ipSAE via local backend OR Modal.

**Implements:**
- `internal/store/` (SQLite + migrations)
- `internal/domain/types.go` (full schema including `FilterConfig` with `MinIPSAE`)
- `internal/backends/local/` (installer.go, registry.go, runner.go, tools.toml, uv.go)
- `internal/backends/modal/` (Go client + Python functions)
- `internal/tools/design/bindcraft.go`, `rfdiffusion.go`, `proteinmpnn.go`
- `internal/tools/score/metrics.go`, `ipsae.go`, `filter.go`
- `internal/tools/jobs/*`
- Jobs panel and designs panel in TUI (designs panel shows ipSAE column)
- CLI: `proteus install <tool>`, `proteus list tools`, `proteus doctor`

**Acceptance criteria:**
1. `proteus doctor` runs cleanly on a fresh machine (no installed tools yet) and lists everything as "not installed."
2. `proteus install ipsae` succeeds in under 60 seconds on a typical broadband connection.
3. `proteus install proteinmpnn` succeeds and `proteus doctor` shows it as installed.
4. `proteus install bindcraft` succeeds end-to-end including PyRosetta and AlphaFold weights download. On failure, the error message identifies the failing install step.
5. `proteus modal deploy` deploys Modal functions successfully (alternative path; tested separately).
6. User can run "design 100 binders against PDB 1ZWG" with `compute_backend = "local"` and the agent: (a) detects BindCraft is needed, (b) checks it's installed (or prompts to install), (c) runs it, (d) scores designs with `score.ipsae`, (e) returns ≥10 designs filtered by `MinIPSAE > 0.5` and `MinPLDDT > 80`.
7. Designs are persisted; surviving a restart shows them in the designs panel with ipSAE values.
8. Jobs panel shows running/queued/completed status with ETAs.
9. Cancellation of a running tool (local or Modal) works (best-effort).
10. The same design task produces the same output schema regardless of `compute_backend`.

### v0.3 — "Plan from target" (Week 5–6)

**Scope:** Free knowledge stack, per-project corpus, structured planning.

**Implements:**
- `internal/tools/knowledge/` (europepmc, openalex, s2, biorxiv, crossref, uniprot, pdb, interpro, web_search, web_fetch, corpus)
- `internal/skills/builtin/plan-from-target.md`, `design-binder.md`
- `pkg/proteinio/` (FASTA, PDB, mmCIF)
- Plan persistence in `plans` table
- Plan view in TUI (`/plan`)

**Acceptance criteria:**
1. User types "design VHH binders against SARS-CoV-2 spike RBD" → agent fetches UniProt P0DTC2, PDB 6M0J, searches Europe PMC, adds top 30 papers to corpus, maps over them, and produces an editable `DesignPlan`.
2. Plan shows: target, application, method, filter thresholds, cost estimate, evidence papers with DOIs.
3. User can approve, edit, or cancel the plan.
4. Corpus persists per project; `knowledge.corpus.grep` returns results consistent with `corpus.search`.
5. Three additional LLM providers (OpenAI, Google) work via `/model`.

### v0.4 — "Closing the loop" (Week 7–8)

**Scope:** Adaptyv integration, antibody + enzyme tracks (with installer
recipes), and the modern TUI visual redesign (§10.7).

**Implements:**
- `internal/tools/lab/` (adaptyv.go + webhook.go)
- `internal/tools/design/rfantibody.go`, `chai2.go`, `rfdiffusion2.go`, `ligandmpnn.go`
- `internal/tools/fold/boltz2.go`, `chai1.go`
- `tools.toml` entries for rfantibody, rfdiffusion2, ligandmpnn, boltz2, chai1
- `internal/skills/builtin/design-antibody.md`, `design-enzyme.md`, `submit-to-adaptyv.md`, `close-the-loop.md`
- Wet-lab panel in TUI
- Webhook receiver
- **Modern TUI design (§10.7):** design-token palette (`theme.go`), bordered
  message input, slash-command autocomplete (`slashmenu.go`, `commands.go`),
  thinking indicator (`spinner.go`), tree-connected tool traces, status footer
  with a context meter, startup welcome, and panel polish.

**Acceptance criteria:**
1. `proteus auth adaptyv` stores token in keychain.
2. Agent calls `lab.targets_search` and lists Adaptyv targets.
3. Submission flow runs end-to-end against Adaptyv staging environment; confirmation modal appears; experiment ID persists.
4. Webhook receiver accepts a test POST and the wet-lab panel updates.
5. `proteus install rfantibody` and `proteus install ligandmpnn` succeed.
6. Antibody track: user can design VHHs against PDB 6M0J using `design.rfantibody`; designs scored with `score.ipsae` even though AlphaFold2 is not the filter (ipSAE works on the RF2-AB output).
7. Enzyme track: user can design enzymes around a theozyme using `design.rfdiffusion2` + `design.ligandmpnn` with `fold.chai1` as the validator.
8. The TUI renders the §10.7 modern design: no full-screen frame, a single
   bordered message input, dim section rules between panels, and a status
   footer carrying the context meter.
9. Typing `/` opens the slash-command autocomplete popup; `↑/↓` select, `Tab`
   completes, `Esc` dismisses.
10. A running turn shows an animated thinking indicator with elapsed seconds and
    an `esc to interrupt` hint; tool calls render as `⏺`/`⎿` traces with a
    duration.
11. `go test ./...` passes and `go vet ./...` is clean after the redesign.

### v0.5 — "Polish" (Week 9–10)

**Scope:** Inline graphics, theming, replay, biosecurity, polish.

**Implements:**
- `internal/tui/graphics.go` (Kitty/Sixel/iTerm2)
- `internal/tools/viz/` (pymol_render, contact_map, metric_plot)
- `internal/tools/knowledge/blast.go`, `local_pdfs.go`, `paperclip.go` (optional)
- `internal/safety/restricted_targets.go`
- `proteus replay` subcommand
- Themes, full keybinding set
- Install scripts, Homebrew tap, GitHub release automation

**Acceptance criteria:**
1. Folding a sequence shows the structure inline in Kitty or iTerm2.
2. `proteus replay <session.json>` reproduces a recorded session deterministically.
3. Biosecurity refusal triggers correctly on a test restricted target.
4. Light and dark themes render correctly on macOS Terminal, iTerm2, Ghostty, WezTerm, Alacritty, and Linux gnome-terminal.
5. Single-binary releases work on all five target platforms.

### v1.0 — "Stable" (Q4 2026)

- BioDesignBench eval published.
- Documented skill marketplace.
- All v0.x criteria pass on CI.

---

## 21. References (key citations)

- Listov, D., et al. *Closing the loop: Experimentally validated methods in AI-driven protein design.* Curr. Opin. Struct. Biol. (2026).
- Dunbrack, R.L. *Rēs ipSAE loquuntur: What's wrong with AlphaFold's ipTM score and how to fix it.* bioRxiv (2025), doi:10.1101/2025.02.10.637595.
- Pacesa, M., et al. *One-shot design of functional protein binders with BindCraft.* Nature (2025).
- Bennett, N.R., et al. *Atomically accurate de novo design of antibodies with RFdiffusion.* Nature (2025).
- Ahern, W., et al. *Atom-level enzyme active site scaffolding using RFdiffusion2.* Nat. Methods (2025).
- Lauko, A., et al. *Computational design of serine hydrolases.* Science (2025).
- Watson, J.L., et al. *De novo design of protein structure and function with RFdiffusion.* Nature (2023).
- Dauparas, J., et al. *Robust deep learning-based protein sequence design using ProteinMPNN.* Science (2022).
- Adaptyv Bio Foundry API documentation, `https://docs.adaptyvbio.com/`.
- uv documentation, `https://docs.astral.sh/uv/`.

---

## 22. Open Questions (defer with documented v1 defaults)

1. **Antibody framework default.** v1 default: humanized hu4D5 (trastuzumab scaffold). Configurable per project.
2. **Modal vs BYO.** v1: BYO. Hosted "Proteus Cloud" out of scope.
3. **Multi-user mode.** Out of scope for v1; schema is forward-compatible.
4. **Reasoning model integration.** v1: surface reasoning tokens in a collapsible block in the chat pane.
5. **Paperclip fallback strategy if user has account.** v1: `knowledge.paperclip` is registered if `PAPERCLIP_TOKEN` is set; the agent prefers it over individual free APIs when available.
6. **License.** v1: Apache-2.0.

---

## 23. Glossary

- **pLDDT** — per-residue confidence score from AlphaFold/ESMFold (0–100).
- **pAE / pAE_interaction** — predicted aligned error; pAE_interaction is the inter-chain subset.
- **ipTM** — interface predicted TM-score, complex confidence (0–1). Biased by chain length / disorder.
- **ipSAE** — interprotein Score from Aligned Errors (Dunbrack 2025). Interface-focused metric that outperforms ipTM for binder design ranking. Works on AF2, AF3, Boltz, Chai outputs.
- **pDockQ / pDockQ2** — interface quality scores; complementary to ipSAE.
- **Theozyme** — idealized arrangement of catalytic functional groups around a reaction transition state.
- **VHH** — heavy-chain-only antibody (nanobody).
- **scFv** — single-chain variable fragment (linked heavy + light).
- **Kd** — dissociation constant; lower = tighter binding.
- **MSA** — multiple sequence alignment; used by AlphaFold2.
- **uv** — Python package manager from Astral; used by Proteus to install all protein design tools in isolated environments.

---

*End of specification, v0.2 — implementation-ready.*
