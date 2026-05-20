# Proteus

A terminal UI agent for de novo protein design. See `docs/SPECS.md`.

## Build

    make build
    ./bin/proteus

## Status

In development — `v0.4.0-dev`.

- **v0.4 "Closing the loop"** (in progress): the modern TUI redesign — a
  design-token palette, a bordered input with slash-command autocomplete, an
  animated thinking indicator, tree-connected tool traces, and a status footer
  with a context meter. The Adaptyv wet-lab integration and the antibody /
  enzyme tracks are still to come.
- **v0.3 "Plan from target"**: the free knowledge stack (Europe PMC, OpenAlex,
  Semantic Scholar, bioRxiv, Crossref, UniProt, RCSB PDB, InterPro, web search /
  fetch), a per-project literature corpus, a structured `DesignPlan` with the
  `/plan` view, the Google Gemini provider, and `pkg/proteinio` FASTA / PDB /
  mmCIF helpers.
- **v0.2 "Real designs"**: SQLite persistence, an async jobs system, the
  uv-based local backend and the Modal backend, the BindCraft / RFdiffusion /
  ProteinMPNN design tools with ipSAE scoring, the jobs + designs TUI panels,
  and in-TUI environment setup (`/install`, `/uninstall`, `/tools`, `/doctor`,
  `/modal deploy`).
- **v0.1 "Hello, sequence"**: the chat TUI, the agent loop, and `fold.esmfold`.
