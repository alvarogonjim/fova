# LigandMPNN

[LigandMPNN](https://github.com/dauparas/LigandMPNN) is the Baker lab's
ligand-aware extension of ProteinMPNN: it samples protein sequences
conditioned on small-molecule and metal context in addition to the
backbone. The same repo also ships the classic ProteinMPNN, SolubleMPNN,
two membrane variants, and a side-chain packing model — a total of 15
checkpoints across five model families. fova runs LigandMPNN inside a
container built from the NGC PyTorch base
(`nvcr.io/nvidia/pytorch:25.04-py3`) so the sm_121 kernels the GB10 needs
come for free; per-tool venvs cannot deliver them, and upstream's
`requirements.txt` pins `torch==2.2.1`, which would clobber the working
build.

**Disk + GPU memory.** The built image is ~1.5 GB (LigandMPNN source +
numpy/scipy/ProDy/ml-collections on top of the NGC base). Model weights
add another ~120 MB on the host under `~/.fova/models/ligandmpnn/`
(15 `.pt` files; the heaviest is the side-chain packing model at ~14 MB,
the rest are 6-11 MB each). The weights cache survives `/uninstall` so
reinstalls are fast. LigandMPNN itself is light on GPU memory — a
typical sequence-design pass against a 200-residue protein fits in 2-4
GB of VRAM, and the side-chain packing model peaks around 6-8 GB. CPU
inference is supported (and used by the upstream tests) at the cost of a
~30x speed penalty.

**Weight URLs.** Hosted on the Baker lab CDN at
`https://files.ipd.uw.edu/pub/ligandmpnn/`. The full list lives in
`internal/backends/local/tools.toml` under
`[[tools.ligandmpnn.weights]]` and is fetched once by the post-install
hook in `Installer.installContainer` via `models_cache.EnsureWeights`.
The set mirrors the active downloads in upstream's `get_model_params.sh`:

- ProteinMPNN (num_edges=48, noise 0.02/0.10/0.20/0.30): 4 files
- LigandMPNN (num_edges=32, atom_context=25, noise 0.05/0.10/0.20/0.30): 4 files
- SolubleMPNN (num_edges=48, noise 0.02/0.10/0.20/0.30): 4 files
- Global / per-residue membrane MPNN (num_edges=48, noise 0.20): 2 files
- LigandMPNN side-chain packing (atom_context=16, noise 0.02): 1 file

The checkpoints are bind-mounted at `/models` at run time and selected
via `--checkpoint_<model_type>` flags on the entrypoint (e.g.
`--checkpoint_ligand_mpnn /models/ligandmpnn_v_32_010_25.pt`).

**Open question for the maintainer.** The upstream CDN does not publish
SHA256 sums for the `.pt` files, so the `WeightSpec` entries in
`tools.toml` have `sha256 = ""`. `models_cache.EnsureWeights` treats an
empty checksum as a presence-only check (per its existing contract —
see `models_cache.go::verifyChecksum`), which means a corrupted download
will not be caught. Two paths forward: (1) compute SHA256s of the live
files once at v0.7.0 release time and bake them into the recipe, or
(2) ask the Baker lab to publish a manifest. Filed as a v0.7 follow-up,
mirroring the same open question on rfdiffusion.

**Typical runtime.** First-time install: 1-2 minutes for the image build
on a warm NGC base + 20-40 seconds for the weights download (~120 MB at
broadband). A single ligand-conditioned sequence smoke test takes a few
seconds. A typical production run (sample 8 sequences against a
ligand-bound active site) lands in well under a minute on the GB10.

**Smoke test.** Defined in the recipe's `smoke_test` field. Runs the
shared `_base/verify_gpu.py` GPU assertion (inlined as
`python -c "import torch; assert torch.cuda.is_available()"`) followed
by the upstream-default `run.py --pdb_path inputs/1BC8.pdb --checkpoint_protein_mpnn /models/proteinmpnn_v_48_020.pt`
invocation with a single sequence sampled. Confirms (a) the container
sees the GPU, (b) `ligandmpnn` imports, and (c) the cached
`proteinmpnn_v_48_020.pt` loads cleanly from `/models`.
