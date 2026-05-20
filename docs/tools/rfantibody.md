# rfantibody

RFantibody (RosettaCommons) is a structure-based de novo antibody / nanobody
design pipeline that chains an antibody-finetuned RFdiffusion, ProteinMPNN, and
an antibody-finetuned RoseTTAFold2. fova runs it inside the NGC PyTorch
container base (`nvcr.io/nvidia/pytorch:25.04-py3`) and resolves all upstream
deps via `uv sync` against the lockfile that ships in the repo. The pinned
RFantibody SHA is **`8fe311415754e0276d1a39c87c57e69c88927a2d`** (main HEAD on
2026-03-05); bump it by editing both `tools.toml` (`git_ref`) and
`containerfiles/rfantibody.Containerfile` (`ARG RFANTIBODY_SHA`). Upstream does
**not** install SE(3)-transformer from a separate git+ URL — it vendors NVIDIA's
implementation in-tree under `include/SE3Transformer/se3_transformer` and builds
it into the `rfantibody` wheel via hatch (`[tool.hatch.build.targets.wheel]`),
so the RFantibody SHA pin is the SE(3) pin; the vendored subtree itself has not
been touched since the project's initial commit (`cf93b368696050c7ce1d60afcc5917b5c296bdab`,
2025-01-22). Disk budget: ~18 GB image (NGC base ~7 GB + `uv sync` venv with
torch-2.3+cu118 wheels and DGL ~4 GB + repo) plus a separate ~5 GB weights
download (`RFdiffusion_Ab.pt`, `ProteinMPNN_v48_noise_0.2.pt`, `RF2_ab.pt`,
`RFab_noframework-nosidechains-5-10-23_trainingparamsadded.pt` from
`files.ipd.uw.edu/pub/RFantibody/` and a Zenodo mirror), which fova stages
under `~/.fova/models/rfantibody/` and bind-mounts at `/models`. Known
quirks: (1) the venv installs CUDA-11.8 torch wheels even though the NGC base
ships CUDA-12.4 PyTorch — the venv `site-packages` overrides via PATH and
upstream's lockfile is the source of truth; (2) RFdiffusion-Ab needs a target
PDB and framework scaffold both bind-mounted under `/work`; (3) typical install
time on a fresh GB10 is 8–15 minutes dominated by the DGL+torch wheel
downloads.
