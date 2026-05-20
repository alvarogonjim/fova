# RFdiffusion2 — atom-level enzyme active site scaffolding via flow matching.
# Upstream: https://github.com/RosettaCommons/RFdiffusion2 (commit pinned via ARG).
#
# Notes for the maintainer (Phase 3 GB10 acceptance):
#   * Upstream ships an Apptainer .sif (rf_diffusion/exec/bakerlab_rf_diffusion_aa.sif)
#     plus model weights via `python setup.py`. We do NOT ship the .sif from
#     inside this image — nesting Apptainer in podman is fragile and we already
#     supply the NGC PyTorch runtime as the base. The Python deps installed
#     below mirror what the .sif provides for inference.
#   * Upstream's conda env (envs/cuda124_env.yml) pulls `pyrosetta` from
#     conda.rosettacommons.org. PyRosetta is required at inference time for
#     the idealization step; we install the pip-distributed `pyrosetta-installer`
#     bootstrap (same approach BindCraft uses). Users who hit a PyRosetta
#     license-acceptance prompt should re-run with `PYROSETTA_NO_PROMPT=1`.
#   * Weights (Base = RFD_173.pt, RFD_140.pt, plus LigandMPNN bundled weights)
#     live under /models/rfdiffusion2/ at run time, mounted from the host's
#     ~/.proteus/models/rfdiffusion2/ cache (EnsureWeights).

FROM nvcr.io/nvidia/pytorch:25.04-py3

ARG RFDIFFUSION2_GIT_REF=main

WORKDIR /opt

# Fail fast on aarch64 (Grace CPU / GB10). RFdiffusion2 needs PyRosetta for the
# inference-time idealization step, and upstream Graylab publishes aarch64
# PyRosetta only for Python 3.9 and 3.10 (no Python 3.12 aarch64 wheel; no
# cxx11thread.serialization aarch64 build). The NGC PyTorch base ships
# Python 3.12 + sm_121-capable CUDA — dropping to Python 3.10 would lose GPU
# support. Track upstream:
#   https://github.com/RosettaCommons/PyRosetta
# Workaround on Grace: use design.rfdiffusion + design.proteinmpnn (the
# RFdiffusion v1 + ProteinMPNN combo, both run on aarch64 without PyRosetta).
RUN if [ "$(uname -m)" != "x86_64" ]; then \
      echo "" >&2 ; \
      echo "ERROR: RFdiffusion2 is unavailable on $(uname -m) in Proteus v0.7." >&2 ; \
      echo "" >&2 ; \
      echo "RFdiffusion2 needs PyRosetta for inference-time idealization, and" >&2 ; \
      echo "upstream Graylab does not yet publish a Python 3.12 aarch64" >&2 ; \
      echo "PyRosetta wheel (only py39/py310)." >&2 ; \
      echo "" >&2 ; \
      echo "Fallback on Grace CPU: use design.rfdiffusion + design.proteinmpnn" >&2 ; \
      echo "(both run on aarch64 without PyRosetta)." >&2 ; \
      echo "" >&2 ; \
      exit 1 ; \
    fi

# System deps: git for the clone; build-essential is already in the NGC base.
RUN apt-get update \
    && DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends \
        git ca-certificates \
    && rm -rf /var/lib/apt/lists/*

# Clone upstream at the requested ref. Using --depth=1 keeps the image lean;
# v0.7.0 doesn't pin a tag because upstream is still pre-1.0 ("under construction"
# per their README banner). Bump the ARG when upstream cuts a release tag.
RUN git clone --depth 1 --branch ${RFDIFFUSION2_GIT_REF} \
        https://github.com/RosettaCommons/RFdiffusion2 /opt/rfdiffusion2

WORKDIR /opt/rfdiffusion2

# Install the pip-side of the cuda124 environment file. The NGC 25.04 base
# already ships a CUDA-12.x toolchain + a matching PyTorch wheel, so we skip
# the conda-only pieces (pytorch=2.4.0, pytorch-cuda=12.4, openbabel, dgl).
# Pin pip to the upstream-declared deps; let the NGC base's torch satisfy
# the framework-level requirement.
RUN python -m pip install --no-cache-dir --upgrade pip \
    && python -m pip install --no-cache-dir \
        "numpy<2" matplotlib \
    && python -m pip install --no-cache-dir -r envs/requirements_cuda124.txt

# PyRosetta via the pip-distributed installer (same path BindCraft uses).
# `serialization=True` enables the cached-state mode the inference pipeline
# relies on for repeated motif scaffolding.
RUN python -m pip install --no-cache-dir pyrosetta-installer \
    && python -c "import pyrosetta_installer; pyrosetta_installer.install_pyrosetta(serialization=True)"

# Make the repo importable without per-user PYTHONPATH gymnastics (the upstream
# README documents this explicitly).
ENV PYTHONPATH=/opt/rfdiffusion2

# GPU reachability is verified at run time via `smoke_test` (tools.toml).
# torch.cuda.is_available() can only succeed under `podman run --gpus all`,
# so we don't even attempt the assert here.
COPY _base/verify_gpu.py /opt/_base/verify_gpu.py

WORKDIR /work

# Default entrypoint runs the inference pipeline; callers (Proteus runner) pass
# Hydra-style flags as a config name + sweep overrides, e.g.:
#     python rf_diffusion/benchmark/pipeline.py --config-name=open_source_demo \
#       sweep.benchmarks=active_site_unindexed_atomic_partial_ligand
ENTRYPOINT ["python", "/opt/rfdiffusion2/rf_diffusion/benchmark/pipeline.py"]
