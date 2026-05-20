"""Proteus Modal app: GPU functions for the protein-design tools.

Deploy with `proteus modal deploy` (which runs `modal deploy` on this file).
Each tool gets an isolated image built from the same uv-based install recipe
the local backend uses, plus a Modal @app.function. A single POST web endpoint
dispatches a {"tool": ..., "input": {...}} request to the matching function so
the Go client only needs one URL.
"""

import subprocess

import modal

app = modal.App("proteus-tools")


def _tool_image(python_version: str, install_commands: list[str]) -> modal.Image:
    """Build a Debian image with uv and a tool installed via its recipe."""
    return (
        modal.Image.debian_slim(python_version=python_version)
        .apt_install("git", "wget", "curl")
        .run_commands(["pip install uv"] + install_commands)
    )


rfdiffusion_image = _tool_image(
    "3.10",
    [
        "git clone https://github.com/RosettaCommons/RFdiffusion /opt/rfdiffusion",
        "uv venv --python 3.10 /opt/rfdiffusion/.venv",
        "uv pip install --python /opt/rfdiffusion/.venv/bin/python "
        "--torch-backend=auto torch==2.4.1",
        "uv pip install --python /opt/rfdiffusion/.venv/bin/python -e /opt/rfdiffusion",
        "uv pip install --python /opt/rfdiffusion/.venv/bin/python "
        "-e /opt/rfdiffusion/env/SE3Transformer",
    ],
)

bindcraft_image = _tool_image(
    "3.10",
    [
        "git clone https://github.com/martinpacesa/BindCraft /opt/bindcraft",
        "uv venv --python 3.10 /opt/bindcraft/.venv",
        "uv pip install --python /opt/bindcraft/.venv/bin/python "
        "--torch-backend=auto torch==2.4.1",
        "uv pip install --python /opt/bindcraft/.venv/bin/python "
        "-r /opt/bindcraft/requirements.txt",
    ],
)

proteinmpnn_image = _tool_image(
    "3.10",
    [
        "git clone https://github.com/dauparas/ProteinMPNN /opt/proteinmpnn",
        "uv venv --python 3.10 /opt/proteinmpnn/.venv",
        "uv pip install --python /opt/proteinmpnn/.venv/bin/python "
        "--torch-backend=auto torch==2.4.1",
        "uv pip install --python /opt/proteinmpnn/.venv/bin/python numpy",
    ],
)

ipsae_image = _tool_image(
    "3.11",
    [
        "git clone https://github.com/DunbrackLab/IPSAE /opt/ipsae",
        "uv venv --python 3.11 /opt/ipsae/.venv",
        "uv pip install --python /opt/ipsae/.venv/bin/python numpy biopython",
    ],
)


def _run(venv_python: str, args: list[str]) -> dict:
    """Run a tool subprocess and return a structured result."""
    proc = subprocess.run(
        [venv_python, *args], capture_output=True, text=True, check=False
    )
    return {
        "exit_code": proc.returncode,
        "stdout": proc.stdout,
        "stderr": proc.stderr,
    }


@app.function(image=rfdiffusion_image, gpu="A10G", timeout=3600)
def run_rfdiffusion(spec: dict) -> dict:
    return _run(
        "/opt/rfdiffusion/.venv/bin/python",
        ["/opt/rfdiffusion/scripts/run_inference.py", *spec.get("args", [])],
    )


@app.function(image=bindcraft_image, gpu="A10G", timeout=3600)
def run_bindcraft(spec: dict) -> dict:
    return _run(
        "/opt/bindcraft/.venv/bin/python",
        ["/opt/bindcraft/bindcraft.py", "--settings", spec.get("settings", "")],
    )


@app.function(image=proteinmpnn_image, gpu="A10G", timeout=1800)
def run_proteinmpnn(spec: dict) -> dict:
    return _run(
        "/opt/proteinmpnn/.venv/bin/python",
        ["/opt/proteinmpnn/protein_mpnn_run.py", *spec.get("args", [])],
    )


@app.function(image=ipsae_image, timeout=600)
def run_ipsae(spec: dict) -> dict:
    return _run(
        "/opt/ipsae/.venv/bin/python",
        ["/opt/ipsae/ipsae.py", *spec.get("args", [])],
    )


_DISPATCH = {
    "design.rfdiffusion": run_rfdiffusion,
    "design.bindcraft": run_bindcraft,
    "design.proteinmpnn": run_proteinmpnn,
    "score.ipsae": run_ipsae,
}


@app.function()
@modal.fastapi_endpoint(method="POST")
def run(payload: dict) -> dict:
    """Dispatch {"tool": ..., "input": {...}} to the matching tool function."""
    tool = payload.get("tool", "")
    fn = _DISPATCH.get(tool)
    if fn is None:
        return {"error": f"unknown tool {tool!r}"}
    return fn.remote(payload.get("input", {}))
