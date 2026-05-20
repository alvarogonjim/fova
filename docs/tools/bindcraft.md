# BindCraft

Container-mode tool for protein binder design
([martinpacesa/BindCraft](https://github.com/martinpacesa/BindCraft)).

The image is built on the shared NGC PyTorch base
(`nvcr.io/nvidia/pytorch:25.04-py3`) and bakes in **PyRosetta** at build
time via `pyrosetta-installer`, so the cxx11thread.serialization wheel
never has to be fetched or compiled at install time on the host. Expect
**~10 GB** of disk for the image (BindCraft itself is small; PyRosetta is
~3 GB, the NGC base ~4 GB, plus the JAX/CUDA Python wheels and other
deps). Initial `/install bindcraft` runs **~15-20 minutes on a fast
network** end-to-end — most of that is the PyRosetta download plus
`pip install jax[cuda12]` and the ColabDesign clone. A real binder run
needs a GPU with **~16 GB+ of VRAM** (BindCraft pushes JAX/AF2 to the
multimer limit). AlphaFold2 weights are NOT in the image; they're
downloaded separately via the `alphafold_params` data asset (~5 GB) and
bind-mounted at run time. The pip list in `bindcraft.Containerfile` is
locked against the live upstream `install_bindcraft.sh` (verified
2026-05-20) — if upstream drifts, re-verify before re-locking the
recipe.
