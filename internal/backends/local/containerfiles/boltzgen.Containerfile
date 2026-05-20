# BoltzGen — universal binder design via diffusion + Boltz-2 refolding.
#
# Upstream: https://github.com/HannesStark/boltzgen
# Paper:    https://hannes-stark.com/assets/boltzgen.pdf
# Bug:      docs/superpowers/specs/2026-05-20-v0.7-agent-ux-and-grounding.md
#           (added Phase 3 as the SPECS-blessed BindCraft alternative on
#           Grace CPU / aarch64, where BindCraft is blocked by the upstream
#           PyRosetta wheel gap)
#
# Multi-arch: BoltzGen is pure Python on top of PyTorch + HuggingFace; it
# has no PyRosetta dependency and runs on both x86_64 and aarch64 with the
# NGC PyTorch base.
#
# Model weights (~6 GB, including the Boltz-2 refolding stack) are NOT baked
# into the image: they're downloaded from HuggingFace on first run into
# /models (mounted from ~/.proteus/models/boltzgen/ at run time) via the
# HF_HOME env var below.

FROM nvcr.io/nvidia/pytorch:25.04-py3

WORKDIR /opt

# Stage the shared GPU smoke fragment for runtime use. torch.cuda.is_available()
# can only succeed under `podman run --gpus all`; the tools.toml `smoke_test`
# invokes verify_gpu.py at run time.
COPY _base/verify_gpu.py /opt/_base/verify_gpu.py

# BoltzGen install on the NGC PyTorch base — two aarch64-specific gotchas:
#
# 1) NGC ships /etc/pip/constraint.txt that hard-pins numpy==1.26.4. BoltzGen
#    0.3.2 hard-pins numpy==2.0.2 (and numba==0.61.0). pip exits at
#    ResolutionImpossible in ~5s. Blank the constraint and unset
#    PIP_CONSTRAINT for the install; we don't delete the file because the
#    NGC entrypoint references it.
# 2) `hydride` (a BoltzGen transitive) has no Linux-aarch64 wheel, so pip
#    falls back to a Cython sdist build that needs C/C++ toolchain headers
#    beyond what NGC ships (cmake, libhdf5-dev, libxml2-dev, libxslt1-dev,
#    libffi-dev, libssl-dev, libgl1).
RUN apt-get update && apt-get install -y --no-install-recommends \
        cmake \
        pkg-config \
        libhdf5-dev \
        libxml2-dev \
        libxslt1-dev \
        libffi-dev \
        libssl-dev \
        libgl1 \
    && rm -rf /var/lib/apt/lists/*

RUN : > /etc/pip/constraint.txt \
 && PIP_CONSTRAINT= pip install --no-cache-dir boltzgen==0.3.2

# HuggingFace cache for the Boltz-2 weights + BoltzGen's own checkpoints.
# /models is bind-mounted from ~/.proteus/models/boltzgen/ at run time so
# weights survive image rebuilds and `/uninstall`.
ENV HF_HOME=/models \
    HF_HUB_DOWNLOAD_TIMEOUT=600

WORKDIR /work
ENTRYPOINT ["boltzgen"]
