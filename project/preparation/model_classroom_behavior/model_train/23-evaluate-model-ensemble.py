"""Evaluate a same-taxonomy PyTorch checkpoint ensemble with Ultralytics."""

from __future__ import annotations

import argparse
import json
from pathlib import Path

import torch
from ultralytics import YOLO
from ultralytics.nn.tasks import Ensemble


class ForwardAdapter(torch.nn.Module):
    """Adapt DetectionModel's keyword order to Ultralytics Ensemble's call."""

    def __init__(self, model: torch.nn.Module):
        super().__init__()
        self.model = model

    def forward(self, x, augment=False, profile=False):
        return self.model(x, profile=profile, augment=augment)


def parse_args():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--weights", nargs="+", type=Path, required=True)
    parser.add_argument("--data", type=Path, required=True)
    parser.add_argument("--split", choices=("val", "test"), default="val")
    parser.add_argument("--imgsz", type=int, default=768)
    parser.add_argument("--batch", type=int, default=1)
    parser.add_argument("--device", default="0")
    parser.add_argument("--workers", type=int, default=0)
    parser.add_argument("--iou", type=float, default=0.5)
    parser.add_argument("--project", type=Path, required=True)
    parser.add_argument("--name", required=True)
    return parser.parse_args()


def main():
    args = parse_args()
    if len(args.weights) < 2:
        raise ValueError("Provide at least two checkpoints for an ensemble.")
    missing = [str(path) for path in args.weights if not path.is_file()]
    if missing:
        raise FileNotFoundError(f"Missing checkpoints: {missing}")

    wrappers = [YOLO(str(path)) for path in args.weights]
    first = wrappers[0]
    if any(model.names != first.names for model in wrappers[1:]):
        raise ValueError("All ensemble members must have identical class-name mappings.")
    ensemble = Ensemble()
    ensemble.extend(ForwardAdapter(model.model) for model in wrappers)
    for attribute in ("stride", "yaml", "nc", "names", "end2end", "args"):
        if hasattr(first.model, attribute):
            setattr(ensemble, attribute, getattr(first.model, attribute))
    first.model = ensemble

    metrics = first.val(
        data=str(args.data),
        split=args.split,
        imgsz=args.imgsz,
        batch=args.batch,
        device=args.device,
        workers=args.workers,
        iou=args.iou,
        augment=False,
        plots=False,
        save_json=True,
        project=str(args.project.resolve()),
        name=args.name,
        exist_ok=False,
        verbose=True,
    )
    report = {
        "weights": [str(path.resolve()) for path in args.weights],
        "data": str(args.data.resolve()),
        "split": args.split,
        "imgsz": args.imgsz,
        "iou": args.iou,
        "augment": False,
        "metrics": {key: float(value) for key, value in metrics.results_dict.items() if key.startswith("metrics/")},
        "speed_ms_per_image": {key: float(value) for key, value in metrics.speed.items()},
    }
    output = Path(metrics.save_dir)
    (output / "ensemble_evaluation.json").write_text(json.dumps(report, indent=2, ensure_ascii=False) + "\n", encoding="utf-8")
    print(json.dumps(report, indent=2, ensure_ascii=False))


if __name__ == "__main__":
    main()
