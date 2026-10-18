"""Download the pinned classroom behavior dataset from Roboflow Universe."""

from __future__ import annotations

import os
import time
from pathlib import Path
from urllib.parse import quote, quote_plus

import requests
import roboflow
from roboflow import Roboflow
from roboflow.config import API_URL
import yaml


API_KEY = os.environ.get("ROBOFLOW_API_KEY")
if not API_KEY:
    raise SystemExit(
        "Set ROBOFLOW_API_KEY in the environment before downloading the dataset."
    )

SCRIPT_DIR = Path(__file__).resolve().parent
DATASET_ROOT = SCRIPT_DIR / "datasets"
DATASET_NAME = "classroom_behavior"

# Roboflow Universe project: main-vdoys/student-dataset-demo, version 1.
# The published project lists reading, hand-raising, writing and Public Domain.


def _check_key_without_sdk_post(api_key: str, model: str, notebook: str, num_retries: int = 0):
    """Work around networks that abort the SDK's POST-only key check.

    Roboflow's later workspace/project/download calls still receive the API key.
    This GET confirms the pinned project is reachable; it does not claim to
    independently validate the key because the project is public.
    """
    response = requests.get(
        f"{API_URL}/main-vdoys/student-dataset-demo",
        params={"api_key": api_key},
        timeout=30,
    )
    if response.status_code != 200 or "project" not in response.json():
        raise RuntimeError(
            "Roboflow SDK key-check POST is blocked and the pinned project GET failed "
            f"(HTTP {response.status_code})."
        )
    return {"workspace": "main-vdoys"}


# Roboflow SDK 1.1.53 validates the key with POST /?api_key=..., which this
# Windows network currently aborts. Use the project GET path instead.
roboflow.check_key = _check_key_without_sdk_post

_requests_get = requests.get


def _get_with_transient_network_retries(*args, **kwargs):
    """Retry intermittent TLS disconnects seen on the Roboflow API host."""
    kwargs.setdefault("timeout", 45)
    for attempt in range(5):
        try:
            return _requests_get(*args, **kwargs)
        except (requests.exceptions.ConnectionError, requests.exceptions.Timeout):
            if attempt == 4:
                raise
            time.sleep(2 * (attempt + 1))


requests.get = _get_with_transient_network_retries


try:
    # Pass the workspace explicitly so the SDK does not make an extra
    # /{workspace} request, which this network also aborts intermittently.
    project = Roboflow(api_key=API_KEY).project(
        "student-dataset-demo", the_workspace="main-vdoys"
    )
    dataset = project.version(1).download(
        model_format="yolov11",
        location=str(DATASET_ROOT / DATASET_NAME),
    )
except Exception as exc:
    # Some SDK/network exceptions include the API URL, which carries the key.
    # Keep the actionable error while ensuring it cannot print the secret.
    safe_error = str(exc).replace(API_KEY, "[redacted]")
    safe_error = safe_error.replace(quote(API_KEY, safe=""), "[redacted]")
    safe_error = safe_error.replace(quote_plus(API_KEY, safe=""), "[redacted]")
    raise SystemExit(f"Roboflow dataset download failed: {safe_error}") from None

data_yaml = Path(dataset.location) / "data.yaml"
if not data_yaml.is_file():
    raise SystemExit(f"Roboflow download did not produce the expected file: {data_yaml}")

data = yaml.safe_load(data_yaml.read_text(encoding="utf-8")) or {}
names = data.get("names")
if isinstance(names, dict):
    class_names = [str(names[key]) for key in sorted(names, key=lambda item: int(item))]
elif isinstance(names, list):
    class_names = [str(name) for name in names]
else:
    raise SystemExit(f"No valid class names found in {data_yaml}")

normalize = lambda name: name.strip().lower().replace("_", "-").replace(" ", "-")
expected = {"hand-raising", "reading", "writing"}
if {normalize(name) for name in class_names} != expected:
    raise SystemExit(
        "Downloaded class mapping differs from the pinned project page: "
        f"{class_names}; inspect the source before training."
    )

labels_path = SCRIPT_DIR.parent / "model_deployment" / "classes.txt"
labels_path.parent.mkdir(parents=True, exist_ok=True)
labels_path.write_text("\n".join(class_names) + "\n", encoding="utf-8")

print(f"{DATASET_NAME} v1 downloaded to {dataset.location}")
print(f"Class order copied to {labels_path}: {class_names}")
print("Review the labels, sample boxes, and split integrity before training.")
