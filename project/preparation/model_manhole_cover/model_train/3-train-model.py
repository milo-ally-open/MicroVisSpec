import argparse
import hashlib
import json
import shutil
from datetime import datetime
from pathlib import Path

from ultralytics import YOLO


SCRIPT_DIR = Path(__file__).resolve().parent
DATA_YAML = SCRIPT_DIR / "datasets" / "manhole_cover" / "data.yaml"
RUNS_DIR = SCRIPT_DIR / "runs" / "train"
RESULTS_DIR = SCRIPT_DIR / "train_result"
DATASET_LABEL = "roboflow-universe-projects/manhole-cover-detection v1"


def parse_args():
    parser = argparse.ArgumentParser(description="Train the manhole cover YOLO11n detector.")
    parser.add_argument("--epochs", type=int, default=100)
    parser.add_argument("--imgsz", type=int, default=640)
    parser.add_argument("--batch", type=int, default=8)
    parser.add_argument("--device", default="0")
    parser.add_argument("--workers", type=int, default=4)
    parser.add_argument("--seed", type=int, default=42)
    parser.add_argument(
        "--run-name",
        default="yolo11n_manhole_cover",
        help="Unique training run name; derives run dir and train_result artifact names.",
    )
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
        name=args.run_name,
        exist_ok=False,
        plots=True,
    )

    run_dir = Path(model.trainer.save_dir)
    RESULTS_DIR.mkdir(parents=True, exist_ok=True)
    checkpoints = {}
    for name in ("best", "last"):
        source = run_dir / "weights" / f"{name}.pt"
        destination = RESULTS_DIR / f"{args.run_name}_{name}.pt"
        shutil.copy2(source, destination)
        checkpoints[name] = {"path": str(destination), "sha256": sha256(destination)}

    record = {
        "created_at": datetime.now().astimezone().isoformat(timespec="seconds"),
        "model": "YOLO11n",
        "dataset": DATASET_LABEL,
        "data_yaml": str(DATA_YAML),
        "run_dir": str(run_dir),
        "run_name": args.run_name,
        "arguments": vars(args),
        "seed": args.seed,
        "deterministic": True,
        "checkpoints": checkpoints,
    }
    record_path = RESULTS_DIR / f"{args.run_name}_training.json"
    record_path.write_text(json.dumps(record, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")
    print(f"Training complete: {run_dir}")
    print(f"Best checkpoint: {RESULTS_DIR / (args.run_name + '_best.pt')}")
    print(f"Training record: {record_path}")


if __name__ == "__main__":
    main()
