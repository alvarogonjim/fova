# BindCraft — protein binder design (martinpacesa/BindCraft).
#
# Pip list locked against upstream install_bindcraft.sh (verified 2026-05-20).
# Conda packages that are OS-level (libgfortran5, ffmpeg) or notebook-only
# (jupyter) are intentionally dropped — they're either provided by the NGC
# PyTorch base or unused in headless inference. Everything else maps 1:1
# from the upstream conda spec to a pip equivalent.
#
# PyRosetta is baked in at build time via pyrosetta-installer (~3 GB of
# binary). This is deliberate — the conda PyRosetta channel is a recurring
# install-time failure point, so we resolve it once at image-build and
# never again at runtime. Build time on a fast network: ~15-20 minutes.

FROM nvcr.io/nvidia/pytorch:25.04-py3

WORKDIR /opt

# Fail fast on aarch64 (Grace CPU / GB10). BindCraft depends on PyRosetta,
# and upstream Graylab publishes aarch64 PyRosetta only for Python 3.9 and
# 3.10 (plain release; no cxx11thread.serialization aarch64 build). The NGC
# PyTorch base ships Python 3.12 + sm_121-capable CUDA — we cannot drop to
# Python 3.10 without losing GPU support. Track upstream:
#   https://github.com/RosettaCommons/PyRosetta (open issue for Py3.12 aarch64)
# Workaround on Grace: use SPECS' documented binder fallback
#   design.rfdiffusion + design.proteinmpnn (no PyRosetta needed).
RUN if [ "$(uname -m)" != "x86_64" ]; then \
      echo "" >&2 ; \
      echo "ERROR: BindCraft is unavailable on $(uname -m) in Proteus v0.7." >&2 ; \
      echo "" >&2 ; \
      echo "BindCraft requires PyRosetta, and upstream Graylab does not yet" >&2 ; \
      echo "publish a Python 3.12 aarch64 PyRosetta wheel (only py39/py310)." >&2 ; \
      echo "" >&2 ; \
      echo "Fallback on Grace CPU: use the SPECS-documented binder path" >&2 ; \
      echo "  design.rfdiffusion + design.proteinmpnn" >&2 ; \
      echo "(both run on aarch64 without PyRosetta)." >&2 ; \
      echo "" >&2 ; \
      echo "To run BindCraft itself, switch backend to Modal (x86_64 cloud)." >&2 ; \
      echo "" >&2 ; \
      exit 1 ; \
    fi

# Clone BindCraft. The repo is small (no LFS); a shallow clone keeps the
# image layer minimal and reproducible at the recorded ref.
RUN git clone --depth 1 https://github.com/martinpacesa/BindCraft bindcraft

# Stage the shared GPU smoke fragment for runtime use. torch.cuda.is_available()
# can only succeed under `podman run --gpus all`; the tools.toml `smoke_test`
# invokes verify_gpu.py at run time (NOT build time, where there is no GPU).
COPY _base/verify_gpu.py /opt/_base/verify_gpu.py

# Core Python deps mirroring upstream install_bindcraft.sh:
#   pandas matplotlib 'numpy<2.0.0' biopython scipy pdbfixer seaborn
#   tqdm fsspec py3dmol chex dm-haiku 'flax<0.10.0' dm-tree joblib
#   ml-collections immutabledict optax
# JAX with the CUDA 12 plugin (NGC base ships CUDA 12.x), pinned to the
# upstream range ('jax>=0.4,<=0.6.0').
RUN pip install --no-cache-dir \
    'numpy<2.0.0' \
    pandas \
    matplotlib \
    biopython \
    scipy \
    pdbfixer \
    seaborn \
    tqdm \
    fsspec \
    py3Dmol \
    chex \
    dm-haiku \
    'flax<0.10.0' \
    dm-tree \
    joblib \
    ml-collections \
    immutabledict \
    optax \
    'jax[cuda12]>=0.4,<=0.6.0'

# ColabDesign — upstream installs --no-deps to avoid clobbering the JAX
# pin above.
RUN pip install --no-cache-dir --no-deps \
    git+https://github.com/sokrypton/ColabDesign.git

# PyRosetta — bake the binary into the image at build time. The installer
# fetches the cxx11thread.serialization wheel from rosettacommons.org and
# pip-installs it; downstream code does `import pyrosetta` with no extra
# steps.
RUN pip install --no-cache-dir pyrosetta-installer && \
    python -c "import pyrosetta_installer; pyrosetta_installer.install_pyrosetta(serialization=True)" && \
    python -c "import pyrosetta; print('pyrosetta ok')"

# Sanity-import the BindCraft module so layer build fails here if any
# transitive dep is missing (this catches drift earlier than the smoke).
WORKDIR /opt/bindcraft
RUN python -c "import sys; sys.path.insert(0, '/opt/bindcraft'); from functions import generic_utils; print('bindcraft imports ok')"

WORKDIR /work
ENTRYPOINT ["python", "/opt/bindcraft/bindcraft.py"]
