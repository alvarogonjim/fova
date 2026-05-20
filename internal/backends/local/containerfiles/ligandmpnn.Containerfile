# LigandMPNN — sequence design conditioned on ligand context.
#
# Upstream: https://github.com/dauparas/LigandMPNN
# Bug: docs/superpowers/specs/2026-05-20-v0.7-agent-ux-and-grounding.md (Bug 17)
#
# Model checkpoints are NOT baked into the image: they're downloaded by the
# Go-side Installer's post-build hook via models_cache.EnsureWeights into
# ~/.proteus/models/ligandmpnn/ and bind-mounted at /models at run time.
# See Installer.installContainer and the [[tools.ligandmpnn.weights]] tables
# in tools.toml.

FROM nvcr.io/nvidia/pytorch:25.04-py3

WORKDIR /opt

# Pin would be nicer once upstream tags a release. LigandMPNN ships v1.0.0
# implicitly via the README; we float on main and surface drift through
# smoke-test failures rather than silent rebuilds.
RUN git clone --depth 1 https://github.com/dauparas/LigandMPNN ligandmpnn

# Minimal runtime deps: scipy + ProDy (PDB parser) + ml-collections. numpy is
# already in the NGC base. PyTorch already ships in the NGC base image — do
# NOT reinstall it; upstream requirements.txt pins torch==2.2.1, which would
# clobber the sm_121-capable build that the GB10 needs.
#
# Blank NGC's /etc/pip/constraint.txt + unset PIP_CONSTRAINT for the install:
# ProDy's sdist build-deps require numpy<1.24, which conflicts with NGC's
# pinned numpy==1.26.4 and fails the install at ResolutionImpossible in ~5s.
# The constraint only matters for the (isolated) PEP-517 build environment;
# the runtime numpy 1.26.4 in the NGC base is still what ProDy is wheeled
# against.
RUN : > /etc/pip/constraint.txt \
 && PIP_CONSTRAINT= pip install --no-cache-dir \
    scipy \
    prody \
    ml-collections

# GPU reachability is verified at run time via `smoke_test` (tools.toml).
# torch.cuda.is_available() can only succeed under `podman run --gpus all`,
# so we don't even attempt the assert here.
COPY _base/verify_gpu.py /opt/_base/verify_gpu.py

WORKDIR /work
ENTRYPOINT ["python", "/opt/ligandmpnn/run.py"]
