# syntax=docker/dockerfile:1
#
# Chai-1 — multi-modal foundation model for biomolecular structure prediction.
# Upstream: https://github.com/chaidiscovery/chai-lab (Apache-2.0).
#
# Builds on the shared NGC PyTorch base used by every container-mode tool in
# Proteus (see internal/backends/local/registry.go BaseImage). Chai-1 and
# Boltz-2 share the same base but ship as separate images: their Python deps
# overlap but their CUDA/torch expectations diverge in practice.
#
# Weights live OUTSIDE the image. They are downloaded into the per-tool
# host cache (~/.proteus/models/chai1/) by `Installer.installContainer`'s
# post-build hook (models_cache.EnsureWeights) and bind-mounted at /models
# at run time. CHAI_DOWNLOADS_DIR points chai_lab at that mount so it skips
# its own download path. See chai_lab.utils.paths.download_if_not_exists.
#
# Auth: none. Weights are served unauthenticated from chaiassets.com. Chai
# Discovery has historically required HuggingFace tokens for some
# preview-channel artefacts; the public v1 weights this Containerfile uses
# do not. The HF_TOKEN env var is therefore declared OPTIONAL — if set on
# the host, the runner can forward it for users who want to pull alternate
# Chai checkpoints from HF.
FROM nvcr.io/nvidia/pytorch:25.04-py3

# Optional HF token for users who want to fetch alternate (private) Chai
# checkpoints. Not required for the default v1 weights. NEVER bake a token
# into the image — the runner injects it from the host env at run time.
ENV HF_TOKEN=""

# Chai-1 reads CHAI_DOWNLOADS_DIR to locate cached weights. We mount the
# per-tool host cache at /models, so point chai_lab there.
ENV CHAI_DOWNLOADS_DIR=/models

WORKDIR /opt

# Install Chai-1 on aarch64 (Grace CPU / GB10).
#
# chai_lab==0.6.1's PyPI metadata pins gemmi~=0.6.3 — no cp312 aarch64
# wheel exists in the 0.6.x line, so pip falls back to a multi-minute
# pybind11/C++ sdist build (gemmi has cp312 aarch64 wheels from 0.7.x onward;
# upstream chai-lab main has already moved to gemmi~=0.7.5 — chai-lab#415).
# Drop the unused pandas[aws,gcp] extras (chai_lab inference only touches
# `parquet`), pre-install compatible deps to bypass NGC's pip constraint
# (numpy==1.26.4), then install chai_lab itself with --no-deps and add
# back only the deps it actually needs. (Note: antipickle is a chai_lab
# trusted-source serialization dep; chai_lab handles its own input safety.)
RUN : > /etc/pip/constraint.txt \
 && PIP_CONSTRAINT= pip install --no-cache-dir \
        "numpy<2,>=1.26" \
        "gemmi>=0.7.5,<0.8" \
        "pandas[parquet]>=2.1,<2.3" \
 && PIP_CONSTRAINT= pip install --no-cache-dir --no-deps "chai_lab==0.6.1" \
 && PIP_CONSTRAINT= pip install --no-cache-dir \
        "antipickle==0.2.0" \
        "beartype>=0.18" \
        "biopython>=1.83" \
        "einops~=0.8" \
        "jaxtyping>=0.2.25" \
        "matplotlib" \
        "modelcif>=1.0" \
        "numba>=0.59,<0.61" \
        "pandera" \
        "rdkit~=2024.9.5" \
        "tmtools>=0.0.3" \
        "tqdm~=4.66" \
        "typer-slim~=0.12" \
        "typing-extensions"

# Bake the shared GPU smoke fragment into the image so the post-install
# smoke_test can exercise it at run time (GPU is not exposed to `podman
# build`, so we deliberately do NOT RUN this at build time).
COPY _base/verify_gpu.py /opt/_base/verify_gpu.py

# Sanity-check the chai_lab install and its CLI shim. No GPU needed.
RUN python -c "import chai_lab; from chai_lab.main import cli; print(f'chai_lab {chai_lab.__version__} ok')"

WORKDIR /work

# `chai-lab` is the entry-point script installed by chai_lab. Equivalent to
# `python -m chai_lab.main`. Usage: `chai-lab fold <input.fasta> <out_dir>`.
ENTRYPOINT ["chai-lab"]
