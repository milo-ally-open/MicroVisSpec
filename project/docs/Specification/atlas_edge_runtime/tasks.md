# Atlas 200I DK A2 边缘运行时与云边代理实施任务

> 关联合同：[spec.md](spec.md)；设计：[design.md](design.md)。本清单记录计划和证据，不代表当前已有实现。

## 任务清单

- [ ] **T-01 Atlas 构建与运行依赖基线** — 核对 CANN/OpenCV/FFmpeg/SQLite、aarch64 TLS MQTT client、systemd 权限；锁定可复现交叉/板端构建方案和包版本；覆盖 REQ-01、REQ-02、REQ-03 / AC-01.1、AC-02.2、AC-03.1；依赖：无。
- [ ] **T-02 agent 设备身份与配置心跳** — 消费激活响应的 broker 地址、原样 `client_id`、独立凭据和 CA；发布符合 device-service 合同的设备/NPU/模型/摄像头/流/MQTT/outbox 状态心跳；覆盖 topic ACL、配置版本/回执与状态 API/MQTT；覆盖 REQ-01、REQ-02 / AC-01.1–01.4、AC-02.1、AC-02.2、AC-02.3、AC-02.5；依赖：T-01、device-service。
- [ ] **T-03 多模型配置、模型目录和推理进程管理** — 参照 AX650 plugin/manager 分层，在 `config/models.json` 定义模型与支持能力、`streams_config.json` 定义逐流 model_id 绑定、`alarm_rules.json` 定义数据驱动且默认关闭的告警；实现 profile 与活动 manifest 的类别/SoC/格式/尺寸匹配、通用 catalog 不固化课堂类别、模型按 model_id 哈希校验和 staging 原子激活、`active/{model_id}` 独立回滚、最大加载数限制、NPU 状态与 Unix socket；插件、model_id、流/规则/manifest 使用 `model_` 前缀和领域名、任务类型组成的统一命名（task 为 detection/segmentation/classification），插件类和 `IAIModel::name()` 与 model_id 一致，算法键和模型族保持各自语义；媒体接口与实现按 `include/media/*.hpp` ↔ `src/media/*.cpp` 同基名配对；`include/common/core.hpp` ↔ `src/common/core.cpp`、`include/common/config.hpp` ↔ `src/common/config.cpp` 同基名配对；Atlas 实现使用 AscendCL/DVPP，不复用 AX650 SDK；覆盖 REQ-03 / AC-03.1、AC-03.2、AC-03.3、AC-03.4、AC-03.5、AC-03.6、AC-03.7、AC-03.8、AC-03.9、AC-03.10，REQ-02 / AC-02.4；依赖：T-01。课堂检测 T-01/T-02 消费此任务提供的 profile/活动模型接口，不构成反向依赖。
- [ ] **T-04 SQLite 事件 outbox 与 ACK 恢复** — 实现稳定 ID、QoS1、accepted/duplicate 完成、rejected quarantine、重连/重启/磁盘告警；覆盖 REQ-04 / AC-04.1、AC-04.2、AC-04.3、AC-04.4、AC-04.5、AC-04.6；依赖：T-02、event-service。
- [ ] **T-05 媒体及证据云边接入** — 将授权输入、处理后实时流、规则触发截图接入 media/event 服务；覆盖 REQ-05 / AC-05.1、AC-05.2、AC-05.3；依赖：T-03/T-04、MediaMTX。
- [ ] **T-06 服务化部署与运维恢复** — systemd 单元、非特权运行、配置/密钥及必需 CANN 环境文件注入、动态库解析、最小设备组和 udev RGN 权限、升级/回滚、日志与健康探针；覆盖 REQ-02、REQ-03、REQ-04、REQ-06 / AC-02.1、AC-02.2、AC-03.2、AC-04.5、AC-04.6、AC-06.1、AC-06.2、AC-06.3；依赖：T-01–T-05。
- [ ] **T-07 边缘服务内集成测试** — 使用隔离的 MQTT、device/event/media stub 与临时 SQLite/模型 fixture，覆盖身份和 ACL、Atlas 状态 payload 合同、配置乱序、两模型 JSON profile 隔离、按 model_id 激活/回滚互不影响、加载上限、模型/插件命名校验、确定性 YOLO 推理后处理、ACK 丢失/重复/永久拒绝、磁盘告警、媒体断连及服务重启；覆盖 REQ-01、REQ-02、REQ-03、REQ-04、REQ-05 / AC-01.1、AC-01.2、AC-01.3、AC-02.1、AC-02.2、AC-02.3、AC-02.4、AC-02.5、AC-03.1、AC-03.2、AC-03.3、AC-03.4、AC-03.5、AC-03.6、AC-03.7、AC-03.8、AC-03.9、AC-03.10、AC-04.1、AC-04.2、AC-04.3、AC-04.4、AC-04.5、AC-04.6、AC-05.1、AC-05.2、AC-05.3；依赖 T-01–T-06。
- [ ] **T-08 Atlas 云边链路实机验收与规格回写** — 在目标板记录激活/撤销、云端配置 ACK、断网缓存恢复、媒体接入和资源数据；模型推理性能/类别精度由课堂检测 T-07 验收；覆盖 REQ-01、REQ-02、REQ-04、REQ-05 / AC-01.1、AC-01.2、AC-01.3、AC-02.1、AC-02.2、AC-02.3、AC-04.1、AC-04.2、AC-04.3、AC-04.4、AC-04.5、AC-04.6、AC-05.1、AC-05.2、AC-05.3；依赖 T-07、真实设备和模型包。

## 验收记录

| AC | 检查方法 | 结果/证据 | 状态 |
|---|---|---|---|
| AC-01.3、AC-03.8 | Atlas ARM64 CMake build；`model_config_test` 校验 model_id/库名并拒绝错配；`model_plugin_identity_test` 动态加载插件核对 ABI 名称；runtime `--check-config`；JSON 引用和 OM SHA-256 交叉检查 | 完整源码副本 CMake 构建成功、3/3 CTest 通过；插件生成 `libmodel_classroom_behavior_detection.so` 并完成 OM AscendCL 初始化探针；runtime `--check-config` 成功；stream、告警规则、manifest 与 profile 使用同一 ID，告警默认关闭 | 通过 |
| AC-03.9 | 检查 media 头源路径与基名配对，并通过 Atlas ARM64 CMake build | Atlas 200I DK A2 完整源码副本构建成功；`include/media` 与 `src/media` 两对同基名文件通过结构检查，产品树不含 `common/media`；CTest 3/3 通过 | 通过 |
| AC-03.10 | 检查 common 公共头与实现同基名，并通过 Atlas ARM64 CMake build | `core.hpp/core.cpp`、`config.hpp/config.cpp` 在完整源码副本上构建成功；Atlas CTest 3/3 通过；源码无旧头文件名引用 | 通过 |
| AC-06.1、AC-06.2 | 最小环境 `ldd` 检查、systemd agent 与 CANN 环境文件权限/状态 | agent/runtime 的动态依赖均解析；`/etc/yunshao/cann.env` 为 `root:yunshao 0640`；agent 以 `yunshao` active 并持续 MQTT/TLS 心跳 | 通过 |
| AC-06.3 | 检查 udev 规则与节点权限；以 `yunshao` 服务身份完成 Atlas RGN/VPSS/VDEC/VENC 帧处理 | udev 将 `/dev/rgn` 应用为 `root:HwHiAiUser 0660`；systemd 两单元含所需硬件组；非 root runtime 合成 H.264 75/75 帧成功 | 通过 |
| AC-02.5、AC-03.1、AC-03.2、AC-05.1 | Atlas agent → TLS MQTT → device-service 心跳；Atlas C++ runtime/OM/OSD/VENC 合成视频 smoke 与 10 分钟实时流持续测试 | 云端 `last_seen_at` 更新且实报 `mqtt_state=connected`、模型 `installed_not_loaded`、流 `not_configured`；短 smoke 75/75 帧通过；实时合成流输出 8997/9000 帧，运行 694 秒，采样 RSS 峰值 459.27 MiB、NPU 峰值 3017/3513 MiB，详见[短 smoke](../../../preparation/model_classroom_behavior/model_val/reports/release_20261020/atlas_runtime_smoke.json)和[持续测试报告](../../../preparation/model_classroom_behavior/model_val/reports/release_20261020/atlas_runtime_sustained.json) | 部分通过：真实课堂 RTSP/重连/发布及内存稳定性仍待验证 |
| AC-01.1–05.3 | 自动化边缘集成测试 + Atlas 实机配置/模型/视频/MQTT 联调 | Atlas 完整源码 CMake build 成功、CTest 3/3；板上 OM 通过 AscendCL 插件初始化/加载探针；TLS/MQTT 心跳已到达云端，合成视频 75/75 帧通过；激活/配置 ACK、真实 RTSP 与断网恢复仍待执行 | 部分执行 |

## 实施偏差与决定

| 日期 | 偏差/新发现 | 规格/设计更新 | 决定人或依据 |
|---|---|---|---|
| 2026-10-18 | 板端没有 Go，未发现 libmosquitto pkg-config；SQLite/FFmpeg/OpenCV/CANN 已安装 | 固定 C++ 双进程候选；MQTT 依赖加入 T-01 核验 | Atlas 环境只读检查 |
| 2026-10-19 | 产品边缘目录已有 agent/model catalog，但没有课堂 AscendCL 推理运行时代码；原代码仍只在 preparation Demo | T-03 补多模型 profiles/按 ID 激活，T-07 增加多模型隔离与推理测试 | 用户评审指出正式工程缺少 AI 推理代码和多模型 JSON |

## 完成摘要

- agent、model catalog、SQLite outbox、classroom-runtime/plugin/JSON 与媒体模块均已在正式边缘工程；完整源码副本在 Atlas 200I DK A2/310B4 构建成功，CTest 3/3 通过。agent 的 systemd/CANN/MQTT 与云端心跳已验收；非 root runtime 完成 10 分钟实时合成流处理，真实授权课堂视频、RTSP/发布重连、长于本次的内存稳定性和断网恢复尚未验收。
- YOLO11n/640 OM `e91d…9564` 与 manifest SHA 一致，已安装到 `/var/lib/yunshao/models`；非 root runtime 完成 75 帧短 smoke 和 10 分钟实时合成流推理/OSD/H.264 编码。实时流输出 8997/9000 帧；采样 RSS 峰值 459.27 MiB，NPU 峰值 3017/3513 MiB，详见持续测试报告。当前未对该 OM 声称课堂精度/ONNX-OM 等价，完整精度测试集不是当前运行时集成前置条件。
