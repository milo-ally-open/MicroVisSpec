# 云韶课堂行为检测模型研发复刻与证据采集——实施任务清单（tasks.md）

> 上游输入：[spec.md](./spec.md)（8 个功能模块 / 42 条 EARS 业务规则 / 20 个异常场景 / 6 类 DFX 约束）、[design.md](./design.md)（I0–I7 接口方案、G1–G6 阶段门禁、D1–D15 决策点、14 个截图采集点明细）、`C:\Users\miligting\Desktop\SKILL.md`（yunshao-classroom-model-rnd Skill）、仓库 SOP（WORKFLOW.md 及五阶段 README）。
> 状态：待执行。
> 变更记录：2026-10-22 依据用户指示，最终成果发布目标由"main+dev 双分支"调整为"仅远程 dev 分支"（路径 `codearts_proof/images/`，不再提交 main 分支）；任务 1.2、8.4、8.5、9.3 及附录 A 相应修订，spec.md / design.md 已同步修订。
> 安全声明：本清单不含任何凭据。`ROBOFLOW_API_KEY` 与 `SSHPASS` 仅经进程级临时环境注入，不落盘、不入命令历史、不进日志与截图。

---

## 全局执行约定（非任务）

**变量与路径约定**

| 变量 | 取值 | 说明 |
|---|---|---|
| `$ATLAS` | `10.160.13.173` | 部署环境信息白名单值，替换 README 示例 `192.168.0.2`（D1） |
| `$RUN` | `rnd_proof_<yyyymmdd>`（如 `rnd_proof_20261022`） | 本轮唯一 run-id，启动时确定 |
| 训练 run-name | `yolo11n_classroom_behavior_$RUN` | 与存量 run（`yolo11n_classroom_behavior`、`…_preenv_py314_20261021`）不冲突（D4） |
| 临时分支 | `proof/classroom_rnd_proof_$RUN` | 唯一临时工作分支，基于最新 `origin/dev` 创建（D10；2026-10-22 修订：仅提交 dev 分支） |
| 板端 run 根 | `/root/model_library/model_classroom_behavior/runs/$RUN/` | 隔离层，内部保持 README 相对结构（D5） |
| 仓库根（Windows） | `C:\Users\miligting\Desktop\Yunshao` | git 操作、截图采集、文件管理宿主 |
| 仓库根（WSL） | 以 `wslpath 'C:\Users\miligting\Desktop\Yunshao'` 输出为准（通常 `/mnt/c/Users/miligting/Desktop/Yunshao`） | 阶段一~三、阶段五 ONNX 侧命令宿主（D3） |

**凭据安全约定**：`export ROBOFLOW_API_KEY=…` 与 `export SSHPASS=…` 仅在当前 shell 进程内注入（命令前缀空格规避 bash history）；板端访问统一 `sshpass -e ssh/scp root@$ATLAS …`，凭据不得出现在命令明文、日志、报告、截图及任何将提交文件中；IP `10.160.13.173` 与用户名 `root` 为已批准部署环境信息可保留。

**可变参数白名单**：全流程仅三类可变值——主机地址（D1）、run-id（D4/D5）、输出路径隔离段（D6）；固定参数（超参、阈值、metric-mode、opset 等）逐字照抄 README，任何偏差须在台账记录理由。

**run-id 隔离与失败重试总则**：任一阶段失败时保留失败证据（截图/日志/产物目录）不删除；重试启用新 run-id 或新 run-name（`…_retry_01`），产物路径全部独立，不覆盖既有运行；失败不污染后续任务路径。

**截图通用要求**：PNG 格式；命名 `<两位阶段号>_<阶段标识>_<两位序号>.png`；单张 ≤10 MB（超限压缩）；阶段命令完成后 ≤5 分钟内采集（`02_train_01` 为启动时刻例外）；画面可辨识阶段与关键结果；无凭据明文；失败现场同样计入证据。

---

## 1. 研发准备与基线建立（I0）

### 1.1 核对 SOP 文档齐套与 Skill 上下文
- [ ] 逐一确认以下文件存在并完成读取：`C:\Users\miligting\Desktop\SKILL.md`、`project/preparation/model_classroom_behavior/WORKFLOW.md`、五阶段 README（model_train / model_export / model_convert / model_val / model_deployment）、`project/preparation/model_classroom_behavior/environment.yml`、`project/docs/Specification/classroom_behavior_detection/spec.md`
- [ ] 核对 WORKFLOW.md 阶段顺序与 Skill 七步流程一致，无相互矛盾
- **执行目录**：`C:\Users\miligting\Desktop\Yunshao`
- **预期产物**：8 个 SOP 文件齐套核对结论（记入台账草稿）
- **对应门禁**：PREPARE 入口条件（Skill 已加载、SOP 已读）
- **失败处理**：任一文件缺失或内容矛盾 → 停止复刻，输出缺失清单并标注"待补"，不执行任何阶段命令
- **规则追溯**：spec 5.1 R1/R2、异常 5.1.3-1/2

### 1.2 Git 基线核对与唯一临时分支创建
- [ ] 在仓库根执行基线四查（远程/分支/用户改动/工作区状态）：
```bash
git remote -v && git branch --show-current && git status --short
git fetch origin
```
- [ ] 核对 `origin = https://atomgit.com/milo-ally-stu/Yunshao.git`，确认远程 `dev` 分支存在（本轮唯一发布目标分支，不提交 main）
- [ ] 确认用户未提交改动（`D codearts_proof/skills/yunshao-classroom-model-rnd/SKILL.md`、未跟踪 `.arts/`、`model_train/yolo11n.pt`、`yolo26n.pt`）保持原样，不暂存、不混入
- [ ] 从最新 dev 更新基线并创建唯一临时分支（2026-10-22 修订：基线由 main 改为 dev）：
```bash
git checkout dev && git merge --ff-only origin/dev
git checkout -b proof/classroom_rnd_proof_$RUN
```
- **执行目录**：`C:\Users\miligting\Desktop\Yunshao`（git-bash）
- **预期产物**：唯一临时分支 `proof/classroom_rnd_proof_$RUN`，基线为最新 `origin/dev`
- **对应门禁**：PREPARE → S1_DATASET 转移条件
- **失败处理**：`merge --ff-only` 或 `checkout` 受阻（本地 dev 分叉/工作区改动冲突）→ 不强推、不重置，改用 `git checkout -b proof/classroom_rnd_proof_$RUN origin/dev` 直接自远程引用创建分支，并在台账记录；本地无 dev 分支 → 先 `git checkout -b dev origin/dev` 自远程创建后再建临时分支；fetch 失败 → 检查网络后重试
- **规则追溯**：spec 5.1 R4/R5、4.5 R2、异常 5.1.3-3

### 1.3 WSL yunshao Conda 环境准备与一致性确认
- [ ] 在 WSL 内按 environment.yml 创建/更新环境并激活（SOP 原文，model_train/README L13–L17）：
```bash
cd <WSL仓库根>/project/preparation/model_classroom_behavior
conda env update -n yunshao -f environment.yml
conda activate yunshao
```
- [ ] 确认环境与 environment.yml 关键项一致（Python 3.12、torch 2.13.0+cu130、ultralytics 8.4.118、roboflow 1.1.53、onnx 1.17.0、onnxruntime 1.20.1），并以 `python -c "import torch;print(torch.__version__,torch.cuda.is_available())"` 确认 CUDA 可用性（仅环境检查，不替代阶段命令）
- **执行目录**：WSL `…/project/preparation/model_classroom_behavior`
- **预期产物**：与 environment.yml 一致的 `yunshao` 环境（记入台账：环境/GPU 版本）
- **对应门禁**：训练前置条件（spec 5.3 R1）
- **失败处理**：`conda env update` 失败 → 检查网络与 conda 后重试；CUDA 不可用 → 记录设备差异，若训练改用 `--device cpu` 须显式记录为不同实验
- **规则追溯**：spec 5.1 R3、5.3 R1

### 1.4 确定运行参数、建立证据目录与截图机制
- [ ] 确定 `$RUN`（`rnd_proof_<yyyymmdd>`）与训练 run-name，核对与 `runs/train/`、`train_result/` 下存量运行名不冲突
- [ ] 创建证据目录 `codearts_proof/images/`（不存在时新建）
- [ ] 准备截图采集机制（D7）：PowerShell `CopyFromScreen` 截取前台终端窗口 → PNG 直写 `codearts_proof/images/`；试截 1 张至临时路径验证可用后删除（不入证据目录）；人工截图工具兜底
- [ ] 将三类可变参数（IP 替换、run-id、输出路径隔离段）的取值与理由登记入台账草稿
- **执行目录**：`C:\Users\miligting\Desktop\Yunshao`
- **预期产物**：`codearts_proof/images/` 目录、可用截图脚本、RunContext 参数记录
- **对应门禁**：S1_DATASET 启动条件
- **失败处理**：截图脚本不可用 → 启用人工截图兜底并记录；run-name 冲突 → 追加日期/序号后缀重新生成
- **规则追溯**：spec 5.8 R1、4.4 R1（命名机制就绪）

---

## 2. 数据集固定与校验复刻（阶段一，I1 → 门禁 G1）

### 2.1 注入凭据并下载固定版本数据集
- [ ] 在 WSL 模型目录以进程级环境变量注入凭据（前缀空格规避命令历史），执行 SOP 原文下载命令（model_train/README L30）：
```bash
export ROBOFLOW_API_KEY=<运维安全注入>
python model_train/1-download-dataset.py
```
- [ ] 核对产物来源固定：Roboflow workspace `main-vdoys` / project `student-dataset-demo` / version `1`，类别顺序 hand-raising、reading、writing
- [ ] 计算 `data.yaml` 与图像/标签清单 SHA-256 登记台账；检查下载产物、日志、终端输出无凭据明文
- **执行目录**：WSL `…/project/preparation/model_classroom_behavior`
- **预期产物**：`model_train/datasets/classroom_behavior/`（data.yaml、图像、YOLO labels）+ 来源与哈希记录
- **对应门禁**：G1 前置输入
- **失败处理**：下载失败（凭据缺失/网络错误/Roboflow 返回失败）→ 停止并报告原因，保留失败现场，本阶段标"待补/失败"，不生成"通过"截图
- **规则追溯**：spec 5.2 R1/R2、4.3 R5、异常 5.2.3-1

### 2.2 生成数据质量分析摘要
- [ ] 执行 SOP 原文分析命令（model_train/README L36）：
```bash
python model_train/2-analyse-dataset.py
```
- [ ] 确认 `model_train/dataset_analysis/classroom_behavior/dataset_summary.json` 与图表生成
- **执行目录**：WSL `…/project/preparation/model_classroom_behavior`
- **预期产物**：`dataset_summary.json` 及分析图表
- **对应门禁**：G1 判定输入
- **失败处理**：脚本异常退出 → 保留错误输出，转入 2.3 按 G1 失败处置
- **规则追溯**：spec 5.2 R3（前置）

### 2.3 执行门禁 G1 训练前完整性判定
- [ ] 读取 `dataset_summary.json` 逐项判定：train/valid/test 三 split 数量非零；类别 ID 与 `data.yaml` names（hand-raising/reading/writing）一致；标签可解析；无跨 split 重复或缺失标签
- [ ] 特别核对 test 标签齐全（已知某些工作区快照曾缺 test 标签），并确认各 split 图像与标签目录完整、无非法或空目录
- [ ] 将 G1 结论与失败项（若有）登记台账
- **执行目录**：WSL（读取开发机产物）
- **预期产物**：G1 通过/失败结论
- **对应门禁**：**G1**（不通过 → 禁止训练）
- **失败处理**：任一项失败 → 停止训练流程，重新下载数据集后重判；重试仍失败 → 中止流程并在台账标注，不出现"可继续训练"结论
- **规则追溯**：spec 5.2 R3/R4、异常 5.2.3-2

### 2.4 采集并归档阶段一截图（2 张）
- [ ] 采集 `01_dataset_01.png`：下载命令完成输出（可辨识 provider/version），触发时机为 2.1 命令完成后 ≤5 分钟
- [ ] 采集 `01_dataset_02.png`：分析命令输出 + `dataset_summary.json` 关键字段（含 `data.yaml` 哈希与清单哈希），触发时机为 2.2 完成且 G1 判定后 ≤5 分钟
- [ ] 核对两张截图可辨识阶段与关键结果、无凭据明文、PNG 且 ≤10 MB
- **执行目录**：`codearts_proof/images/`
- **预期产物**：`01_dataset_01.png`、`01_dataset_02.png`
- **对应门禁**：G1 证据（失败时采集失败现场并标"失败"状态）
- **失败处理**：无法采集 → 台账标"待补"并列入补采清单，不伪造截图
- **规则追溯**：spec 5.8 R1–R4、4.1 R2、6.1

---

## 3. 训练与 test 留出集验证复刻（阶段二，I2 → 门禁 G2）

### 3.1 固定超参训练 YOLO11n/640（唯一 run-name）
- [ ] 在 WSL `yunshao` 环境执行 SOP 原文训练命令（model_train/README L44，run-name 为隔离参数；固定参数逐字不变）：
```bash
python model_train/3-train-model.py --epochs 100 --imgsz 640 --batch 8 --device 0 --workers 4 --seed 42 --run-name yolo11n_classroom_behavior_$RUN
```
- [ ] 训练启动回显出现后立即采集 `02_train_01.png`（见 3.4 清单，不等训练完成）
- [ ] 训练完成后核对 `model_train/runs/train/yolo11n_classroom_behavior_$RUN/` 与 `model_train/train_result/yolo11n_classroom_behavior_$RUN/`（best/last checkpoint、training.json）落盘，并计算 best/last checkpoint SHA-256
- **执行目录**：WSL `…/project/preparation/model_classroom_behavior`
- **预期产物**：`runs/train/<run-name>/`（Ultralytics 曲线、日志、原始 checkpoint）+ `train_result/<run-name>/`（`yolo11n_classroom_behavior_best.pt`、`_last.pt`、`training.json`）
- **对应门禁**：G2 输入
- **失败处理**：训练中断/失败 → 保留失败运行目录与日志，诊断设备/显存后以新 run-name（`…_retry_01`）重试，产物路径独立不覆盖；改用 CPU 须显式 `--device cpu` 并记录为不同实验
- **规则追溯**：spec 5.3 R1/R2/R3、异常 5.3.3-1

### 3.2 test 留出集验证 best checkpoint
- [ ] 执行 SOP 原文验证命令（model_train/README L52）：
```bash
python model_train/4-validate-model.py --datasets classroom_behavior --split test --checkpoint best --imgsz 640 --batch 8 --device 0
```
- [ ] 确认仅 `--split test` 的结果记录为留出集结果，训练日志 val 指标不得替代
- **执行目录**：WSL `…/project/preparation/model_classroom_behavior`
- **预期产物**：`model_train/runs/validation/` 下 timestamped 目录、`evaluation.json`、`report.csv`、逐类指标
- **对应门禁**：G2 输入
- **失败处理**：脚本异常 → 保留输出诊断后重试；test 指标不达标且绝对门禁已定义 → 以新 run-name 返回训练，test 集保持封存用于模型选择；门禁未定义 → 如实报告测量值标"未定义"（D14）
- **规则追溯**：spec 5.3 R4、异常 5.3.3-2

### 3.3 执行门禁 G2 判定与追溯信息归集
- [ ] 判定：训练退出码 0；`train_result/<run-name>/` 下 best/last checkpoint 与 `training.json` 存在；`--split test` 的 `evaluation.json`/`report.csv` 落盘
- [ ] 归集追溯记录入台账：完整命令、代码版本（`git rev-parse HEAD`）、数据集哈希（取自阶段一）、环境/GPU 版本、训练日志位置、best/last checkpoint 哈希、逐类指标；缺失项标"待补"
- **执行目录**：WSL（读取产物）/ 台账草稿
- **预期产物**：G2 通过/失败结论 + 阶段二台账条目
- **对应门禁**：**G2**（不通过 → 不进入 ONNX 导出，按失败诊断路径处置）
- **失败处理**：G2 失败 → 按"数据/训练/导出/ATC/运行时/阈值"归类诊断后再决定是否重训；重训用新 run-name 并保留失败证据
- **规则追溯**：spec 5.3 R5/R6

### 3.4 采集并归档阶段二截图（3 张）
- [ ] `02_train_01.png`：训练命令与启动回显（可辨识全部超参与 run-name），触发时机为训练启动回显出现后（3.1 内已采集）
- [ ] `02_train_02.png`：训练完成产物清单 + best/last checkpoint SHA-256，触发时机为 3.1 完成后 ≤5 分钟
- [ ] `02_train_03.png`：test 留出集验证输出 + 逐类指标，触发时机为 3.2 完成后 ≤5 分钟
- **执行目录**：`codearts_proof/images/`
- **预期产物**：`02_train_01.png`、`02_train_02.png`、`02_train_03.png`
- **对应门禁**：G2 证据
- **失败处理**：失败现场截图保留并标"失败"，不得删除失败记录只展示成功片段；缺失项标"待补"
- **规则追溯**：spec 4.2 R3、5.8 R3、6.1

---

## 4. ONNX 固定参数导出复刻（阶段三，I3 → 门禁 G3）

### 4.1 记录存量产物哈希并导出 ONNX
- [ ] 导出前记录 `model_export/artifacts/` 内存量 ONNX 与 `export_manifest.json` 的 SHA-256（防无记录覆盖）
- [ ] 在 WSL `yunshao` 环境执行 SOP 原文导出命令（model_export/README L21，附 README 明示的 `--weights-root` 重试参数指向本轮 run 目录）：
```bash
python model_export/export_onnx.py --model-name yolo11n --datasets classroom_behavior --checkpoint best --imgsz 640 --device cpu --opset 17 --weights-root model_train/train_result/yolo11n_classroom_behavior_$RUN
```
- **执行目录**：WSL `…/project/preparation/model_classroom_behavior`
- **预期产物**：`model_export/artifacts/yolo11n_classroom_behavior_best.onnx` + `export_manifest.json`（记录导出时间、Ultralytics 版本、输入尺寸、opset、权重与 ONNX SHA-256）
- **对应门禁**：G3 输入
- **失败处理**：导出失败 → 保留错误输出，诊断导出/依赖问题，禁止进入 ATC 转换
- **规则追溯**：spec 5.4 R1/R2

### 4.2 执行门禁 G3 manifest 五要素核对
- [ ] 逐项核对 `export_manifest.json`：①manifest 存在；②checkpoint SHA-256 = 本轮 `train_result/<run-name>/…_best.pt` 实算值；③ONNX SHA-256 = `artifacts/` 实算值；④输入 shape = 1×3×640×640 静态；⑤类别顺序 = hand-raising、reading、writing
- [ ] 将核对结论登记台账
- **执行目录**：WSL（读取产物）
- **预期产物**：G3 通过/失败结论（差异清单）
- **对应门禁**：**G3**（任一不匹配 → 阻断进入 ATC）
- **失败处理**：核对失败 → 停止并报告具体差异项，禁止进入阶段四；修复（重新导出）后重判
- **规则追溯**：spec 5.4 R3、异常 5.4.3-1

### 4.3 采集并归档阶段三截图（1 张）
- [ ] `03_export_01.png`：导出命令输出 + manifest 内容（可辨识 opset/shape/双哈希），触发时机为 4.1 完成且 4.2 核对后 ≤5 分钟
- **执行目录**：`codearts_proof/images/`
- **预期产物**：`03_export_01.png`
- **对应门禁**：G3 证据
- **失败处理**：失败现场截图保留标"失败"；缺失标"待补"
- **规则追溯**：spec 5.8 R1–R4、6.1

---

## 5. Atlas 板端 ATC 转换复刻（阶段四，I4 → 门禁 G4）

### 5.1 建立板端隔离 run 目录并上传 staged 文件
- [ ] 注入 `export SSHPASS=<安全注入>`（前缀空格规避历史），后续板端访问统一 `sshpass -e`
- [ ] 建立唯一板端 run 目录（对应 model_convert/README L12，IP 替换 + 隔离层，相对结构不变）：
```bash
sshpass -e ssh root@$ATLAS 'mkdir -p /root/model_library/model_classroom_behavior/runs/$RUN/model_convert /root/model_library/model_classroom_behavior/runs/$RUN/model_export/artifacts /root/model_library/model_classroom_behavior/runs/$RUN/model_deployment'
```
- [ ] 上传转换脚本、AIPP 配置与本轮 ONNX（对应 README L13–L14，目标路径指向 run 隔离目录；不传数据集与凭据）：
```bash
sshpass -e scp project/preparation/model_classroom_behavior/model_convert/convert_atc.sh project/preparation/model_classroom_behavior/model_convert/aipp_nv21.cfg root@$ATLAS:/root/model_library/model_classroom_behavior/runs/$RUN/model_convert/
sshpass -e scp project/preparation/model_classroom_behavior/model_export/artifacts/yolo11n_classroom_behavior_best.onnx root@$ATLAS:/root/model_library/model_classroom_behavior/runs/$RUN/model_export/artifacts/
```
- **执行目录**：`C:\Users\miligting\Desktop\Yunshao`（git-bash，自仓库根的相对路径）
- **预期产物**：板端 `runs/$RUN/` 隔离目录含 staged 脚本/AIPP/ONNX；既有模型与运行时文件不受影响
- **对应门禁**：G4 前置（隔离规则）
- **失败处理**：连接失败（IP 不可达/认证失败/网络不通）→ 停止阶段并报告，不回退到非板端转换冒充板端证据；再次实验 → 新 run 目录，既有文件保持原样
- **规则追溯**：spec 5.5 R1/R3、异常 5.5.3-1

### 5.2 板端 CRLF 归一化与 staged ONNX 哈希核对
- [ ] SSH 进入板端 run 根，加载 CANN 环境并核对 staged ONNX 哈希（SOP 原文 L29 上下文 + spec 5.5 R2）：
```bash
cd /root/model_library/model_classroom_behavior/runs/$RUN
source /usr/local/Ascend/ascend-toolkit/set_env.sh
file model_convert/convert_atc.sh | grep -q CRLF && tr -d '\r' < model_convert/convert_atc.sh > model_convert/convert_atc_lf.sh && mv model_convert/convert_atc_lf.sh model_convert/convert_atc.sh
sha256sum model_export/artifacts/yolo11n_classroom_behavior_best.onnx
```
- [ ] 将 staged ONNX 哈希与开发机 `export_manifest.json` 记录核对一致；CRLF 归一化仅作用于隔离副本，记录归一化后脚本哈希，原始仓库文件不动
- **执行目录**：板端 SSH 会话（`runs/$RUN/`）
- **预期产物**：哈希核对一致结论 + 归一化记录
- **对应门禁**：G4 前置（staged 校验规则）
- **失败处理**：staged ONNX 哈希不匹配 → 停止转换并报告差异（重新上传后重核）
- **规则追溯**：spec 5.5 R2/R4、D11

### 5.3 执行 ATC 转换并校验 OM 产物
- [ ] 自 staged 模型根运行转换（SOP 原文 L30，cd 路径指向 run 隔离根）：
```bash
bash model_convert/convert_atc.sh model_export/artifacts/yolo11n_classroom_behavior_best.onnx
```
- [ ] 转换后确认 OM 非空（脚本内建 `test -s` 断言），从 `model_convert/artifacts/yolo11n_classroom_behavior_best/SHA256SUMS` 重算 ONNX/AIPP/OM 三项哈希并核对一致
- [ ] 校验通过后将 OM 复制到 run 专属部署路径 `…/runs/$RUN/model_deployment/yolo11n_classroom_behavior_best.om`（不手改文件名与清单）
- [ ] 记录板卡型号（Atlas 200I DK A2 / Ascend 310B4）、CANN/ATC 版本（基线 7.0.RC1）、执行命令与输出 OM 哈希、文件大小
- **执行目录**：板端 SSH 会话（`runs/$RUN/`）
- **预期产物**：`model_convert/artifacts/yolo11n_classroom_behavior_best/yolo11n_classroom_behavior_best.om` + `SHA256SUMS` + 部署路径副本
- **对应门禁**：**G4**（OM 为空或哈希不符 → 不复制、不进入评估）
- **失败处理**：ATC 转换失败 → 保留错误输出截图，诊断归类（导出/ATC/运行时）后再定重试路径；OM 校验失败 → 禁止复制与评估，报告差异项
- **规则追溯**：spec 5.5 R5、异常 5.5.3-2/3

### 5.4 采集并归档阶段四截图（2 张）
- [ ] `04_convert_01.png`：板端会话（环境加载 + 哈希核对 + ATC 成功与 OM 路径/大小），触发时机为 5.3 转换完成后 ≤5 分钟
- [ ] `04_convert_02.png`：SHA256SUMS 重算核对输出，触发时机为 5.3 校验完成后 ≤5 分钟
- [ ] 核对截图无 `SSHPASS` 等凭据明文（IP `10.160.13.173` 与 `root` 白名单可保留）
- **执行目录**：`codearts_proof/images/`
- **预期产物**：`04_convert_01.png`、`04_convert_02.png`
- **对应门禁**：G4 证据
- **失败处理**：失败现场截图保留标"失败"；缺失标"待补"
- **规则追溯**：spec 4.3 R3、5.8 R1–R4、6.1

---

## 6. 精度、性能与内存评估复刻（阶段五，I5 → 门禁 G5）

### 6.1 开发机 ONNX 端三项评估
- [ ] 在 WSL `yunshao` 环境依次执行 ONNX 端精度/性能/内存评估（SOP 原文 model_val/README L23/L40–L41，`--output` 追加 run-id 隔离段）：
```bash
python model_val/accuracy_eval.py --backend onnx --model model_export/artifacts/yolo11n_classroom_behavior_best.onnx --data model_train/datasets/classroom_behavior/data.yaml --split test --output model_val/reports/release/$RUN/onnx --imgsz 640 --conf 0.25 --iou 0.45 --device 0 --warmup 5 --metric-mode native
python model_val/performance_eval.py --backend onnx --model model_export/artifacts/yolo11n_classroom_behavior_best.onnx --data model_train/datasets/classroom_behavior/data.yaml --split test --output model_val/reports/release/$RUN/onnx_performance.json --imgsz 640 --conf 0.25 --iou 0.45 --device 0 --warmup 5
python model_val/memory_eval.py --backend onnx --model model_export/artifacts/yolo11n_classroom_behavior_best.onnx --data model_train/datasets/classroom_behavior/data.yaml --split test --output model_val/reports/release/$RUN/onnx_memory.json --imgsz 640 --device 0 --limit 1
```
- [ ] 确认精度报告为正式口径：`--metric-mode native --conf 0.25`、无 `--limit`（memory 的 `--limit 1` 为 SOP 允许项）
- **执行目录**：WSL `…/project/preparation/model_classroom_behavior`
- **预期产物**：`onnx_report.json` + 逐类 CSV、`onnx_performance.json`、`onnx_memory.json`（均位于 `model_val/reports/release/$RUN/`）
- **对应门禁**：G5 输入
- **失败处理**：评估中断 → 保留输出诊断后重跑；数据缺失/图片不可读 → 先修复或明确记录差异，不得声称完整集结果
- **规则追溯**：spec 5.6 R2/R4

### 6.2 同步数据与脚本至板端 run 目录
- [ ] 将同一 `data.yaml`、完整数据快照（含 test split 数据）、`model_val/*.py` 及本轮 OM 上传至板端 run 根，保持相对路径与文件内容一致（model_val/README L26 要求）：
```bash
sshpass -e scp -r project/preparation/model_classroom_behavior/model_train/datasets/classroom_behavior root@$ATLAS:/root/model_library/model_classroom_behavior/runs/$RUN/model_train/datasets/
sshpass -e scp -r project/preparation/model_classroom_behavior/model_val root@$ATLAS:/root/model_library/model_classroom_behavior/runs/$RUN/
```
- [ ] 核对两端数据路径与图像/标签清单哈希一致
- **执行目录**：`C:\Users\miligting\Desktop\Yunshao`（git-bash）
- **预期产物**：板端 run 根下与开发机同快照、同相对结构的数据与脚本
- **对应门禁**：G5 前置（同条件评估规则）
- **失败处理**：传输失败 → 重试；不得改变相对结构；快照不一致 → 视为无效对比，重新同步
- **规则追溯**：spec 5.6 R1、异常 5.6.3-2

### 6.3 板端 OM 端三项评估与 npu-smi 快照
- [ ] SSH 进入板端 run 根，加载 CANN 环境，评估前采集 `npu-smi info` 快照（含温度/健康/并发负载），再依次执行 OM 端三项评估（SOP 原文 L29–L30/L50–L51，输出 run-id 隔离段），评估后再次采集 `npu-smi info` 快照：
```bash
cd /root/model_library/model_classroom_behavior/runs/$RUN
source /usr/local/Ascend/ascend-toolkit/set_env.sh
npu-smi info
/usr/local/miniconda3/bin/python model_val/accuracy_eval.py --backend om --model model_deployment/yolo11n_classroom_behavior_best.om --data model_train/datasets/classroom_behavior/data.yaml --split test --output model_val/reports/release/$RUN/om --imgsz 640 --conf 0.25 --iou 0.45 --device 0 --warmup 5 --metric-mode native
/usr/local/miniconda3/bin/python model_val/performance_eval.py --backend om --model model_deployment/yolo11n_classroom_behavior_best.om --data model_train/datasets/classroom_behavior/data.yaml --split test --output model_val/reports/release/$RUN/om_performance.json --imgsz 640 --conf 0.25 --iou 0.45 --device 0 --warmup 5
/usr/local/miniconda3/bin/python model_val/memory_eval.py --backend om --model model_deployment/yolo11n_classroom_behavior_best.om --data model_train/datasets/classroom_behavior/data.yaml --split test --output model_val/reports/release/$RUN/om_memory.json --imgsz 640 --device 0 --limit 1
npu-smi info
```
- [ ] 记录板卡温度、健康状态、CANN/运行时版本（7.0.RC1 / Miniconda Python 3.9.2）与并发负载入台账
- **执行目录**：板端 SSH 会话（`runs/$RUN/`）
- **预期产物**：`om_report.json` + 逐类 CSV、`om_performance.json`、`om_memory.json` + 前后两次 `npu-smi` 快照
- **对应门禁**：G5 输入
- **失败处理**：评估中断/数据不完整 → 记录差异修复后重跑；RSS（主机进程内存）与 npu-smi（设备级内存）不得互相冒充
- **规则追溯**：spec 5.6 R4/R6、异常 5.6.3-2

### 6.4 执行等价性对比与门禁 G5 判定
- [ ] 将 OM 端报告 scp 回开发机相同 `model_val/reports/release/$RUN/` 目录，再执行 SOP 原文等价对比（model_val/README L59）：
```bash
python model_val/precision_eval.py --onnx-report model_val/reports/release/$RUN/onnx/onnx_report.json --om-report model_val/reports/release/$RUN/om/om_report.json --output model_val/reports/release/$RUN/onnx_vs_om --max-map-drop 0.02 --max-class-ap50-drop 0.05
```
- [ ] 判定 G5 四要素：①两报告 `metric_mode=native`、conf=0.25、iou=0.45 完全一致（不一致视为无效对比重跑）；②正式报告无 `--limit`；③parity：mAP50-95 降幅 ≤0.02 且逐类 AP50 降幅 ≤0.05（等价性判断，非绝对质量目标）；④性能/内存报告完整（FPS、延迟分位、模型大小、峰值 RSS、张量大小、npu-smi 前后快照）
- [ ] 绝对 mAP/FPS/内存阈值：项目未定义 → 结论区如实标"未定义"，仅报告测量值，禁止发明通过阈值
- [ ] 将等价结论与环境记录登记台账
- **执行目录**：WSL `…/project/preparation/model_classroom_behavior`
- **预期产物**：`onnx_vs_om/` 对比 JSON/CSV + G5 通过/失败结论
- **对应门禁**：**G5**（不通过 → 禁止进入 demo 阶段）
- **失败处理**：parity 失败 → 先检查哈希、预处理、AIPP、类别顺序与运行时差异，未定位前不重训；绝对门禁（若已定义）失败 → 以新 run-name 返回训练，test 集封存
- **规则追溯**：spec 5.6 R1/R3/R5、异常 5.6.3-1/3

### 6.5 采集并归档阶段五截图（3 张）
- [ ] `05_val_01.png`：ONNX 端 accuracy_eval 输出（mAP50-95 与逐类 AP50），触发时机为 6.1 完成后 ≤5 分钟
- [ ] `05_val_02.png`：板端 OM 端同级输出，触发时机为 6.3 完成后 ≤5 分钟
- [ ] `05_val_03.png`：precision_eval 对比结论 + performance/memory 摘要与 npu-smi 快照，触发时机为 6.4 完成后 ≤5 分钟
- **执行目录**：`codearts_proof/images/`
- **预期产物**：`05_val_01.png`、`05_val_02.png`、`05_val_03.png`
- **对应门禁**：G5 证据
- **失败处理**：失败现场截图保留标"失败"；缺失标"待补"
- **规则追溯**：spec 5.8 R1–R4、6.1

---

## 7. H.264 部署演示验收复刻（阶段六，I6 → 门禁 G6）

### 7.1 准备输入视频与板端源码 staged
- [ ] 确认前置门禁 G5 已通过（评估未通过时本阶段不启动）
- [ ] 按输入优先级准备 H.264 输入：授权 H.264 课堂样例或 RTSP 源 → 仓库 `video/classroom_behavior.mp4`（HEVC 时按 SOP 原文 model_deployment/README L21 转码）→ 从留出 test 图像生成合成烟测视频（置于板端 `acceptance_$RUN/`，命名清晰并明确标注"软件链路烟测"，不声称真实课堂流或产品验收）：
```bash
ffmpeg -y -i video/classroom_behavior.mp4 -vf 'scale=trunc(iw/2)*2:trunc(ih/2)*2' -c:v libx264 -pix_fmt yuv420p -an /tmp/classroom_behavior_h264.mp4
sshpass -e scp /tmp/classroom_behavior_h264.mp4 root@$ATLAS:/root/model_library/model_classroom_behavior/runs/$RUN/model_deployment/
```
- [ ] 上传 `model_deployment` 源码（`CMakeLists.txt`、`src/`、`classes.txt`）至板端 run 目录：
```bash
sshpass -e scp -r project/preparation/model_classroom_behavior/model_deployment/CMakeLists.txt project/preparation/model_classroom_behavior/model_deployment/src project/preparation/model_classroom_behavior/model_deployment/classes.txt root@$ATLAS:/root/model_library/model_classroom_behavior/runs/$RUN/model_deployment/
```
- **执行目录**：`C:\Users\miligting\Desktop\Yunshao`（git-bash）
- **预期产物**：板端 `runs/$RUN/model_deployment/` 含源码、classes.txt、H.264 输入视频
- **对应门禁**：G6 前置（G5 通过 + 输入就绪）
- **失败处理**：无可用输入源 → 采用合成烟测方案并在记录中标注；源码上传失败 → 重试
- **规则追溯**：spec 5.7 R1/R5、D12

### 7.2 CMake 全新构建与类别顺序校验
- [ ] SSH 进入板端 run 部署目录，加载 CANN 环境后以全新 `build_$RUN` 目录构建（SOP 原文 L33–L37，构建目录按 spec 5.7 R2 隔离）：
```bash
cd /root/model_library/model_classroom_behavior/runs/$RUN/model_deployment
source /usr/local/Ascend/ascend-toolkit/set_env.sh
cmake -S . -B build_$RUN -DCMAKE_BUILD_TYPE=Release
cmake --build build_$RUN -j2
```
- [ ] 核对 `classes.txt` 三行顺序 = hand-raising、reading、writing（与 `data.yaml`/模型输出一致；不符则 demo 必须明确失败，禁止改标签绕过）
- [ ] 确认使用本轮 run 专属 OM（5.3 已复制至部署路径）与 classes.txt，不与其他轮次混用
- **执行目录**：板端 SSH 会话（`runs/$RUN/model_deployment/`）
- **预期产物**：`build_$RUN/atlas_yolo_acl_demo` 可执行文件
- **对应门禁**：G6 前置（类别顺序规则）
- **失败处理**：构建失败 → 诊断 CANN ACL/DVPP、OpenCV C++、PkgConfig、FFmpeg 开发依赖；类别顺序错误或形状不匹配 → 明确失败退出，不输出含糊成功
- **规则追溯**：spec 5.7 R2/R3

### 7.3 运行 demo 并记录运行统计
- [ ] 运行 demo（SOP 原文 L46，demo 二进制位于 `build_$RUN/`，VPSS/Region 默认避开产品占用的 group 0/handle 0）：
```bash
./build_$RUN/atlas_yolo_acl_demo --model yolo11n_classroom_behavior_best.om --video classroom_behavior_h264.mp4 --output detected.h264 --labels classes.txt --conf 0.25 --iou 0.45 --vpss-group 1 --region-handle 1
```
- [ ] 记录运行统计：退出码、模型输出形状、处理/编码帧数、有效 FPS、推理/后处理/OSD/编码耗时、峰值 RSS、输出 `detected.h264` 的 SHA-256；使用合成视频时明确标注"软件链路烟测"
- **执行目录**：板端 SSH 会话（`runs/$RUN/model_deployment/`）
- **预期产物**：`detected.h264` + 完整运行统计记录
- **对应门禁**：G6 输入
- **失败处理**：VPSS group / Region handle 资源冲突 → 改选空闲 ID（`--vpss-group <空闲组号>` / `--region-handle <空闲编号>`）重试并记录所换 ID，不动运行中的课堂产品服务
- **规则追溯**：spec 5.7 R4/R6、异常 5.7.3-1

### 7.4 ffprobe 输出验证与门禁 G6 判定
- [ ] 执行 SOP 原文输出验证（model_deployment/README L60）：
```bash
ffprobe -v error -f h264 -count_frames -show_entries stream=codec_name,width,height,nb_read_frames -of default=noprint_wrappers=1 detected.h264
```
- [ ] 判定 G6：demo 退出码 0 且运行统计齐全；ffprobe 三条件——codec=H.264、输出非空可解码、`nb_read_frames` = demo 日志处理帧数
- **执行目录**：板端 SSH 会话（`runs/$RUN/model_deployment/`）
- **预期产物**：G6 通过/失败结论 + ffprobe 输出记录
- **对应门禁**：**G6**（不通过 → 判定失败并保留错误证据）
- **失败处理**：输出为空、不可解码或帧数与日志不符 → 判定失败，保留错误输出截图与日志，禁止把失败烟测包装成验收通过
- **规则追溯**：spec 5.7 R6、异常 5.7.3-2

### 7.5 采集并归档阶段六截图（3 张）
- [ ] `06_demo_01.png`：CMake 构建成功输出（可辨识 `build_$RUN`），触发时机为 7.2 完成后 ≤5 分钟
- [ ] `06_demo_02.png`：demo 运行统计（模型输出形状/帧数/FPS/耗时/RSS/退出码），触发时机为 7.3 完成后 ≤5 分钟
- [ ] `06_demo_03.png`：ffprobe 验证输出，触发时机为 7.4 完成后 ≤5 分钟
- **执行目录**：`codearts_proof/images/`
- **预期产物**：`06_demo_01.png`、`06_demo_02.png`、`06_demo_03.png`
- **对应门禁**：G6 证据
- **失败处理**：失败现场截图保留标"失败"；缺失标"待补"
- **规则追溯**：spec 5.8 R1–R4、6.1

---

## 8. 证据截图归档与 dev 分支提交推送（I7 + C1–C3）

### 8.1 截图命名合规校验与脱敏检查（C2/C3）
- [ ] 以正则 `^(01_dataset|02_train|03_export|04_convert|05_val|06_demo)_[0-9]{2}\.png$` 全量校验 `codearts_proof/images/` 内文件名，不合规者归档前重命名
- [ ] 核对单张 ≤10 MB（超限压缩后归档）、PNG 格式、画面可辨识阶段与关键结果（无全黑/空白画面）
- [ ] 逐一检查截图画面：无 API key、密码、私钥、访问令牌等敏感信息（IP `10.160.13.173` 与 `root` 为白名单可保留）
- **执行目录**：`codearts_proof/images/`
- **预期产物**：命名与脱敏双通过的证据集
- **对应门禁**：ARCHIVE 门禁（截图 ≥6 张且每阶段 ≥1 张、命名合规、脱敏通过）
- **失败处理**：含敏感信息 → 脱敏处理后重新归档，原始含敏文件不入库；命名不合规 → 重命名后复验
- **规则追溯**：spec 5.8 R2/R4、6.1、异常 5.8.3-3

### 8.2 编写截图索引与证据台账
- [ ] 创建 `codearts_proof/images/README.md` 索引文件：每条记录含阶段（01_dataset~06_demo）、命令、截图文件名、采集时间、状态（成功/失败/待补，失败附原因摘要）
- [ ] 编写台账章节：Git 版本/分支、各阶段命令与退出码、数据集来源与哈希、类别顺序、训练参数与 checkpoint 哈希、ONNX/OM 清单与哈希、评估与等价结论、性能/内存/NPU 快照摘要、demo 输入来源与输出哈希、发布验证记录、三类可变参数取值理由；缺失项统一标注"待补"
- **执行目录**：`codearts_proof/images/`
- **预期产物**：`codearts_proof/images/README.md`（索引 + 台账）
- **对应门禁**：ARCHIVE 门禁输入
- **失败处理**：某阶段无截图 → 台账标"待补"并列入补采清单，不伪造截图
- **规则追溯**：spec 5.8 R3、6.2、D9

### 8.3 白名单暂存与证据提交
- [ ] 提交前三查（status/diff/暂存清单）并核对白名单（仅 `codearts_proof/images/` 及索引文件）：
```bash
git status --short && git diff --check
git add codearts_proof/images/
git commit -m "proof: classroom rnd cycle evidence ($RUN)"
```
- [ ] 核对暂存清单排除：数据集、PT 权重、ONNX、OM、生成视频、凭据文件、无关用户改动（`D codearts_proof/skills/…/SKILL.md` 删除态、`.arts/`、`yolo11n.pt`、`yolo26n.pt` 均不得混入）；提交消息不含凭据
- **执行目录**：`C:\Users\miligting\Desktop\Yunshao`（git-bash，临时分支上）
- **预期产物**：仅含证据截图与索引的证据提交
- **对应门禁**：PUBLISH 前置（提交范围规则）
- **失败处理**：暂存清单出现排除项 → 移出后重新核对再提交
- **规则追溯**：spec 5.8 R5、6.3、异常 5.1.3-3

### 8.4 合并临时分支并推送远程 dev 分支
- [ ] 将临时分支 fast-forward 合并至本地 dev 并推送远程（D10 发布路径；2026-10-22 修订：仅提交 dev 分支，不执行任何针对 main 的合并与推送）：
```bash
git checkout dev && git merge --ff-only proof/classroom_rnd_proof_$RUN
git push origin dev
```
- [ ] 确认推送目标仅为 `origin/dev`，全程无 `git push origin main` 及任何 main 分支合并操作
- **执行目录**：`C:\Users\miligting\Desktop\Yunshao`（git-bash）
- **预期产物**：远程 `dev` 分支收到证据提交（`origin/dev` 最新提交包含 `codearts_proof/images/`）
- **对应门禁**：PUBLISH → DONE 转移条件
- **失败处理**：推送被拒（远程 dev 有新提交）→ `git fetch origin` 后 ff 合并远端新提交重新推送（禁止 force、禁止删除远程分支）；推送失败（网络/权限）→ 重试并复验远程内容，未确认前不宣告完成
- **规则追溯**：spec 5.8 R6/R7、异常 5.8.3-1/2

### 8.5 远程 dev 分支内容验证
- [ ] 推送完成后验证远程 dev 分支证据路径存在且内容与本地提交一致（2026-10-22 修订：由双分支一致性验证改为单 dev 分支验证）：
```bash
git ls-remote origin dev
git fetch origin && git ls-tree -r origin/dev codearts_proof/images/
```
- [ ] 核对远程 dev 分支 `codearts_proof/images/` 文件清单与 blob 哈希同本地证据提交完全一致（含全部截图与索引文件）
- **执行目录**：`C:\Users\miligting\Desktop\Yunshao`（git-bash）
- **预期产物**：dev 分支远程内容验证输出（记入台账发布验证记录）
- **对应门禁**：DONE 达成条件
- **失败处理**：远程内容缺失或不一致 → 重推 `origin/dev` 后复验；推送后发现敏感信息已入库 → 脱敏替换文件提交修正并核查是否需作废旧敏感文件
- **规则追溯**：spec 4.2 R2、4.5 R1、5.8 R6、异常 5.8.3-2/3

---

## 9. 端到端验收与收尾

### 9.1 六阶段证据完整性核验
- [ ] 核对 `codearts_proof/images/`：六个阶段每阶段 ≥1 张、全流程 ≥6 张（计划 14 张）；失败阶段以失败现场截图计入
- [ ] 逐张核对画面可辨识所属阶段（命令/路径/报告文件名）与关键结果（成功/失败状态、核心指标或产物名）
- [ ] 缺失项在索引/台账中标注"待补"并列入补采清单，不伪造截图
- **执行目录**：`codearts_proof/images/`
- **预期产物**：证据完整性核验结论
- **对应门禁**：最终验收（对应 4.2 R1）
- **失败处理**：证据不完整 → 补采或"待补"标注后再交付
- **规则追溯**：spec 4.2 R1、5.8 R3、异常 5.8.3-4

### 9.2 EARS 规则追溯核对
- [ ] 对照附录 A 追溯表逐条核对 42 条 EARS 业务规则的落点任务与执行证据（截图/台账/命令输出）
- [ ] 未覆盖或证据缺失项回补执行或标注
- **执行目录**：`C:\Users\miligting\Desktop\Yunshao`
- **预期产物**：42 条规则全覆盖核对结论
- **对应门禁**：最终验收
- **失败处理**：发现规则未落实 → 回到对应任务组补救后复验
- **规则追溯**：spec 全部 42 条（见附录 A）

### 9.3 敏感信息与提交范围终检
- [ ] 以 `git show --stat HEAD`（HEAD 即 dev 分支证据提交）核对提交内容仅 `codearts_proof/images/` 及索引文件，无数据集/权重/ONNX/OM/视频/凭据
- [ ] 核对全程无 force push、无删除远程分支记录；截图终检无敏感明文
- **执行目录**：`C:\Users\miligting\Desktop\Yunshao`（git-bash）
- **预期产物**：安全与范围终检结论
- **对应门禁**：最终验收（对应 4.3/6.3）
- **失败处理**：发现违禁内容 → 按脱敏修正提交处理并复查
- **规则追溯**：spec 4.3 R1–R5、6.3、5.8 R7

### 9.4 输出最终完成报告
- [ ] 汇总交付：任务清单摘要（编号/标题/一句话说明）、任务总数与截图数、执行顺序说明、G1–G6 各门禁结论、失败与"待补"项清单、各阶段截图清单
- [ ] 向操作员交付完成报告并归档结论至台账
- **执行目录**：`C:\Users\miligting\Desktop\Yunshao`
- **预期产物**：最终完成报告
- **对应门禁**：DONE 收口
- **失败处理**：存在未闭环项 → 在报告中显式列出，不掩盖
- **规则追溯**：spec 1.3（核心输出全集）

---

## 附录 A：42 条 EARS 业务规则 → 任务映射追溯表

| spec 模块 | 规则 | 落点任务 |
|---|---|---|
| 5.1 R1 Skill 前置 | 1.1 | |
| 5.1 R2 SOP 命令唯一来源 | 1.1 + 各阶段命令任务（命令逐字取自 README） | |
| 5.1 R3 依赖变更约束 | 1.3 | |
| 5.1 R4 Git 基线 | 1.2 | |
| 5.1 R5 禁止项（跳序/虚构） | 1.1/1.2 + 全局执行顺序（第 9 组复验） | |
| 5.2 R1 数据源固定 | 2.1 | |
| 5.2 R2 凭据注入 | 2.1 + 全局凭据约定 | |
| 5.2 R3 训练前完整性校验 | 2.3 | |
| 5.2 R4 禁止缺 test 标签训练 | 2.3 | |
| 5.3 R1 环境规则 | 1.3、3.1 | |
| 5.3 R2 固定超参 | 3.1 | |
| 5.3 R3 唯一 run-name | 1.4、3.1（失败处理） | |
| 5.3 R4 留出集规则 | 3.2 | |
| 5.3 R5 失败重试规则 | 3.1/3.3（失败处理） | |
| 5.3 R6 追溯记录规则 | 3.3、8.2 | |
| 5.4 R1 导出对象规则 | 4.1 | |
| 5.4 R2 固定导出参数 | 4.1 | |
| 5.4 R3 导出校验规则 | 4.2 | |
| 5.5 R1 板卡与环境规则 | 5.1 + 全局凭据约定 | |
| 5.5 R2 staged 校验规则 | 5.2 | |
| 5.5 R3 隔离与实验目录规则 | 5.1 | |
| 5.5 R4 换行符归一化规则 | 5.2 | |
| 5.5 R5 OM 校验规则 | 5.3 | |
| 5.6 R1 同条件评估规则 | 6.2、6.4 | |
| 5.6 R2 完整评估规则 | 6.1、6.3 | |
| 5.6 R3 等价门禁规则 | 6.4 | |
| 5.6 R4 性能与内存报告规则 | 6.1、6.3、6.4 | |
| 5.6 R5 绝对阈值规则 | 6.4 | |
| 5.6 R6 环境记录规则 | 6.3、8.2 | |
| 5.7 R1 前置门禁规则 | 7.1 | |
| 5.7 R2 构建规则 | 7.2 | |
| 5.7 R3 类别顺序规则 | 7.2 | |
| 5.7 R4 产品隔离规则 | 7.3（失败处理） | |
| 5.7 R5 输入来源规则 | 7.1 | |
| 5.7 R6 运行记录规则 | 7.3、7.4 | |
| 5.8 R1 归档路径规则 | 1.4、8.1 | |
| 5.8 R2 命名规则 | 8.1 | |
| 5.8 R3 完整性规则 | 9.1 | |
| 5.8 R4 脱敏规则 | 8.1、9.3 | |
| 5.8 R5 提交范围规则 | 8.3 | |
| 5.8 R6 dev 分支推送规则 | 8.4、8.5 | |
| 5.8 R7 禁止项（强推/删分支/包装失败） | 8.4、9.3 | |

## 附录 B：截图采集计划总表（14 张）

| 序号 | 文件名 | 阶段/门禁 | 内容要求 | 触发时机 |
|---|---|---|---|---|
| 1 | `01_dataset_01.png` | 阶段一 / G1 | 下载命令完成输出（可辨识 provider/version） | 2.1 完成后 ≤5 分钟 |
| 2 | `01_dataset_02.png` | 阶段一 / G1 | 分析输出 + dataset_summary.json 关键字段（data.yaml 哈希与清单哈希） | 2.2 完成且 G1 判定后 ≤5 分钟 |
| 3 | `02_train_01.png` | 阶段二 / G2 | 训练命令与启动回显（全部超参与 run-name） | 训练启动回显出现后立即 |
| 4 | `02_train_02.png` | 阶段二 / G2 | 训练完成产物清单 + best/last checkpoint SHA-256 | 3.1 完成后 ≤5 分钟 |
| 5 | `02_train_03.png` | 阶段二 / G2 | test 留出集验证输出 + 逐类指标 | 3.2 完成后 ≤5 分钟 |
| 6 | `03_export_01.png` | 阶段三 / G3 | 导出命令输出 + manifest 内容（opset/shape/双哈希） | 4.1 完成且 G3 核对后 ≤5 分钟 |
| 7 | `04_convert_01.png` | 阶段四 / G4 | 板端会话（环境加载+哈希核对+ATC 成功与 OM 路径/大小） | 5.3 转换完成后 ≤5 分钟 |
| 8 | `04_convert_02.png` | 阶段四 / G4 | SHA256SUMS 重算核对输出 | 5.3 校验完成后 ≤5 分钟 |
| 9 | `05_val_01.png` | 阶段五 / G5 | ONNX 端 accuracy_eval 输出（mAP50-95 与逐类 AP50） | 6.1 完成后 ≤5 分钟 |
| 10 | `05_val_02.png` | 阶段五 / G5 | 板端 OM 端同级输出 | 6.3 完成后 ≤5 分钟 |
| 11 | `05_val_03.png` | 阶段五 / G5 | precision_eval 对比结论 + performance/memory 摘要 + npu-smi 快照 | 6.4 完成后 ≤5 分钟 |
| 12 | `06_demo_01.png` | 阶段六 / G6 | CMake 构建成功输出（可辨识 build_$RUN） | 7.2 完成后 ≤5 分钟 |
| 13 | `06_demo_02.png` | 阶段六 / G6 | demo 运行统计（输出形状/帧数/FPS/耗时/RSS/退出码） | 7.3 完成后 ≤5 分钟 |
| 14 | `06_demo_03.png` | 阶段六 / G6 | ffprobe 验证输出 | 7.4 完成后 ≤5 分钟 |

> 说明：design.md D8 记载计划数为 12 张（概数），本文按 I1–I6 截图采集点明细汇总为 14 张，满足 spec"每阶段 ≥1 张、合计 ≥6 张"下限；失败阶段以失败现场截图计入。

## 附录 C：执行顺序与依赖关系

**强制串行链**（WORKFLOW 阶段顺序 + 门禁单向放行）：

```
1 准备(I0) → 2 阶段一(I1/G1) → 3 阶段二(I2/G2) → 4 阶段三(I3/G3)
→ 5 阶段四(I4/G4) → 6 阶段五(I5/G5) → 7 阶段六(I6/G6) → 8 归档发布(I7) → 9 端到端验收
```

**关键依赖**：
- 每阶段门禁（G1–G6）未通过不得进入下一阶段；失败证据保留后按各任务"失败处理"路径处置（重试启用新 run-id/run-name，路径隔离不污染）。
- 3.1 依赖 2.3（G1 通过）；4.1 依赖 3.3（G2 通过）；5.1 依赖 4.2（G3 通过）；6.1/6.2 依赖 5.3（G4 通过）；7.1 依赖 6.4（G5 通过）；8.3 依赖 8.1/8.2（归档门禁）；8.4 依赖 8.3；8.5 依赖 8.4；9.* 依赖 8.5。
- 组内并行点：6.1（开发机 ONNX 侧）与 6.2（数据同步）可在 G4 通过后并行开展；6.3 依赖 6.2 完成。
- 截图采集（各阶段 .4/.5 子任务）依附对应命令任务，≤5 分钟时效约束要求命令完成后立即执行。

**宿主路由**：任务组 2/3/4、6.1、6.4 在 WSL `yunshao` 环境；5、6.2–6.3、7 在板端 SSH（凭据经 `sshpass -e` 临时注入）；1、8、9 在开发机 Windows git-bash。

**与门禁的对应总览**：G1→2.3、G2→3.3、G3→4.2、G4→5.3、G5→6.4、G6→7.4、ARCHIVE 门禁→8.1/8.2、PUBLISH/DONE 门禁→8.4/8.5、最终验收→9.1–9.3。