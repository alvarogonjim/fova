# Shared smoke fragment for container-mode tools. Each per-tool Containerfile
# may RUN this to verify the NGC base ships sm_121-capable PyTorch before the
# tool's own deps are installed.
import torch
assert torch.cuda.is_available(), "torch.cuda not available — NGC base image broken"
print(f"torch {torch.__version__} cuda {torch.version.cuda} ok")
