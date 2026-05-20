<div align="center">

```
  ┌─╮
  │ ╰──●
  ├─╮
  │ ╰─
  │
```

# fova

**design proteins in your terminal**

[![status](https://img.shields.io/badge/status-alpha-EF9F27?style=flat-square)](#status)
[![license](https://img.shields.io/badge/license-AGPLv3-3B6D11?style=flat-square)](LICENSE)
[![go](https://img.shields.io/badge/go-1.22+-3B6D11?style=flat-square)](https://go.dev)

</div>

---

Fova is a terminal agent for de novo protein design. It plans, runs, and ranks design jobs — and ships the survivors to a wet lab — from a single Go binary.

**Free by default.** No account needed. Local LLMs work out of the box. Paid LLMs and Adaptyv wet-lab submission are opt-in.

```sh
$ fova design --target PD-L1 --n 50
▸ planning            retrieved 12 papers · BindCraft + ProteinMPNN
▸ scaffolding         RFdiffusion · 200 backbones · 4m 12s
▸ sequence design     ProteinMPNN · 200 sequences · 2m 41s
▸ predict             AlphaFold3 · 1m 08s
▸ rank · ipSAE        shortlist: 47 designs · top score 0.84
▸ confirm wet-lab submission? [y/N]
```

---

## What fova does

- **Plans** design jobs from a natural-language target description, grounded in current literature (Europe PMC, OpenAlex, Semantic Scholar, bioRxiv).
- **Orchestrates** experimentally validated design tools: BindCraft, RFdiffusion, ProteinMPNN, AlphaFold3, ESMFold, and more.
- **Ranks** designs by ipSAE — the primary interface-quality metric in fova — with pLDDT, ipTM, and PAE as secondary signals.
- **Ships** small filtered shortlists (≤100 designs) to the bench via the Adaptyv Foundry API.
- **Tracks** full provenance: every design carries lineage from intent → tool versions → wet-lab result.

## Design principles

1. **Free by default.** Every feature works without a paid account. Paid features opt-in.
2. **Experimentally validated tools only.** Built-in tools have documented wet-lab success.
3. **Small, filtered shortlists.** Modern pipelines ship ≤100 designs to the bench, not 10,000.
4. **Type-safe tool I/O.** The LLM sees structured outputs, never raw stdout.
5. **Human-in-the-loop checkpoints.** Confirmation before any operation that's slow (>5 min), costly (>$1), or irreversible (wet-lab submission).
6. **Local-first, cloud-elastic.** Works offline with Ollama; scales out to your own Modal account for GPU.
7. **Provenance everywhere.** Every design has a full lineage record.
8. **Beautiful by default.** The terminal is a product surface.

---

## Install

```sh
# macOS / Linux
curl -fsSL https://fova.dev/install | sh

# Or via Homebrew
brew install fova

# Or download a binary
# https://github.com/<you>/fova/releases
```

Verify:

```sh
fova --version
# fova 0.5.0
```

