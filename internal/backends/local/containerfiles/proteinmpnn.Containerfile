# ProteinMPNN container — sequence design from a fixed backbone.
#
# Layout:
#   /opt/proteinmpnn   ← upstream clone (https://github.com/dauparas/ProteinMPNN)
#                        Model weights ship inside the clone (vanilla_model_weights/,
#                        soluble_model_weights/, ca_model_weights/), so the image is
#                        self-contained. No external weights mount is required.
#   /work              ← per-job workspace, bind-mounted by the runner.
#
# Smoke layers:
#   1. verify_gpu.py  — shared NGC reachability assertion (torch.cuda.is_available()).
#      Runs BEFORE the tool-specific call so a broken base image fails fast.
#   2. protein_mpnn_run.py --help — proves argparse + imports load end-to-end.
#
# Do not run `podman build` against this from inside the worktree — GB10
# verification is the maintainer's Phase 3 step (see plan v0.7 §Phase 3).

FROM nvcr.io/nvidia/pytorch:25.04-py3

WORKDIR /opt

# Pin to main; upstream is effectively frozen but we record the ref for
# reproducibility. The recipe's `git_ref` in tools.toml mirrors this.
RUN git clone --depth=1 https://github.com/dauparas/ProteinMPNN /opt/proteinmpnn

# ProteinMPNN ships no requirements.txt / setup.py. PyTorch comes from the NGC
# base; ProteinMPNN itself only needs numpy + scipy on top.
RUN pip install --no-cache-dir numpy scipy

# Bake the shared GPU-reachability fragment into the image so the smoke test in
# tools.toml can layer (verify_gpu) → (--help) without depending on a host mount.
COPY _base/verify_gpu.py /opt/_base/verify_gpu.py

WORKDIR /work

# Entrypoint is the canonical inference script. `tools.toml` declares an
# entrypoint string for the Runner; this ENTRYPOINT only affects ad-hoc
# `podman run proteus/proteinmpnn:v1.0.1 …` invocations.
ENTRYPOINT ["python", "/opt/proteinmpnn/protein_mpnn_run.py"]
