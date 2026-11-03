import argparse
import hashlib
import json
import shutil
from datetime import datetime
from pathlib import Path

from ultralytics import YOLO


SCRIPT_DIR = Path(__file__).resolve().parent
DATA_YAML = SCRIPT_DIR / "datasets" / "classroom_behavior" / "data.yaml"
RUNS_DIR = SCRIPT_DIR / "runs" / "train"
RESULTS_DIR = SCRIPT_DIR / "train_result"


def parse_args():
    parser = argparse.ArgumentParser(description="Train the classroom behavior YOLO11n detector.")
    parser.add_argument("--epochs", type=int, default=100)
    parser.add_argument("--imgsz", type=int, default=640)
    parser.add_argument("--batch", type=int, default=8)
    parser.add_argument("--device", default="0")
    parser.add_argument("--workers", type=int, default=4)
    parser.add_argument("--seed", type=int, default=42)
    return parser.parse_args()


def sha256(path):
    digest = hashlib.sha256()
    with path.open("rb") as stream:
        for chunk in iter(lambda: stream.read(1024 * 1024), b""):
            digest.update(chunk)
    return digest.hexdigest()


def main():
    args = parse_args()
    if not DATA_YAML.is_file():
        raise SystemExit(f"Dataset not found: {DATA_YAML}. Run 1-download-dataset.py first.")

    model = YOLO("yolo11n.pt")
    model.train(
        data=str(DATA_YAML),
        epochs=args.epochs,
        imgsz=args.imgsz,
        batch=args.batch,
        device=args.device,
        workers=args.workers,
        seed=args.seed,
        deterministic=True,
        patience=30,
        project=str(RUNS_DIR),
        name="yolo11n_classroom_behavior",
        exist_ok=False,
        plots=True,
    )

    run_dir = Path(model.trainer.save_dir)
    RESULTS_DIR.mkdir(parents=True, exist_ok=True)
    checkpoints = {}
    for name in ("best", "last"):
        source = run_dir / "weights" / f"{name}.pt"
        destination = RESULTS_DIR / f"yolo11n_classroom_behavior_{name}.pt"
        shutil.copy2(source, destination)
        checkpoints[name] = {"path": str(destination), "sha256": sha256(destination)}

    record = {
        "created_at": datetime.now().astimezone().isoformat(timespec="seconds"),
        "model": "YOLO11n",
        "dataset": "main-vdoys/student-dataset-demo v1",
        "data_yaml": str(DATA_YAML),
        "run_dir": str(run_dir),
        "arguments": vars(args),
        "seed": args.seed,
        "deterministic": True,
        "checkpoints": checkpoints,
    }
    record_path = RESULTS_DIR / "yolo11n_classroom_behavior_training.json"
    record_path.write_text(json.dumps(record, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")
    print(f"Training complete: {run_dir}")
    print(f"Best checkpoint: {RESULTS_DIR / 'yolo11n_classroom_behavior_best.pt'}")
    print(f"Training record: {record_path}")


if __name__ == "__main__":
    main()
