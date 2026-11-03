"""Create a non-destructive checkpoint interpolation candidate."""

from __future__ import annotations

import argparse
import copy
import hashlib
from pathlib import Path

import torch


def sha256(path: Path) -> str:
    digest = hashlib.sha256()
    with path.open("rb") as stream:
        for chunk in iter(lambda: stream.read(1024 * 1024), b""):
            digest.update(chunk)
    return digest.hexdigest()


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--base", type=Path, required=True, help="Base checkpoint.")
    parser.add_argument("--fine-tuned", type=Path, required=True, help="Fine-tuned checkpoint from the same initialization.")
    parser.add_argument("--alpha", type=float, default=0.5, help="Fine-tuned checkpoint interpolation weight, in [0, 1].")
    parser.add_argument("--output", type=Path, required=True, help="Output checkpoint path.")
    args = parser.parse_args()
    if not 0.0 <= args.alpha <= 1.0:
        parser.error("--alpha must be between 0 and 1")
    if args.output.exists():
        parser.error(f"Refusing to overwrite existing output: {args.output}")

    base = torch.load(args.base, map_location="cpu", weights_only=False)
    tuned = torch.load(args.fine_tuned, map_location="cpu", weights_only=False)
    if not isinstance(base.get("model"), torch.nn.Module) or not isinstance(tuned.get("model"), torch.nn.Module):
        raise TypeError("Both checkpoints must contain a serialized PyTorch model under 'model'.")

    base_state = base["model"].state_dict()
    tuned_state = tuned["model"].state_dict()
    if base_state.keys() != tuned_state.keys():
        raise ValueError("Checkpoint state dictionaries do not have identical keys.")

    soup = copy.deepcopy(tuned)
    blended_state = {}
    for key, tuned_tensor in tuned_state.items():
        base_tensor = base_state[key]
        if base_tensor.shape != tuned_tensor.shape or base_tensor.dtype != tuned_tensor.dtype:
            raise ValueError(f"Incompatible tensors at {key}: {base_tensor.shape}/{base_tensor.dtype} vs {tuned_tensor.shape}/{tuned_tensor.dtype}")
        if torch.is_floating_point(tuned_tensor):
            blended_state[key] = base_tensor * (1.0 - args.alpha) + tuned_tensor * args.alpha
        else:
            blended_state[key] = tuned_tensor
    soup["model"].load_state_dict(blended_state, strict=True)
    soup["ema"] = None
    soup["optimizer"] = None
    soup["scaler"] = None
    args.output.parent.mkdir(parents=True, exist_ok=True)
    torch.save(soup, args.output)
    print(f"Saved checkpoint: {args.output}")
    print(f"Base SHA-256: {sha256(args.base)}")
    print(f"Fine-tuned SHA-256: {sha256(args.fine_tuned)}")
    print(f"Alpha: {args.alpha}")
    print(f"Output SHA-256: {sha256(args.output)}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
