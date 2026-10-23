# G0–G6 门禁清单

逐项打勾。任何一项打不了勾，就停在这一门，不要带着未验证项往下走。

## G0 场景立项

- [ ] 场景 id 已确认：`model_<snake_case>`
- [ ] 五阶段目录齐套：`model_train` / `model_export` / `model_convert` / `model_val` / `model_deployment`
- [ ] `WORKFLOW.md` 只做阶段索引，根部没有总 README
- [ ] `environment.yml`、`.gitignore` 就位
- [ ] 目标物体与类别序已写进场景档案
- [ ] 数据源 provider / project / version / license 已记录
- [ ] 既有场景零改动（`git status` 里没有其他场景的改动）
- [ ] 板端型号、CANN 版本已确认

## G1 数据集校验

- [ ] 下载命令与来源元数据已记录（provider、workspace、project、version）
- [ ] `data.yaml` 内容与哈希已记录
- [ ] train / valid / test 三划分均非空
- [ ] 每张图都有对应标签，无缺标
- [ ] 类别 ID 与 `data.yaml` 的 names 顺序一致
- [ ] 标签可解析，无越界坐标
- [ ] 跨 split 重复已检测并处理（记录原始数、重复数、清洗后数）
- [ ] 清洗后的划分数量已记录

## G2 训练 + 验证

- [ ] conda 环境由模型本地 `environment.yml` 创建，环境名与版本已记录
- [ ] 本次 run 使用唯一 `--run-name`，未覆盖历史 run
- [ ] 训练超参与 SOP 一致（基座、epoch、batch、device、workers、seed、deterministic、patience）
- [ ] 训练日志与 best/last 权重的 SHA-256 已记录
- [ ] valid 划分出完整指标（逐类 P/R/mAP）
- [ ] test 划分全程未参与调参
- [ ] 失败重试换用了新 run-name，失败证据保留

## G3 ONNX 导出

- [ ] 用已验证的 checkpoint 导出
- [ ] 输入固定 1×3×640×640、静态 shape、opset 17
- [ ] `export_manifest.json` 生成，五要素齐全（batch / 尺寸 / opset / 类别序 / 权重哈希）
- [ ] checkpoint 与 ONNX 的 SHA-256 已记录
- [ ] 类别序与训练、评估、demo 三处一致

## G4 板端 ATC

- [ ] 板端使用 SSH key 登录，未在源码或日志中出现密码、私钥、内网 IP
- [ ] 本次实验有独立板端 run 目录，既有模型与 runtime 文件未被覆盖
- [ ] 已 `source /usr/local/Ascend/ascend-toolkit/set_env.sh`
- [ ] 上板 ONNX 哈希与 `export_manifest.json` 一致
- [ ] CRLF 脚本已就地转 LF，转换后哈希已记录
- [ ] ATC 日志无 error，OM 非空
- [ ] `SHA256SUMS` 中 ONNX / AIPP / OM 三件复验一致

## G5 双端评估

- [ ] ONNX 与 OM 使用同一数据快照、同一 `data.yaml`、同一 test 划分
- [ ] 两侧 `--metric-mode native --conf 0.25`，均未加 `--limit`
- [ ] 两侧报告的 `metric_mode`、conf、IoU 完全一致
- [ ] parity 对比跑过：mAP50-95 降幅与逐类 AP50 降幅均已记录
- [ ] 降幅在项目阈值内（缺省 0.02 / 0.05）
- [ ] 性能：FPS 与延迟分位已记录（板端与开发机分开）
- [ ] 内存：进程峰值 RSS 已记录，另有 `npu-smi` 前后快照
- [ ] 板卡温度、健康状态、CANN/runtime 版本、并发负载已记录
- [ ] 无项目阈值时，已明确标注「绝对模型质量门槛：未指定」

## G6 H.264 演示

- [ ] 仅在 G5 通过后编译，`build_<run-id>` 为全新目录
- [ ] 未停止或重配正在运行的产品服务
- [ ] VPSS group 与 Region handle 使用空闲 ID（默认 1 / 1），资源冲突已按错误提示换 ID
- [ ] 输入素材来源已记录（真实流 / 留出集合成片），合成片明确标注为烟测
- [ ] 退出码、处理帧数、编码帧数、有效 FPS、峰值 RSS、输出 SHA-256 已记录
- [ ] `ffprobe` 确认输出非空、可解码、编码为 H.264、帧数与日志一致
- [ ] 未把合成烟测表述为真实场景或产品验收
