# RFdiffusion — protein structure generation (binders, motif scaffolding,
# symmetric design). Upstream: https://github.com/RosettaCommons/RFdiffusion
#
# Model weights are NOT baked into the image — they are downloaded once into
# ~/.proteus/models/rfdiffusion/ by Installer.installContainer's post-build
# hook (via models_cache.EnsureWeights) and bind-mounted at /models at run
# time. This keeps the image small and survives /uninstall cleanly.

FROM nvcr.io/nvidia/pytorch:25.04-py3

# Build-time deps for the RFdiffusion clone (git already in the NGC base, but
# pin explicitly so a missing tool fails fast).
RUN apt-get update \
    && apt-get install -y --no-install-recommends git \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /opt

# Pin to a known-working tag once upstream cuts one. Until then we float on
# main and surface upstream drift through smoke-test failures rather than
# silent rebuilds. RFdiffusion's setup.py advertises version 1.1.0.
RUN git clone --depth 1 https://github.com/RosettaCommons/RFdiffusion.git rfdiffusion

# Tool-specific Python deps. PyTorch is already in the NGC base — do NOT
# reinstall it (sm_121 kernels would be lost). e3nn / hydra-core / omegaconf
# are the RFdiffusion runtime; pyrsistent + decorator + pynvml are pulled in
# by SE3Transformer's requirements.txt; dgl is required by SE3Transformer.
# Blank NGC's /etc/pip/constraint.txt + unset PIP_CONSTRAINT for the install:
# several of these deps (and SE3Transformer below) have sdist build-time
# numpy/torch pins that conflict with NGC's runtime pin and fail at
# ResolutionImpossible. The constraint only matters for the (isolated)
# PEP-517 build env; the runtime stack from the NGC base is what's used.
RUN : > /etc/pip/constraint.txt \
 && PIP_CONSTRAINT= pip install --no-cache-dir \
    hydra-core \
    omegaconf \
    e3nn \
    pyrsistent \
    decorator \
    pynvml \
    dgl

# Install NVIDIA's SE(3)-Transformer (vendored under env/SE3Transformer) and
# the rfdiffusion package itself in editable mode so its scripts/ entrypoint
# resolves the local source tree.
RUN PIP_CONSTRAINT= pip install --no-cache-dir -e /opt/rfdiffusion/env/SE3Transformer \
 && PIP_CONSTRAINT= pip install --no-cache-dir -e /opt/rfdiffusion

# GPU reachability is verified at run time via `smoke_test` (tools.toml).
# torch.cuda.is_available() can only succeed under `podman run --gpus all`,
# so we don't even attempt the assert here.
COPY _base/verify_gpu.py /opt/_base/verify_gpu.py

WORKDIR /work
ENTRYPOINT ["python", "/opt/rfdiffusion/scripts/run_inference.py"]
