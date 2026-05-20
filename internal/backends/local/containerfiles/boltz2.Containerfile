# Boltz-2 — biomolecular structure + binding-affinity prediction (Bug 20).
#
# Image lands at ~10 GB after pip resolves; weights (~6 GB) live OUTSIDE
# the image, downloaded onto the host at install time by Installer's
# post-build hook (models_cache.EnsureWeights) and bind-mounted at
# /models/boltz2 on each run. Re-builds therefore do not refetch weights.
#
# Upstream:   https://github.com/jwohlwend/boltz
# CLI entry:  `boltz` (script declared in upstream pyproject — boltz.main:cli)
# Subcommand: `boltz predict <input.yaml> --out_dir <dir> --cache /models/boltz2`
#
# Keep this image disjoint from chai1.Containerfile per the v0.7 spec — both
# predict structures but their torch/CUDA + Python dep sets diverge in
# practice (cuequivariance for boltz vs. chai_lab's own stack for chai1).
FROM nvcr.io/nvidia/pytorch:25.04-py3

WORKDIR /opt

# git is already in the NGC base, but install it explicitly so a stripped
# base image fails fast at build time instead of after pip resolution.
RUN apt-get update \
    && apt-get install -y --no-install-recommends git \
    && rm -rf /var/lib/apt/lists/*

# Float on main for now; upstream tags 2.2.x in pyproject but doesn't push
# git tags consistently. Smoke-test failures (rather than silent rebuilds)
# surface upstream drift.
RUN git clone --depth 1 https://github.com/jwohlwend/boltz.git boltz2

# Editable install matches upstream's "daily-updates" path from their README.
# `[cuda]` adds the NVIDIA cuEquivariance kernels Boltz-2 prefers on Hopper/
# Blackwell. Torch is already in the NGC base — do NOT reinstall it (the
# upstream pin `torch>=2.2` would otherwise replace the sm_121 build).
#
# --only-binary on cuequivariance wheels makes a missing aarch64 wheel fail
# loudly instead of silently triggering a CUDA-toolkit-bound source build.
# cu-equiv has cp312 aarch64 wheels from 0.5.0+; this is a safety net.
# --retries / --timeout harden transient PyPI / index.nvidia.com stalls.
# Blank NGC's pip constraint for the install. boltz2's sdist build-deps
# include packages that pin numpy<X, which conflict with NGC's pinned
# numpy==1.26.4 and fail at ResolutionImpossible. Same idiom as boltzgen
# / chai1 / ligandmpnn / rfdiffusion — the constraint only matters for the
# isolated PEP-517 build env.
RUN : > /etc/pip/constraint.txt \
 && PIP_CONSTRAINT= pip install --no-cache-dir \
        --retries 5 --timeout 60 \
        --only-binary=cuequivariance_ops_cu12,cuequivariance_ops_torch_cu12 \
        -e "/opt/boltz2[cuda]"

# GPU reachability is verified at run time via `smoke_test` (tools.toml).
# torch.cuda.is_available() can only succeed under `podman run --gpus all`,
# so we don't even attempt the assert here.
COPY _base/verify_gpu.py /opt/_base/verify_gpu.py

WORKDIR /work
ENTRYPOINT ["boltz", "predict"]
