"""Train the YOLO11n vehicle detection model.

Run this script from any directory. Dataset YAML files and run outputs default
to paths next to this script; training settings can be overridden from CLI.
"""

import argparse
from pathlib import Path
from ultralytics import YOLO


SCRIPT_DIR = Path(__file__).resolve().parent
DATASETS = {
    "vehicle_detection": SCRIPT_DIR / "datasets" / "vehicle_detection" / "data.yaml",
}


def parse_int_list(value):
    """Parse comma-separated integer IDs, such as ``0,2,3``."""
    if value is None or not value.strip():
        return None
    try:
        return [int(item.strip()) for item in value.split(",")]
    except ValueError as exc:
        raise argparse.ArgumentTypeError("expected comma-separated integers, e.g. 0,2") from exc


def parse_args():
    parser = argparse.ArgumentParser(
        description="Train the vehicle detection model."
    )

    # Model and data selection
    parser.add_argument("--model", default="yolo11n.pt", help="Starting weights or model YAML")
    parser.add_argument(
        "--datasets",
        nargs="+",
        choices=tuple(DATASETS),
        default=list(DATASETS),
        help="Dataset to train.",
    )
    parser.add_argument(
        "--data-root",
        type=Path,
        default=SCRIPT_DIR / "datasets",
        help="Directory containing <dataset-name>/data.yaml",
    )
    parser.add_argument(
        "--epochs", type=int, required=True, help="Training epochs (required)"
    )
    parser.add_argument("--imgsz", type=int, default=640, help="Training image size")
    parser.add_argument("--batch", type=int, default=16, help="Batch size; -1 enables auto batch")
    parser.add_argument("--device", default="0", help="CUDA device, e.g. 0, 0,1, or cpu")
    parser.add_argument("--workers", type=int, default=8, help="Dataloader worker processes")
    parser.add_argument("--classes", type=parse_int_list, help="Optional class IDs, e.g. 0,1,3")
    parser.add_argument("--single-cls", action=argparse.BooleanOptionalAction, default=False)
    parser.add_argument("--fraction", type=float, default=1.0, help="Fraction of training data to use")

    # Run management and validation
    parser.add_argument(
        "--project", type=Path, default=SCRIPT_DIR / "runs" / "train", help="Run output directory"
    )
    parser.add_argument("--name", default="yolo11n", help="Run name prefix")
    parser.add_argument("--exist-ok", action=argparse.BooleanOptionalAction, default=False)
    parser.add_argument("--save", action=argparse.BooleanOptionalAction, default=True)
    parser.add_argument("--save-period", type=int, default=-1, help="Checkpoint interval in epochs; -1 disables")
    parser.add_argument("--val", action=argparse.BooleanOptionalAction, default=True)
    parser.add_argument("--split", choices=("val", "test"), default="val", help="Split used for validation")
    parser.add_argument("--plots", action=argparse.BooleanOptionalAction, default=True)
    parser.add_argument("--patience", type=int, default=30, help="Early stopping patience; 0 disables")
    parser.add_argument("--seed", type=int, default=42)
    parser.add_argument("--deterministic", action=argparse.BooleanOptionalAction, default=True)
    parser.add_argument("--resume", action=argparse.BooleanOptionalAction, default=False)
    parser.add_argument("--profile", action=argparse.BooleanOptionalAction, default=False)
    parser.add_argument("--amp", action=argparse.BooleanOptionalAction, default=True)
    parser.add_argument("--compile", action=argparse.BooleanOptionalAction, default=False)

    # Optimizer and learning-rate schedule
    parser.add_argument(
        "--optimizer",
        choices=("auto", "SGD", "Adam", "AdamW", "NAdam", "RAdam", "RMSProp"),
        default="auto",
    )
    parser.add_argument("--lr0", type=float, default=0.01, help="Initial learning rate")
    parser.add_argument("--lrf", type=float, default=0.01, help="Final learning-rate fraction")
    parser.add_argument("--momentum", type=float, default=0.937)
    parser.add_argument("--weight-decay", type=float, default=0.0005)
    parser.add_argument("--warmup-epochs", type=float, default=3.0)
    parser.add_argument("--warmup-momentum", type=float, default=0.8)
    parser.add_argument("--warmup-bias-lr", type=float, default=0.1)
    parser.add_argument("--cos-lr", action=argparse.BooleanOptionalAction, default=False)
    parser.add_argument("--nbs", type=int, default=64, help="Nominal batch size for loss scaling")
    parser.add_argument("--pretrained", action=argparse.BooleanOptionalAction, default=True)
    parser.add_argument(
        "--freeze",
        type=int,
        nargs="*",
        help="Optional layer indices to freeze, e.g. --freeze 0 1 2; omit to train all layers",
    )
    parser.add_argument("--multi-scale", type=float, default=0.0, help="Multi-scale training fraction; 0 disables")
    parser.add_argument("--rect", action=argparse.BooleanOptionalAction, default=False)
    # Detection loss and target assignment
    parser.add_argument("--box", type=float, default=7.5, help="Bounding-box loss gain")
    parser.add_argument("--cls", type=float, default=0.5, help="Classification loss gain")
    parser.add_argument(
        "--cls-pw",
        type=float,
        default=0.0,
        help="Class-imbalance weighting power; 0 disables, 1 uses full inverse-frequency weighting",
    )
    parser.add_argument("--dfl", type=float, default=1.5, help="Distribution focal loss gain")

    # Training-time image preprocessing and augmentation
    parser.add_argument("--hsv-h", type=float, default=0.015)
    parser.add_argument("--hsv-s", type=float, default=0.7)
    parser.add_argument("--hsv-v", type=float, default=0.4)
    parser.add_argument("--degrees", type=float, default=0.0)
    parser.add_argument("--translate", type=float, default=0.1)
    parser.add_argument("--scale", type=float, default=0.5)
    parser.add_argument("--shear", type=float, default=0.0)
    parser.add_argument("--perspective", type=float, default=0.0)
    parser.add_argument("--flipud", type=float, default=0.0)
    parser.add_argument("--fliplr", type=float, default=0.5)
    parser.add_argument("--bgr", type=float, default=0.0, help="Probability of BGR channel reversal")
    parser.add_argument("--mosaic", type=float, default=1.0)
    parser.add_argument("--mixup", type=float, default=0.0)
    parser.add_argument("--cutmix", type=float, default=0.0)
    parser.add_argument("--close-mosaic", type=int, default=10, help="Disable mosaic for final N epochs")
    # Validation and loader performance
    parser.add_argument("--cache", choices=("none", "ram", "disk"), default="none")
    parser.add_argument("--time", type=float, help="Optional training time limit in hours")
    parser.add_argument("--max-det", type=int, default=300)
    parser.add_argument("--conf", type=float, help="Optional validation confidence threshold")
    parser.add_argument("--iou", type=float, default=0.7, help="Validation NMS IoU threshold")
    parser.add_argument("--save-json", action=argparse.BooleanOptionalAction, default=False)

    return parser.parse_args()


def build_train_kwargs(args, data_yaml, run_name):
    freeze = args.freeze
    if freeze is not None and len(freeze) == 1:
        freeze = freeze[0]

    return {
        "data": str(data_yaml),
        "epochs": args.epochs,
        "time": args.time,
        "imgsz": args.imgsz,
        "batch": args.batch,
        "device": args.device,
        "workers": args.workers,
        "project": str(args.project),
        "name": run_name,
        "exist_ok": args.exist_ok,
        "pretrained": args.pretrained,
        "optimizer": args.optimizer,
        "verbose": True,
        "seed": args.seed,
        "deterministic": args.deterministic,
        "single_cls": args.single_cls,
        "rect": args.rect,
        "cos_lr": args.cos_lr,
        "close_mosaic": args.close_mosaic,
        "resume": args.resume,
        "amp": args.amp,
        "fraction": args.fraction,
        "profile": args.profile,
        "freeze": freeze,
        "multi_scale": args.multi_scale if args.multi_scale > 0 else False,
        "val": args.val,
        "split": args.split,
        "plots": args.plots,
        "compile": args.compile,
        "save": args.save,
        "save_period": args.save_period,
        "patience": args.patience,
        "classes": args.classes,
        "cache": False if args.cache == "none" else args.cache,
        "conf": args.conf,
        "iou": args.iou,
        "save_json": args.save_json,
        "lr0": args.lr0,
        "lrf": args.lrf,
        "momentum": args.momentum,
        "weight_decay": args.weight_decay,
        "warmup_epochs": args.warmup_epochs,
        "warmup_momentum": args.warmup_momentum,
        "warmup_bias_lr": args.warmup_bias_lr,
        "box": args.box,
        "cls": args.cls,
        "cls_pw": args.cls_pw,
        "dfl": args.dfl,
        "nbs": args.nbs,
        "hsv_h": args.hsv_h,
        "hsv_s": args.hsv_s,
        "hsv_v": args.hsv_v,
        "degrees": args.degrees,
        "translate": args.translate,
        "scale": args.scale,
        "shear": args.shear,
        "perspective": args.perspective,
        "flipud": args.flipud,
        "fliplr": args.fliplr,
        "bgr": args.bgr,
        "mosaic": args.mosaic,
        "mixup": args.mixup,
        "cutmix": args.cutmix,
        "max_det": args.max_det,
    }


def main():
    args = parse_args()
    args.project = args.project.expanduser().resolve()
    args.data_root = args.data_root.expanduser().resolve()

    for dataset_name in args.datasets:
        data_yaml = args.data_root / dataset_name / "data.yaml"
        if not data_yaml.is_file():
            raise FileNotFoundError(f"Dataset YAML not found: {data_yaml}")

        run_name = f"{args.name}_{dataset_name}"
        last_checkpoint = args.project / run_name / "weights" / "last.pt"
        if args.resume:
            if not last_checkpoint.is_file():
                raise FileNotFoundError(
                    f"Cannot resume {dataset_name}: checkpoint not found: {last_checkpoint}"
                )
            model = YOLO(str(last_checkpoint))
        else:
            model = YOLO(args.model)

        print(f"\n=== Training {dataset_name} with {args.model} ===")
        print(f"Dataset: {data_yaml}")
        model.train(**build_train_kwargs(args, data_yaml, run_name))


if __name__ == "__main__":
    main()
