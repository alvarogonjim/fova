# RFdiffusion2

Atom-level enzyme active-site scaffolding via flow matching
(Ahern et al., 2025; bioRxiv). Upstream:
<https://github.com/RosettaCommons/RFdiffusion2>.

## Install notes

The image (`fova/rfdiffusion2:0.1.0`) FROMs `nvcr.io/nvidia/pytorch:25.04-py3`
and pip-installs the dependencies declared in upstream's
`envs/requirements_cuda124.txt` (Hydra, e3nn, RDKit, mdtraj, PyG, RAPIDS
cugraph-ops, etc.) plus `pyrosetta-installer`. Build time on a GB10 is roughly
**25–35 minutes** (most of it spent compiling the PyG `torch-scatter` /
`torch-sparse` wheels against the NGC CUDA toolchain). The built image is
~9 GB; model weights add another ~5 GB and live in
`~/.fova/models/rfdiffusion2/` (the four files listed by upstream's
`setup.py`: two diffusion checkpoints — `RFD_173.pt`, `RFD_140.pt` — and two
bundled LigandMPNN weights). Weights are fetched from
`https://files.ipd.uw.edu/pub/rfdiffusion2/` via the `rfdiffusion2_weights`
data asset and mounted at `/models/rfdiffusion2/` inside the container. Known
quirks: (1) upstream is still pre-1.0 ("under construction" banner in the
README), so the Containerfile clones `main` rather than a tagged release —
bump `ARG RFDIFFUSION2_GIT_REF` once a release is cut; (2) PyRosetta requires
license acceptance the first time `pyrosetta_installer` runs — pass
`PYROSETTA_NO_PROMPT=1` for unattended builds; (3) upstream's recommended
distribution is an Apptainer `.sif` plus a `setup.py` that fetches both `.sif`
and `.pt` files — we deliberately do NOT nest Apptainer inside podman, and
only fetch the `.pt` weights, since the NGC base already supplies the runtime.
