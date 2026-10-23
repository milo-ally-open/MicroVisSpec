# 场景档案：<目标物体>

> 复制本文件 → 填完 → 按需并入 `references/scenario-profiles.md` 的总表与明细段。
> 未确认的项写「待补」，不要留空、不要猜。

## 基本信息

| 项 | 值 |
|---|---|
| 场景 id | `model_<snake_case>` |
| 目标物体 | |
| 业务用途 | |
| 提出人 / 日期 | |
| 交付目标 | 分支 / MR / 发布标签 |

## 类别

| ID | 类别名 | 说明 |
|---|---|---|
| 0 | | |
| 1 | | |

类别序一经确定不得改动；`data.yaml`、`classes.txt`、评估报告、demo 四处必须同序。

## 数据源

| 项 | 值 |
|---|---|
| provider | Roboflow / Kaggle / 自建 |
| workspace / project / version | |
| license | |
| 原始数量 / 清洗后数量 | |
| 已知问题 | 重复、缺标、类别不平衡、跨 split 泄漏 |

## 训练

| 项 | 值 |
|---|---|
| 基座 | 默认 YOLO11n / 640 |
| epoch / batch / device / workers | 默认 100 / 8 / 0 / 4 |
| seed / deterministic / patience | 默认 42 / 是 / 30 |
| 环境 | conda，来自模型本地 `environment.yml` |

## 板端

| 项 | 值 |
|---|---|
| 型号 | Atlas 200I DK A2（Ascend 310B4） |
| CANN / runtime | |
| 资源约定 | VPSS group / Region handle 空闲 ID |

## 门禁阈值

| 项 | 值 |
|---|---|
| test mAP50-95 目标 | 项目定义 / 未指定 |
| ONNX→OM mAP50-95 降幅上限 | 缺省 0.02 |
| 逐类 AP50 降幅上限 | 缺省 0.05 |
| 板端 FPS 目标 | 项目定义 / 未指定 |
| 内存上限 | 项目定义 / 未指定 |

## 验收与交付

| 项 | 值 |
|---|---|
| demo 输入 | 真实流 / 合成烟测 |
| 交付物 | OM + classes.txt + AIPP 参数 + 报告 + 验收记录 |
| 交接文档位置 | `model_deployment/README.md` 或 `run_records/<run-id>.md` |
