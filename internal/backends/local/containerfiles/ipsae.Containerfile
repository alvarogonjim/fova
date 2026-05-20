# ipSAE — scoring function for interprotein interactions in AlphaFold2/3 and
# Boltz models. CPU-only (pure Python + NumPy); we still FROM the NGC PyTorch
# base for homogeneity with the rest of the Proteus container fleet.
#
# Upstream: https://github.com/DunbrackLab/IPSAE  (single-file ipsae.py)
# License:  MIT.
#
# Phase 2 deliverable for Bug 22. Build: produces proteus/ipsae:v1.0.0.
FROM nvcr.io/nvidia/pytorch:25.04-py3

# Clone at a pinned-ish layer; if the upstream cuts a tagged release later we
# should swap to `git -C /opt/ipsae checkout <tag>` to make builds reproducible.
WORKDIR /opt
RUN git clone https://github.com/DunbrackLab/IPSAE ipsae

# The README only requires NumPy. NumPy already ships in the NGC base, so this
# is effectively a no-op upgrade — but installing it explicitly pins the
# contract and surfaces failures during build rather than at first invocation.
RUN pip install --no-cache-dir numpy

# All Proteus container runs mount the workspace at /work.
WORKDIR /work

# ipsae.py expects positional args:
#   <pae_file> <structure_file> <pae_cutoff> <dist_cutoff>
# The Runner appends those at invocation time.
ENTRYPOINT ["python", "/opt/ipsae/ipsae.py"]
