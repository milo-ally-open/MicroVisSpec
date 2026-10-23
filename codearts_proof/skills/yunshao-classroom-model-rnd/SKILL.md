---
name: yunshao-classroom-model-rnd
description: "Run the Yunshao classroom-behavior model workflow from version-pinned data through YOLO11n training, Atlas 310B4 evaluation, and H.264 demo verification. Use when reproducing, improving, evaluating, or deploying this vertical model or distilling its process into a reusable skill."
---

# Yunshao Classroom Model R&D

Use this skill only for the classroom-behavior model in this repository. Treat the repository SOPs as the source of truth, preserve their stage order and directory layout, and report evidence for every gate.

## Read these project sources first

1. Read [classroom behavior specification](../../../project/docs/Specification/classroom_behavior_detection/spec.md) and [workflow index](../../../project/preparation/model_classroom_behavior/WORKFLOW.md).
2. Follow each stage README in this order:
   - [Training and dataset SOP](../../../project/preparation/model_classroom_behavior/model_train/README.md)
   - [ONNX export SOP](../../../project/preparation/model_classroom_behavior/model_export/README.md)
   - [Atlas conversion SOP](../../../project/preparation/model_classroom_behavior/model_convert/README.md)
   - [Accuracy, performance, and memory SOP](../../../project/preparation/model_classroom_behavior/model_val/README.md)
   - [Deployment demo SOP](../../../project/preparation/model_classroom_behavior/model_deployment/README.md)
3. Read `project/preparation/model_classroom_behavior/environment.yml` before changing dependencies. Do not replace a stage command with a guessed command when its README provides one.

## Keep the project boundaries

Use this model root and retain the stage-specific structure:

```text
project/preparation/model_classroom_behavior/
├── WORKFLOW.md
├── environment.yml
├── model_train/
│   ├── 1-download-dataset.py
│   ├── 2-analyse-dataset.py
│   ├── 3-train-model.py
│   ├── 4-validate-model.py
│   ├── datasets/classroom_behavior/
│   ├── dataset_analysis/classroom_behavior/
│   ├── runs/train/<run-name>/
│   └── train_result/<run-name>/
├── model_export/artifacts/
├── model_convert/artifacts/
├── model_val/reports/release/<run-id>/
└── model_deployment/
    ├── src/include/
    ├── samples/
    ├── build_<run-id>/
    └── acceptance_<run-id>/
```

Keep `WORKFLOW.md` as the stage index; do not add an aggregate README at the model root. Do not move files between `model_classroom_behavior`, `model_vehicle_detection`, or `model_warehouse_detection`. Keep product runtime code under `project/src/device_sides/atlas200idka2/`; the preparation demo must not replace it.

Before repository edits, confirm the actual Git root, remote, current branch, and user changes. Fetch the remote and fast-forward from the latest existing `main` or `dev`, then create a unique temporary branch. Never invent a branch or remote, overwrite user changes, stage datasets/checkpoints/ONNX/OM/videos/credentials, or delete a remote branch unless the user explicitly names it.

## Run the stages in order

### 1. Pin and verify the dataset

For the current classroom workflow, use Roboflow workspace `main-vdoys`, project `student-dataset-demo`, version `1`, with class order `hand-raising`, `reading`, `writing`. Use Kaggle only when the selected vertical SOP or user specifies it; do not silently swap providers, versions, class maps, or splits.

Inject `ROBOFLOW_API_KEY` through the process environment or an approved secret store. Never write API keys, passwords, private keys, or access tokens to source files, shell history, logs, reports, or this skill. Record the provider, dataset version, license/provenance, `data.yaml` hash, and image/label manifest hash without recording credentials.

Run the exact download and analysis commands from `model_train/README.md`. Before training, verify that train/valid/test images and labels exist, class IDs match `data.yaml`, labels parse, split counts are nonzero, and there are no cross-split duplicates or missing labels. Stop and repair/re-download the dataset if any check fails.

### 2. Train and validate

Create or update the `yunshao` Conda environment from the model-local `environment.yml`. From the model root, train the fixed YOLO11n/640 model with the SOP parameters: 100 epochs, batch 8, device 0, workers 4, seed 42, deterministic mode, and patience 30. Assign a unique `--run-name` to every attempt; do not overwrite a previous run.

Validate the selected best checkpoint using the model training SOP and record the complete command, code revision, dataset hashes, environment/GPU versions, training log, best/last checkpoint hashes, and per-class metrics. Use the validation split to choose training changes. Keep the test split as a holdout; do not tune hyperparameters or thresholds from individual test examples.

If a quality or downstream gate fails, diagnose whether the failure is data, training, export, ATC, runtime, or threshold mismatch before deciding to retrain. For a training retry, use a new run name and separate export, report, OM, and demo paths. Preserve failed-run evidence. Do not select a model based on repeatedly inspecting test results.

### 3. Export the fixed ONNX

Export only a validated checkpoint using the model-local exporter and the SOP settings: batch 1, 640×640, static shape, opset 17, CPU export. For a retry, point `--weights-root` at that run’s `train_result/<run-name>` directory. Verify `export_manifest.json`, checkpoint SHA-256, ONNX SHA-256, input shape, and class order before continuing.

### 4. Convert on the Atlas board

Use the Atlas 200I DK A2 / Ascend 310B4 with the CANN version stated by the SOP. Use SSH key authentication and a user-supplied host alias or `ATLAS_HOST`; never embed board passwords or private IPs in source or skill content. Stage each experiment under a unique board run directory and preserve existing models and runtime files.

Load `/usr/local/Ascend/ascend-toolkit/set_env.sh`, verify the staged ONNX hash against `export_manifest.json`, and run `model_convert/convert_atc.sh` from the staged model root. If the board-side shell script has CRLF line endings, normalize only that isolated copy to LF before execution and record its final hash. Confirm the expected nonempty OM and recompute the ONNX, AIPP, and OM hashes from `SHA256SUMS` before copying the OM to the run-specific deployment path.

### 5. Evaluate accuracy, performance, and memory

Use the same Roboflow snapshot, `data.yaml`, manifest, test split, class order, 640 input, confidence threshold, and IoU threshold for ONNX and OM. Run both complete test reports with `--metric-mode native --conf 0.25` and without `--limit`: the evaluator retains candidates down to 0.001 for AP and reports operating-point precision/recall at 0.25. Do not use `auto` across machines because installed packages can select different metric implementations. Keep smoke results separate from formal test reports.

Run the SOP `precision_eval.py` comparison with `--max-map-drop 0.02` and `--max-class-ap50-drop 0.05`. These are ONNX-to-OM parity limits, not absolute model-quality targets. The comparator gates mAP50-95 and per-class AP50 and reports operating-point precision/recall deltas; it does not define an absolute accuracy threshold. Confirm both reports record the same `metric_mode`, confidence, and IoU before interpreting precision/recall deltas.

Run full-test OM performance evaluation and the SOP memory evaluation. Record pipeline FPS and latency percentiles, model size, process peak RSS, input/output tensor sizes, plus `npu-smi` snapshots before and after. RSS is host process memory; `npu-smi` is device-wide memory and may not attribute usage to this process. Record board temperature, health, CANN/runtime version, and concurrent workload.

Apply any absolute mAP/FPS/memory acceptance limits already defined by the project or supplied by the user. If none exist, report measured values and mark absolute model-quality acceptance as unspecified; do not invent a pass threshold. If ONNX/OM parity fails, first inspect hashes, preprocessing, AIPP, class order, and runtime before retraining. If a defined model-quality gate fails, return to training with a unique run and keep the test set sealed for model selection.

### 6. Build and run the H.264 demo only after evaluation passes

Build from the staged `model_deployment` sources with CMake in a fresh `build_<run-id>` directory. Pass the run-specific OM and `classes.txt`; verify the labels remain ordered as `hand-raising`, `reading`, `writing` and the model output is compatible with the demo.

Do not stop or reconfigure the running classroom product service to make the demo work. VPSS group 0 and Region handle 0 may already be owned. Use the demo’s `--vpss-group` and `--region-handle` options to select free resources; the current demo defaults to group 1 and handle 1. Treat resource-exists errors as a collision and choose another free ID.

Use an authorized H.264 classroom sample or RTSP source when available. If none is provided, generate a clearly named synthetic H.264 smoke clip from held-out test images in the run’s `acceptance_<run-id>` directory. Run the demo, record exit status, processed/encoded frames, effective FPS, inference timing, peak RSS, and output SHA-256. Use `ffprobe` to confirm the output is nonempty, decodable H.264 and the frame count matches the demo log. Label a synthetic clip as software-chain smoke only; never claim real classroom-stream or product acceptance from it.

## Final evidence and packaging

Keep a compact evidence ledger containing: Git revision/branch; stage commands and exit codes; dataset provider/version/license and hashes; class order; training parameters and checkpoint hash; ONNX manifest/hash; ATC/CANN/SOC and OM hash; complete ONNX/OM reports and parity verdict; performance/memory/NPU snapshots; demo input provenance, output hash, codec, and processed frame count. Mark missing evidence `待补`.

Only after all project-defined gates pass, create or update the requested reusable skill under `codearts_proof/skills/`. Validate the skill and its UI metadata. Before pushing, inspect `git status`, `git diff --check`, and the exact staged file list. Push only the requested temporary branch; exclude downloaded data, weights, ONNX/OM files, generated videos, credentials, and unrelated user changes.
