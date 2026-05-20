# ProteinMPNN (Bug 15)

ProteinMPNN ([dauparas/ProteinMPNN](https://github.com/dauparas/ProteinMPNN))
is the sequence-from-structure inverse-folding tool wired to
`design.proteinmpnn`. fova packages it as a self-contained OCI image
(`fova/proteinmpnn:v1.0.1`) built from the NGC PyTorch base
(`nvcr.io/nvidia/pytorch:25.04-py3`); the per-tool layer adds only `numpy`
and `scipy` on top of the upstream clone at `/opt/proteinmpnn`. **Model
weights ship inside the upstream repo** (`vanilla_model_weights/`,
`soluble_model_weights/`, `ca_model_weights/` — roughly 80 MB combined), so
there is no separate weight download and `weights_paths` in `tools.toml`
stays empty. Expect ~7 GB on disk after `/install proteinmpnn` (the NGC
base layer dominates; the per-tool layer is ~0.5 GB) and 2–4 GB of GPU
memory at inference time for typical single-chain designs of ≤300
residues. A first-time install on a warm NGC cache takes ~3 minutes; a
10-residue smoke design returns in under 30 s on the GB10. The smoke test
runs `python /opt/_base/verify_gpu.py` first (the shared
`torch.cuda.is_available()` assertion) and then `protein_mpnn_run.py
--help`, so a broken base image fails fast before the tool-specific call.
