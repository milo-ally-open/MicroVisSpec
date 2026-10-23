# 场景档案

四个已存在的检测场景 + 新增流程。**没核实的一律写「待补」，不要猜。**

## 总表

| 场景 id | 目标物体 | 数据源 | 类别序 | 状态 |
|---|---|---|---|---|
| `model_classroom_behavior` | 课堂行为 | Roboflow `main-vdoys` / `student-dataset-demo` / v1 | `hand-raising`, `reading`, `writing` | 五阶段走通 |
| `model_manhole_cover` | 井盖 | Roboflow Universe `roboflow-universe-projects/manhole-cover-detection` / v1 / Public Domain | 单类，以 `data.yaml` names 为准 | G0–G6 PASS（2026-10-22） |
| `model_vehicle_detection` | 待补 | 待补 | 待补 | 待补 |
| `model_warehouse_detection` | 待补 | 待补 | 待补 | 待补 |

## classroom_behavior

- 模型根：`project/preparation/model_classroom_behavior/`
- 规格：`project/docs/Specification/classroom_behavior_detection/spec.md`
- 数据源：Roboflow workspace `main-vdoys`，project `student-dataset-demo`，version `1`
- 类别序：`hand-raising` → `reading` → `writing`（顺序固定，评估与 demo 的 `classes.txt` 必须同序）
- 训练：YOLO11n / 640 / 100 epoch / batch 8 / device 0 / workers 4 / seed 42 / deterministic / patience 30
- 板端：Atlas 200I DK A2（Ascend 310B4）
- demo 资源：VPSS 默认 group 1、Region 默认 handle 1（0 号可能被产品服务占用）

## manhole_cover

- 模型根：`project/preparation/model_manhole_cover/`（由 classroom 骨架移植，补了显式 `--run-name`）
- 数据源：Roboflow Universe `roboflow-universe-projects/manhole-cover-detection` v1，单类，Public Domain，真实源 `ff-vt4js`
- 数据清洗：原始 2297 张存在 535 处跨 split 重复（test/train 泄漏 149 张），SHA-256 去重后 1149 张（train 572 / valid 355 / test 222）
- 基座：YOLO11n / 640 / 100 epoch / seed 42
- 实测：test mAP50 = 0.8718、mAP50-95 = 0.6215；OM mAP50-95 = 0.6194（降幅 0.0005）；OM NPU 58.6 FPS；demo 97 帧 / 23.25 FPS
- 证据：`model_deployment/run_records/rnd_proof_20261022.md`，截图索引 `codearts_proof/images/README.md`

## 新增一个场景

1. 先填 `templates/scenario-profile.md`，把目标物体、类别序、数据源、板端、阈值写死；**类别序和数据源没定就别往下走**。
2. 复制 `project/preparation/model_manhole_cover/` 为 `model_<scenario>/`，改 `WORKFLOW.md` 里的场景名、移植说明、默认 run-name。
3. 在 `project/docs/Specification/<scenario>_detection/` 建规格目录（沿用 `project-spec-workflow` 的产物结构）。
4. 按 G0–G6 跑，每过一门在 `run_records/<run-id>.md` 记一行。
5. 全部 PASS 后，把这份档案补进本文件的总表与明细段。
6. 只有当流程有**新增的通用经验**时，才回头改 `SKILL.md`；场景特例写在这里，不要写进主流程。
