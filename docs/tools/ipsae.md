# ipSAE

[ipSAE](https://github.com/DunbrackLab/IPSAE) is a single-file Python script
(`ipsae.py`) from the Dunbrack lab that scores interprotein interactions in
AlphaFold2, AlphaFold3, and Boltz models. fova runs it from
`fova/ipsae:v1.0.0`, a thin layer on top of the shared
`nvcr.io/nvidia/pytorch:25.04-py3` base that `git clone`s the upstream repo
into `/opt/ipsae` and installs `numpy` (the script's only declared
dependency). The image weighs in under 1 GB on top of the NGC base, fetches no
model weights, and runs on CPU — typical scoring of a 2- or 3-chain complex
finishes in a few seconds. Known quirks: ipsae is **CPU-only** so its smoke
test does not assert `torch.cuda.is_available()`, and the script exits with
status 1 when invoked with fewer than four positional arguments (including
`--help`) — that's by design upstream and is fine for our pre-Phase-3 smoke
hook, which only needs the interpreter and `numpy` to import.
