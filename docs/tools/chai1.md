# Chai-1

Chai-1 ([chaidiscovery/chai-lab](https://github.com/chaidiscovery/chai-lab),
Apache-2.0) is a multi-modal foundation model for biomolecular structure
prediction. fova installs `chai_lab==0.6.1` from PyPI into a
container image built on the shared NGC PyTorch base
(`nvcr.io/nvidia/pytorch:25.04-py3`).

**Disk:** the image itself is ~6.5 GB (NGC base + chai_lab deps); model
weights add another ~1.3 GB and are downloaded once at install time into
`~/.fova/models/chai1/` (seven files served unauthenticated from
`chaiassets.com`). The weights cache is preserved across `/uninstall` so
re-installs are fast.

**GPU memory:** Chai-1 requires a CUDA GPU with bfloat16 support. Chai
recommends A100/H100/L40S 48 GB+ for medium complexes; an RTX 4090 or
NVIDIA GB10 has enough memory for short monomers and small heterodimers.
A 30-residue monomer fold (the smoke-test fixture) runs in roughly 30–60
seconds on an L40S after the first call warms the model.

**Authentication:** none. Chai's public v1 weights are pulled from
`chaiassets.com` without a token. `HF_TOKEN` is declared in the recipe
but **left empty** — set it on the host *only* if you want to pull
alternate Chai checkpoints from HuggingFace. The token is forwarded into
the container at run time and is **never** baked into the image.

**Entry point:** `chai-lab fold <input.fasta> <output_dir>` (equivalent
to `python -m chai_lab.main fold ...`). Smoke test: `python
/opt/_base/verify_gpu.py && chai-lab fold /work/chai1_smoke.fasta
/work/out`.
