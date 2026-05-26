# fova

**A terminal agent for de novo protein design.** fova plans, runs, and ranks
design jobs from a single Go binary — and (optionally) ships the survivors to a
wet lab. It pairs an LLM-driven planner with GPU-bound design and prediction
tools, free literature retrieval, and a built-in jobs system.

> **Status: alpha (v0.5.0).** The core loop works end-to-end, but the agent
> still has rough edges and the wet-lab + antibody/enzyme tracks are
> incomplete. See [Known issues](docs/KNOWN-ISSUES-2026-05-21.md) before using
> it on anything you care about.

```
 ┌─╮
 │ ╰──●     fova — protein design, from the terminal
 ├─╮
 │ ╰─
 │
```

---

## Why fova

- **Free by default.** No account is required. Local LLMs (Ollama, vLLM,
  LM Studio) work out of the box. Paid providers (Anthropic, OpenAI, Google)
  and Adaptyv wet-lab submission are opt-in.
- **One static binary.** Pure Go, pure-Go SQLite, no system Python required at
  install time. Heavy ML tools (RFdiffusion, ProteinMPNN, BindCraft, …) are
  installed on demand into per-tool `uv` environments via `/install`.
- **Free knowledge stack.** Europe PMC, OpenAlex, Semantic Scholar, bioRxiv,
  Crossref, UniProt, RCSB PDB, InterPro, BLAST, web search/fetch — and a
  per-project local literature corpus backed by Bleve.
- **Provenance everywhere.** Every design carries its full lineage in a local
  SQLite store. Sessions can be exported as replay documents.

## Features

- Bubble Tea TUI with chat, jobs, designs, and lab panels; slash commands with
  autocomplete; animated thinking indicator; tree-connected tool traces.
- **Design tools:** BindCraft, RFdiffusion, RFdiffusion2, ProteinMPNN,
  LigandMPNN, RFantibody, BoltzGen.
- **Structure prediction:** ESMFold, Boltz-2, Chai-1.
- **Scoring:** ipSAE (primary interface metric), structural metrics, filters.
- **Knowledge:** the free APIs above plus a per-project Bleve corpus, local PDF
  ingestion, and an optional Paperclip MCP forwarder.
- **Backends:** local (uv-managed) and Modal (BYO — you bring your own Modal
  account).
- **Wet-lab (preview):** Adaptyv Foundry submission, target search, cost
  estimate, experiment status, and a webhook receiver for results.
- **Replay:** `fova export <session> out.json` and `fova replay out.json`.

## Install

### One-line installer

```sh
curl -fsSL https://fova.dev/install | sh
```

The installer fetches the latest GitHub release for your platform
(`linux`/`darwin` × `amd64`/`arm64`), verifies SHA256 against the release's
`checksums.txt`, and installs to `~/.local/bin/fova`. Override the target with
`FOVA_INSTALL_DIR=/somewhere/else` or pin a version with `FOVA_VERSION=v0.5.0`.

### From source

```sh
git clone https://github.com/alvarogonjim/fova
cd fova
make build              # writes bin/fova
./bin/fova
```

Requirements: Go 1.22+. fova is Unix-only (it shells out to `bash` and a
container runtime) — Windows users should run under WSL.

### Homebrew

```sh
brew install alvarogonjim/tap/fova
```

## Quickstart

```sh
fova                    # launches the TUI
```

On first run, an onboarding wizard walks you through picking a model and
(optionally) entering an API key. After that, type into the chat and the agent
will plan, call tools, run jobs, and rank designs.

A useful first turn:

> *Design a binder against PD-L1. Pull the top-3 recent papers, draft a plan,
> and stop at the plan-approval gate.*

The agent will hit Europe PMC + OpenAlex, build a `DesignPlan`, and pause for
`/plan approve` before launching any GPU jobs.

## LLM setup

fova picks a provider based on the first API key it finds and your
`config.toml`'s `[defaults]` block. Set the env var for the provider you want.

### Cloud providers

```sh
export ANTHROPIC_API_KEY=sk-ant-…    # Claude
export OPENAI_API_KEY=sk-…           # GPT
export GOOGLE_API_KEY=…              # Gemini
```

Models are defined in `~/.config/fova/models.toml` (materialized from the
embedded defaults on first run). Edit it via `/config edit models` or switch
the active model in-app with `/model <id>`.

### Ollama (local, easiest)

[Install Ollama](https://ollama.com), pull a tool-capable model, then point
fova at it:

```sh
ollama pull llama3.3:70b
ollama serve                         # default: http://localhost:11434
fova                                 # /model llama3.3:70b
```

Ollama is pre-wired in `models.toml` as the `ollama` provider with
`base_url = "http://localhost:11434/v1"`. No API key needed.

### vLLM (local, throughput-tuned)

Run any OpenAI-compatible vLLM server:

```sh
vllm serve Qwen/Qwen3.6-27B \
  --host 0.0.0.0 --port 8000 \
  --api-key secret-token-please-change
export VLLM_API_KEY=secret-token-please-change
```

`vllm` is pre-wired as a provider in `models.toml`. Add or edit `[[model]]`
entries to point at your served model. The base URL defaults to
`http://localhost:8000/v1`.

### LM Studio (local, GUI-friendly)

LM Studio exposes an OpenAI-compatible server at `http://localhost:1234/v1`.
Add a provider block to `models.toml`:

```toml
[[provider]]
name     = "lmstudio"
kind     = "openai"
base_url = "http://localhost:1234/v1"

[[model]]
id             = "your-model-name"
display_name   = "LM Studio (local)"
provider       = "lmstudio"
context_tokens = 32768
supports_tools = true
```

Reload with `/reload`.

## Design tools

Design tools run inside isolated `uv` environments managed by fova. Install
them from inside the TUI:

```
/tools              # list available tools and their install status
/install bindcraft  # install one
/doctor             # check the local environment
```

The local backend assumes a container runtime (Docker or Podman) is available
on PATH for tools that need CUDA — `/doctor` will tell you what's missing.

For elastic compute, deploy the same tools to Modal:

```
/modal deploy
```

This writes `functions.py` and runs the Modal CLI for you. You bring your own
Modal account; fova does not run a proprietary cloud backend.

## Configuration

fova materializes its config on first run. Default locations follow XDG:

| File                         | Purpose                                          |
| ---------------------------- | ------------------------------------------------ |
| `~/.config/fova/config.toml` | UI theme, default provider/model, backend, knowledge tunables, webhook, budget |
| `~/.config/fova/models.toml` | Provider definitions + model catalog            |
| `~/.config/fova/system.md`   | Base system prompt — edit to steer the agent    |
| `~/.config/fova/skills/`     | Markdown skills surfaced via `/skills`          |
| `~/fova/`                    | Data dir: workspace DB, project corpora, jobs   |

Open any of these with `/config edit <name>` or print the path with
`/config path`. Validate with `/config validate`.

### Environment variables

| Variable                | Effect                                                         |
| ----------------------- | -------------------------------------------------------------- |
| `FOVA_HOME`             | Override the data dir (`~/fova`).                              |
| `FOVA_CONFIG_DIR`       | Override the config dir (`~/.config/fova`).                    |
| `FOVA_COMPUTE_BACKEND`  | `local` or `modal` — overrides `[defaults]`.                   |
| `ANTHROPIC_API_KEY`     | Claude API key.                                                |
| `OPENAI_API_KEY`        | OpenAI API key.                                                |
| `GOOGLE_API_KEY`        | Gemini API key.                                                |
| `VLLM_API_KEY`          | Bearer for the local vLLM server (if you set one).             |
| `ADAPTYV_API_TOKEN`     | Adaptyv Foundry token for wet-lab submission.                  |
| `PAPERCLIP_TOKEN`       | Enable the optional `knowledge.paperclip` MCP forwarder.       |

## Slash commands

Type `/` in the chat to see the menu. Highlights:

- `/plan approve` · `/plan cancel` — gate the agent at the plan checkpoint.
- `/model <id>` — switch model (and provider) mid-session.
- `/tools` · `/install` · `/uninstall` · `/doctor` — manage local tools.
- `/modal deploy` — push the tool registry to Modal.
- `/auth <provider>` — store an API key in the OS keyring.
- `/skills list|show|new|edit|validate|reset|path` — manage skills.
- `/config edit|reset|validate|path` — manage config assets.
- `/reload` — pick up edits to `config.toml` / `models.toml` without
  restarting.
- `/keys` · `/help` — keybindings and command reference.
- `/quit` — save and exit.

## Roadmap

- **v0.5 "Public alpha"** *(current)*: cleanup pass, public README, version
  alignment.
- **v0.4 "Closing the loop"** *(in progress)*: TUI redesign with a
  design-token palette, slash-command autocomplete, animated thinking
  indicator, and a context-meter status footer. Adaptyv wet-lab integration
  and the antibody/enzyme tracks are still to come.
- **v0.3 "Plan from target"** *(done)*: free knowledge stack, per-project
  literature corpus, the structured `DesignPlan` and `/plan` view, the Google
  Gemini provider, and the `pkg/proteinio` FASTA/PDB/mmCIF helpers.
- **v0.2 "Real designs"** *(done)*: SQLite persistence, async jobs system, uv
  + Modal backends, BindCraft/RFdiffusion/ProteinMPNN with ipSAE scoring, the
  jobs + designs TUI panels, and in-TUI environment setup.
- **v0.1 "Hello, sequence"** *(done)*: chat TUI, agent loop, and `fold.esmfold`.

See [`docs/SPECS.md`](docs/SPECS.md) for the full implementation spec and
[`docs/DESIGN.md`](docs/DESIGN.md) for the visual identity.

## Contributing

This is an early-stage alpha. Issues, bug reports, and PRs are welcome —
expect rough edges and a fast-moving codebase. Before opening a PR:

```sh
go vet ./...
go test ./...
```

If you're adding a tool, follow the existing adapters under
`internal/tools/` and `internal/backends/local/`. Tool docs live in
[`docs/tools/`](docs/tools/).

## License

[AGPL-3.0-or-later](LICENSE). If you run a modified fova as a network service,
the AGPL requires you to make your modifications available.
