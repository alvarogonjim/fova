# Boltz-2

[Boltz-2](https://github.com/jwohlwend/boltz) is a biomolecular foundation
model that jointly predicts complex structure and binding affinity (the
successor to Boltz-1, claiming AlphaFold3-class structural accuracy plus
FEP-class affinity in a single forward pass). fova runs it inside a
container built from the NGC PyTorch base
(`nvcr.io/nvidia/pytorch:25.04-py3`) so the GB10's sm_121 kernels come for
free; per-tool venvs cannot deliver them. The image installs `boltz[cuda]`
in editable mode against a `--depth 1` upstream clone, which pulls
cuequivariance for hardware-accelerated equivariant ops on Hopper and
Blackwell.

**Disk + GPU memory.** Built image lands at ~10 GB (Boltz source + torch
extensions + cuequivariance + rdkit + pytorch-lightning on top of the NGC
base). Model weights add another ~6.13 GB on the host under
`~/.fova/models/boltz2/` (four files: `ccd.pkl` ~330 MB, `mols.tar` ~1.73
GB, `boltz2_conf.ckpt` ~2.13 GB, `boltz2_aff.ckpt` ~1.92 GB). The cache
survives `/uninstall` so reinstalls are fast. Steady-state VRAM is roughly
24 GB for a small monomer (rises with length and number of co-folded
chains/ligands); the GB10's 24 GB Blackwell tier just fits a typical
binding-affinity job, but anything past ~600 residues plus a ligand benefits
from `--use_msa_server` + smaller `recycle_steps`.

**Weight URLs.** Hosted on HuggingFace at
`boltz-community/boltz-1` (CCD) and `boltz-community/boltz-2` (everything
else). The full list lives in `internal/backends/local/tools.toml` under
`[[tools.boltz2.weights]]` and is fetched once by the post-install hook in
`Installer.installContainer` via `models_cache.EnsureWeights`. Files are
bind-mounted into the container at `/models/boltz2` at run time; the
adapter must pass `--cache /models/boltz2` to `boltz predict` so the
in-process downloader looks at the host cache rather than re-fetching into
`~/.boltz`.

**Open question for the maintainer.** Upstream does NOT publish SHA256
sums for any of the four weight files. The shipping downloader in
`src/boltz/main.py` uses `urllib.request.urlretrieve` with no checksum
verification, and HuggingFace exposes only its XetHub-CAS digest (an
internal content hash, not a stable SHA256 sidecar). All four
`[[tools.boltz2.weights]]` entries therefore set `sha256 = ""`, which
`models_cache.EnsureWeights` treats as a presence-only check. Two paths
forward at release time: (1) hash the files once on the GB10 and pin those
digests, or (2) ask the Boltz authors to publish a manifest. Filed as a
v0.7 follow-up alongside the matching rfdiffusion gap.

**Typical runtime.** First-time install: 8-12 minutes for the image build
on a warm NGC base (cuequivariance has prebuilt wheels for cu12 so no
JIT-compile penalty) + 3-4 minutes for the weights download (6 GB at
~30 MB/s, parallelisable but `EnsureWeights` is currently serial). A
30-residue monomer fold (the smoke test) takes ~40-60 s on a 24 GB GPU
once weights are cached. A typical production fold of a 200-residue
monomer lands in 2-4 minutes; a 30-residue binder + 200-residue target +
small-molecule ligand + affinity prediction is in the 6-10 minute range.

**Smoke test.** Defined in the recipe's `smoke_test` field. Runs the shared
`_base/verify_gpu.py` GPU assertion (inlined as `python -c "import torch;
assert torch.cuda.is_available()"`) followed by a 30-aa monomer fold from
a generated YAML (`msa: empty` to skip the MSA server, `--override` to
ignore any stale tmp) and an `ATOM`-record count over the produced PDB to
confirm the writer actually emitted atoms. Confirms (a) the container sees
the GPU, (b) `boltz` imports, (c) the cached `boltz2_conf.ckpt` loads
cleanly, and (d) the inference graph completes end-to-end.
