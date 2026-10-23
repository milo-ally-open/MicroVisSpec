---
name: yunshao-vertical-model-rnd
description: "Generalized Yunshao vertical detection-model R&D engine. Turn any requirement into a runnable detection scenario: pin the dataset, train the fixed YOLO baseline, export ONNX, convert with ATC for Atlas 310B4, gate ONNX/OM parity plus performance and memory, and ship an H.264 demo backed by a replayable evidence ledger. Use for any object category, whenever a new scenario is requested or an existing one is reproduced, retrained, or handed to the product runtime."
---

# 云哨垂直模型研发（通用引擎）

输入是「要检测什么」，输出是「一个能在 Atlas 200I DK A2（Ascend 310B4）上跑起来的垂直检测模型，加一套别人照着就能重跑的证据」。

本 skill 只管**流程与门禁**，不管业务语义。场景相关的数据源、类别、阈值从 `references/scenario-profiles.md` 或当次需求里取；仓库里该场景的 `WORKFLOW.md` 和各阶段 `README.md` 是最终执行依据，与本 skill 冲突时以仓库文档为准。

## 适用与不适用

**适用**

- 新增检测场景：新物体、新数据源、新类别
- 复现或改进已有场景：换数据版本、重训、调阈值、换板端
- 把结果交接给产品 runtime

**不适用**

- 分类 / 分割 / 关键点等非检测任务
- 拿不到开发板时——可跑到 G3，G4 起逐项标「待补」，不要用模拟结果顶替
- 只要一个演示、不需要证据链——直接照 `model_deployment/README.md` 走

## 开工前先定这 8 件事

| # | 项 | 缺了会怎样 |
|---|---|---|
| 1 | 场景 id：`model_<snake_case>` | 目录、run-id、报告命名全线混乱 |
| 2 | 目标物体 + 类别序 | 类别 ID 错位，精度全部作废 |
| 3 | 数据源：provider / workspace / project / version / license | 无法复现，不能对外声称版本 |
| 4 | 基座与超参：默认 YOLO11n / 640 / 100ep / batch 8 / seed 42 / deterministic | 结果与历史 run 不可比 |
| 5 | 板端型号 + CANN 版本 | ATC 参数只能靠猜 |
| 6 | 门禁阈值：mAP / parity / FPS / 内存 | 只能报实测值并标「未指定」 |
| 7 | 交付目标：分支、MR、发布标签 | 推错分支 |
| 8 | 时间盒与算力：本机 GPU、板端占用 | 训练和 ATC 排队失控 |

第 2、3 项缺任意一项就别开工，先向用户问清。

## 场景骨架

固定五阶段，根目录 `project/preparation/model_<scenario>/`：

```text
model_<scenario>/
├── WORKFLOW.md            # 只做阶段索引，根部不放总 README
├── environment.yml
├── .gitignore
├── model_train/           # 下载 / 分析 / 训练 / 验证
├── model_export/          # 固定输入 ONNX
├── model_convert/         # Atlas ATC
├── model_val/             # 精度 / 性能 / 内存
└── model_deployment/      # H.264 demo
```

新场景直接照 `model_manhole_cover` 复制骨架——它是从 `model_classroom_behavior` 移植、并补了显式 `--run-name` 的版本，移植适配说明写在它的 `WORKFLOW.md` 里。

规格文档放 `project/docs/Specification/<scenario>_detection/`；产品 runtime 在 `project/src/device_sides/atlas200idka2/`，preparation 的 demo 不许顶替产品代码。

## 七道门禁

| 门 | 阶段 | 关键产出 | 通过条件 | 不通过怎么办 |
|---|---|---|---|---|
| G0 | 场景立项 | 五阶段骨架、`WORKFLOW.md`、数据源与类别选定 | 目录齐套、既有场景零改动、类别序写入文档 | 回到需求，先定类别与数据源 |
| G1 | 数据集校验 | 下载与分析报告、`data.yaml` 哈希、去重记录 | 三划分非空、零跨 split 重复、零缺标、类别 ID 与 `data.yaml` 一致 | 修复或重下，绝不在脏数据上开训 |
| G2 | 训练 + 验证 | best/last 权重、逐类指标、训练日志 | valid 出完整指标；test 全程留出 | 换新 `--run-name` 重训，失败证据保留 |
| G3 | ONNX 导出 | `export_manifest.json`、ONNX 哈希 | 输入 1×3×640×640 静态、opset 17、类别序与哈希全部对齐 | 修导出脚本，不要为此改模型结构 |
| G4 | 板端 ATC | OM、`SHA256SUMS` | OM 非空、三件哈希复验一致 | 查 ATC 日志、CANN 版本、脚本 CRLF |
| G5 | 双端评估 | ONNX/OM 完整 test 报告、parity 对比 | 降幅在项目阈值内；缺省 mAP50-95 ≤ 0.02、类 AP50 ≤ 0.05 | 先查预处理、AIPP、类别序、runtime，再谈重训 |
| G6 | H.264 demo | 验收视频、ffprobe 结果、运行统计 | 退出码 0、帧数与日志一致、输出可解码 | 查 VPSS / Region 资源冲突与 EOF 处理 |

阈值只认项目文档或用户给的值。没有就给实测值 + 标注「未指定」，**不许自造 pass 线**。逐项勾选清单见 `references/gate-checklist.md`。

## 阶段要点

**G0 立项**：建目录、写 `WORKFLOW.md` 索引、选数据源。改既有场景前先确认 git root、remote、当前分支、用户未提交的改动。

**G1 数据**：跑 SOP 里的下载与分析命令。校验图像/标签成对、类别 ID 对齐、划分非空、无跨 split 重复、无缺标。数据源换成 Kaggle 或其他 provider 时，必须显式说明并记录，不许静默替换。

**G2 训练**：从模型本地 `environment.yml` 建 conda 环境；每次尝试用唯一 `--run-name`，不覆盖旧 run；选模型只看 valid，test 只在最后出报告。

**G3 导出**：只导已验证的 checkpoint；固定 batch 1、640×640、静态、opset 17；核对 manifest、权重哈希、ONNX 哈希、输入形状、类别序。

**G4 转换**：SSH key 登录，独立板端 run 目录，保留既有模型；`source set_env.sh` 后跑 `convert_atc.sh`；CRLF 只就地转 LF 并记录最终哈希；产物哈希从 `SHA256SUMS` 复算。

**G5 评估**：ONNX 与 OM 用同一快照、同一 `data.yaml`、同一 test 划分、同一 640 输入、同一 conf/IoU；`--metric-mode native --conf 0.25`，不加 `--limit`；两侧报告的 `metric_mode`、conf、IoU 必须一致，才允许比较 delta。性能记 FPS 与延迟分位，内存记进程 RSS 加 `npu-smi` 快照。

**G6 演示**：评估通过后再编译，`build_<run-id>` 用新目录；不要为了 demo 去停或改产品服务；VPSS group 0 / Region 0 可能已被占用，用 `--vpss-group` / `--region-handle` 选空闲 ID；没有真实素材时用留出集生成合成 H.264 烟测，并且只能称「软件链路烟测」。

## 模型团队约定

**命名**

- run-id：`<scenario>_<yyyymmdd>`，同日多轮加 `_b`、`_c`
- run-name：`<baseline>_<scenario>`，每次重试换新名
- 产物：`model_train/train_result/<run-name>/`、`model_val/reports/release/<run-id>/`、`model_deployment/build_<run-id>/`、`acceptance_<run-id>/`
- 证据：`model_deployment/run_records/<run-id>.md`，按 `references/evidence-ledger.md` 填

**禁止提交**：`datasets/`、`runs/`、`*.pt`、`*.onnx`、`*.om`、生成的视频、密钥与令牌、`.env`。

**Git 纪律**：推之前看 `git status`、`git diff --check`、暂存清单三样；只推用户点名的分支；不改别人的历史，除非用户明确要求覆盖。

## 交接给产品 runtime

交付物是：OM + `classes.txt` + AIPP 预处理参数 + 精度/性能/内存报告 + demo 验收记录。交接说明只写一份，放 `model_deployment/README.md` 或 `run_records/<run-id>.md`，产品侧只读这份。板端路径、CANN 与 runtime 版本写死具体版本号，不写「最新」。

## 反模式

- 用 test 集调参、反复看 test 结果挑模型
- 失败后原地覆盖 run-name，把失败证据冲掉
- 把合成烟测说成真实场景验收
- 编造项目未定义的 pass 阈值
- 把权重、ONNX、OM、视频塞进 git
- 改产品 runtime 去迁就 demo
- 用一个场景的特例（某数据源、某类别数）冒充通用规则

## 参考

- `references/scenario-profiles.md` —— 已有场景档案与新增场景流程
- `references/gate-checklist.md` —— G0–G6 逐项勾选清单
- `references/evidence-ledger.md` —— 证据台账模板
- `templates/scenario-profile.md` —— 新场景档案填写模板
