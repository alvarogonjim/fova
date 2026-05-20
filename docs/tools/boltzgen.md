# BoltzGen

**Upstream:** https://github.com/HannesStark/boltzgen
**Paper:** Stark et al., *BoltzGen: Toward Universal Binder Design* (2026) — https://hannes-stark.com/assets/boltzgen.pdf
**Method:** generative protein binder design; the model proposes binders and refolds them through Boltz-2 for structure verification.

## Why it's in v0.7

BindCraft is unavailable on aarch64 (Grace CPU / GB10) because upstream PyRosetta has no Python 3.12 aarch64 wheel. BoltzGen has no PyRosetta dependency and runs on both x86_64 and aarch64 with the NGC PyTorch base, so it's the SPECS-blessed alternative binder method on Grace. The `design-binder.md` skill in `internal/skills/builtin/` lists it as the primary path on aarch64 and the secondary path on x86_64 (where BindCraft and BoltzGen are both available).

## Install

```
/install boltzgen
```

Builds `fova/boltzgen:v0.3.2` from `internal/backends/local/containerfiles/boltzgen.Containerfile`. PyPI install only; no source clone or build step. Image size ~5 GB (NGC base + boltzgen + deps); first run downloads ~6 GB of HuggingFace weights into `~/.fova/models/boltzgen/` via `HF_HOME=/models` — those survive image rebuilds and `/uninstall`.

## Protocols

BoltzGen exposes several `--protocol` modes via the `boltzgen run` CLI:

| Protocol | Use case |
|---|---|
| `protein-anything` | Design proteins to bind proteins or peptides (the typical binder case). |
| `peptide-anything` | Design cyclic peptides or others to bind proteins. |
| `protein-small_molecule` | Design proteins to bind small molecules; includes binding-affinity prediction. |
| `antibody-anything` | Design antibody CDRs. |
| `nanobody-anything` | Design nanobody CDRs. |
| `protein-redesign` | Redesign / optimise existing proteins. |

## GPU + memory

Upstream benchmarks BoltzGen on A100 (~seconds per design); the GB10's Grace+Hopper combination provides similar headroom. Allocate ~24 GB VRAM for the `protein-anything` protocol with the default `--num_designs 50` warm-up run; production runs at `--num_designs 10000..60000` benefit from more.

## Quick start

```
/install boltzgen
# In a chat session:
"design 20 binders against PDB 1G13 chain A using BoltzGen"
```

Under the hood `boltzgen run <yaml> --output <dir> --protocol protein-anything --num_designs 50 --budget 10` is invoked inside the container; the `/work` mount is the design's output dir on the host.

## Known limitations

- First run downloads ~6 GB of weights from HuggingFace. Use `HF_HUB_DOWNLOAD_TIMEOUT` to extend the default timeout on slow networks (already set to 600s in the Containerfile).
- BoltzGen's diversity-optimisation filtering step takes ~15 s and can be rerun with different settings via `boltzgen run --steps filtering`.
- The MIT licence covers BoltzGen itself; the Boltz-2 refolding head is released under the same terms (verify upstream before commercial use).
