# RFantibody — structure-based de novo antibody / nanobody design.
#
# Upstream: https://github.com/RosettaCommons/RFantibody
#
# SE(3)-transformer pin (the fragile bit): upstream RFantibody DOES NOT install
# SE(3)-transformer from a separate git+ URL — it vendors NVIDIA's reference
# implementation in-tree at `include/SE3Transformer/se3_transformer` and bundles
# it into the `rfantibody` wheel via the hatch build (see pyproject.toml:
# `[tool.hatch.build.targets.wheel].packages` includes
# `"include/SE3Transformer/se3_transformer"`). There is therefore no external
# fork commit to pin; the vendored subtree IS the pin, and we lock it by
# pinning the RFantibody commit itself. The subtree was last touched in the
# initial commit `cf93b368696050c7ce1d60afcc5917b5c296bdab` (2025-01-22) and
# is unchanged through the pinned RFantibody SHA.
#
# Pinned RFantibody SHA: 8fe311415754e0276d1a39c87c57e69c88927a2d (2026-03-05
# main HEAD at v0.7 cut). Refresh by re-reading `gh api
# repos/RosettaCommons/RFantibody/commits/main` and updating both this comment
# and the ARG below.
#
# Torch / CUDA tension: NGC pytorch:25.04-py3 ships torch 2.6 + CUDA 12.4.
# RFantibody pins torch==2.3.* + cuda-python==11.8 (see pyproject.toml). The
# `uv sync` step below creates an isolated `.venv` with the upstream-pinned
# torch + DGL wheels, which override the NGC site-packages for tool execution.
# We still FROM the NGC tag because every other Proteus tool does (one base =
# one pulled image on the GB10) and because the NGC tag is what the v0.7 spec
# fixes as `BaseImage`. If Proteus ever drops to a slimmer base, RFantibody is
# the canary for the torch-2.3 + cu118 wheels: confirm DGL still has matching
# wheels at https://data.dgl.ai/wheels/torch-2.3/cu118/ before bumping.

FROM nvcr.io/nvidia/pytorch:25.04-py3

ARG RFANTIBODY_SHA=8fe311415754e0276d1a39c87c57e69c88927a2d

# Fail fast on aarch64 (Grace CPU / GB10). RFantibody's uv.lock pins
# `dgl==2.4.0+cu118` from data.dgl.ai/wheels/torch-2.3/cu118/, which only
# publishes x86_64 wheels. There is no aarch64 build in the cu118 channel
# (verified May 2026). On Grace, `uv sync --frozen` exits with:
#   "Distribution dgl==2.4.0+cu118 … can't be installed because the binary
#    distribution is incompatible with the current platform"
# Working around it would require patching RFantibody's uv.lock to source
# DGL from PyPI (no GPU-specific wheel) or rebuilding DGL from source on
# aarch64 (multi-hour, complex). Until upstream Rosetta/DGL publish an
# aarch64 wheel for cu118, RFantibody is unavailable on Grace.
# Track upstream:
#   https://github.com/RosettaCommons/RFantibody/issues
#   https://github.com/dmlc/dgl/issues  (aarch64 cu wheels)
RUN if [ "$(uname -m)" != "x86_64" ]; then \
      echo "" >&2 ; \
      echo "ERROR: RFantibody is unavailable on $(uname -m) in Proteus v0.7." >&2 ; \
      echo "" >&2 ; \
      echo "RFantibody's uv.lock pins dgl==2.4.0+cu118 from data.dgl.ai, which" >&2 ; \
      echo "only publishes x86_64 wheels. There is no aarch64 build in the" >&2 ; \
      echo "cu118 channel." >&2 ; \
      echo "" >&2 ; \
      echo "On Grace CPU, antibody/nanobody design is not supported in v0.7." >&2 ; \
      echo "Use the Modal backend (x86_64 cloud) for design.rfantibody." >&2 ; \
      echo "" >&2 ; \
      exit 1 ; \
    fi

# Install uv (RFantibody's pinned package manager) and the few apt deps the
# upstream Docker recipe pulls in.
RUN apt-get update && apt-get install -y --no-install-recommends \
        git curl ca-certificates make \
    && rm -rf /var/lib/apt/lists/* \
    && curl -LsSf https://astral.sh/uv/install.sh | sh
ENV PATH="/root/.local/bin:${PATH}"

WORKDIR /opt
RUN git clone https://github.com/RosettaCommons/RFantibody.git rfantibody \
    && cd rfantibody \
    && git checkout ${RFANTIBODY_SHA} \
    && git rev-parse HEAD > /opt/rfantibody.sha

# `uv sync` reads pyproject.toml + uv.lock and creates `.venv/` with torch
# 2.3 + cu118, DGL from upstream's pinned wheel, and the vendored SE(3)-
# transformer wheel built from include/SE3Transformer/. This is the only
# install path upstream tests; reproducing it by hand (a raw pip list) breaks
# under torch/DGL ABI drift.
WORKDIR /opt/rfantibody
RUN uv sync --frozen

# Shared GPU smoke fragment (see ../_base/verify_gpu.py) — copied in so
# `smoke_test` can run it without remounting the source tree.
COPY _base/verify_gpu.py /opt/_base/verify_gpu.py

# Workspace lives at /work; weights at /models (downloaded host-side via the
# v0.7 models cache and bind-mounted at runtime — never baked in).
WORKDIR /work
ENV PROTEUS_RFANTIBODY_WEIGHTS=/models

# Entry point is the `rfdiffusion` console script from the .venv (the primary
# antibody design step). The .venv binaries are on PATH after activation —
# we exec through `uv run` so the venv resolution is explicit and the
# entrypoint stays stable even if uv moves the venv path.
ENTRYPOINT ["uv", "run", "--project", "/opt/rfantibody", "rfdiffusion"]
