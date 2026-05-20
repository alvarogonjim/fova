# RFdiffusion

[RFdiffusion](https://github.com/RosettaCommons/RFdiffusion) is a diffusion
model for protein backbone generation (unconditional design, motif scaffolding,
binder design, symmetric oligomers, partial diffusion). fova runs it inside
a container built from the NGC PyTorch base (`nvcr.io/nvidia/pytorch:25.04-py3`)
so the sm_121 kernels the GB10 needs come for free; per-tool venvs cannot
deliver them.

**Disk + GPU memory.** The built image is ~6 GB (RFdiffusion source +
SE(3)-Transformer + dgl + e3nn on top of the NGC base). Model weights add
another ~4-5 GB on the host under `~/.fova/models/rfdiffusion/` (8
checkpoint files: `Base_ckpt.pt`, `Complex_base_ckpt.pt`,
`Complex_Fold_base_ckpt.pt`, `InpaintSeq_ckpt.pt`, `InpaintSeq_Fold_ckpt.pt`,
`ActiveSite_ckpt.pt`, `Base_epoch8_ckpt.pt`, `Complex_beta_ckpt.pt`). The
weights cache survives `/uninstall` so reinstalls are fast. RFdiffusion uses
roughly 8-16 GB of GPU VRAM for typical binder design jobs; longer
proteins or larger targets scale O(N^2).

**Weight URLs.** Hosted on the Baker lab CDN at
`http://files.ipd.uw.edu/pub/RFdiffusion/`. The full list lives in
`internal/backends/local/tools.toml` under `[[tools.rfdiffusion.weights]]`
and is fetched once by the post-install hook in
`Installer.installContainer` via `models_cache.EnsureWeights`. The
checkpoints are bind-mounted at `/models` at run time
(`inference.model_directory_path=/models`).

**Open question for the maintainer.** The upstream README does not publish
SHA256 sums for the checkpoint files, so the WeightSpec entries in
`tools.toml` have `sha256 = ""`. `models_cache.EnsureWeights` treats an
empty checksum as a presence-only check (per its existing contract — see
`models_cache.go::verifyChecksum`), which means a corrupted download will
not be caught. Two paths forward: (1) compute SHA256s of the live files
once at v0.7.0 release time and bake them into the recipe, or (2) ask the
Baker lab to publish a manifest. Filed as a v0.7 follow-up.

**Typical runtime.** First-time install: 6-10 minutes for the image build
on a warm NGC base + 2-3 minutes for the weights download (4-5 GB at
~30 MB/s). A 5-residue unconditional design smoke test takes ~30 s. A
production binder run (100-residue binder vs ~150 residue target, 10
designs) lands in 5-15 minutes depending on the GPU.

**Smoke test.** Defined in the recipe's `smoke_test` field. Runs the shared
`_base/verify_gpu.py` GPU assertion (inlined as `python -c "import torch;
assert torch.cuda.is_available()"`) followed by `run_inference.py
'contigmap.contigs=[5-5]' inference.num_designs=1
inference.model_directory_path=/models`. Confirms (a) the container sees
the GPU, (b) `rfdiffusion` imports, and (c) the cached `Base_ckpt.pt`
loads cleanly.
