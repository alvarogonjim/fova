# Proteus

A terminal UI agent for de novo protein design. See `docs/SPECS.md`.

## Build

    make build
    ./bin/proteus

## Status

v0.2 — "Real designs": SQLite persistence, an async jobs system, the uv-based
local backend and the Modal backend, the BindCraft / RFdiffusion / ProteinMPNN
design tools with ipSAE scoring, and the jobs + designs TUI panels. Environment
setup runs inside the TUI as slash commands — `/install`, `/uninstall`,
`/tools`, `/doctor`, `/modal deploy`.

Previously: v0.1 — "Hello, sequence": chat TUI, agent loop, `fold.esmfold`.
