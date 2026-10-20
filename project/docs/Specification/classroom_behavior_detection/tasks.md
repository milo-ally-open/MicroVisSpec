# 教室行为检测实施任务

> 关联合同：[spec.md](spec.md)；设计：[design.md](design.md)。T 编号仅在本规格组内唯一。

## 任务清单

- [ ] **T-01 活动模型语义接入** — classroom-runtime；读取 Atlas runtime 已激活模型的 ID、版本、输入尺寸、有序类别和加载状态，报告课堂侧模型状态并拒绝不匹配的类别语义；覆盖 REQ-01 / AC-01.1、AC-01.2、AC-01.3；依赖 Atlas 边缘运行时 T-03，不重复实现清单、哈希或激活逻辑。
- [ ] **T-02 AscendCL 插件与多流推理管理器** — `plugins/` + `src/manager/`；迁入并模块化正式 Demo 的 AscendCL YOLO 推理、类别映射、边框/置信度叠加与 Atlas 媒体通道；实现稳定 `IAIModel` 插件接口、严格校验 `config/models.json`、按 `streams_config.json` 的 model_id 选插件/阈值并按 Atlas 限额加载，分别报告摄像头/NPU/模型/云状态；插件文件、库、实现类和 JSON model_id 使用 `model_` 前缀、领域名和任务后缀，课堂身份为 `model_classroom_behavior_detection` / `ModelClassroomBehaviorDetection`；覆盖 REQ-02 / AC-02.1、AC-02.2、AC-02.3、AC-02.4；依赖本组 T-01 和 Atlas 边缘运行时 T-03。
- [ ] **T-03 规则配置解析与事件状态机** — agent/runtime；三类禁用默认、参数完整性、阈值/持续/冷却判定、版本 ACK、稳定 event_id、重复帧抑制；覆盖 REQ-03 / AC-03.1、AC-03.2、AC-03.3、AC-03.4、AC-03.6；依赖 T-02、device-service。
- [ ] **T-04 云端课堂事件准入校验** — device-service 历史规则版本校验 API + event-service 调用；核验 applied 时间、类别、阈值、持续时长和 severity；对明确永久拒绝返回 reason_code，对依赖故障保持重试；覆盖 REQ-03 / AC-03.5；依赖 T-03。
- [ ] **T-05 课堂数据最小化与证据边界** — 边缘消息 schema、日志、event-service 证据；检查不含身份/跟踪/成绩/专注度字段，普通检测不存储、不截图、不上传，规则命中截图鉴权和 24 小时到期；覆盖 REQ-04 / AC-04.1、AC-04.2；依赖 T-03/T-04。
- [ ] **T-06 课堂推理/多模型配置/规则自动化集成测试** — 确定性推理输出 fixture + 两个独立 profile 测试配置，覆盖 profile 解析与 model_id 隔离、插件身份命名、类别映射、缺失/失败模型状态、默认关闭、规则参数缺失/越界、未达阈值、持续命中只生成一个事件、规则版本乱序/失效、云端准入和事件负 ACK quarantine；模型包篡改/兼容/独立激活由 Atlas 边缘运行时测试覆盖；覆盖 REQ-01、REQ-02、REQ-03、REQ-04 / AC-01.1、AC-01.2、AC-01.3、AC-02.1、AC-02.2、AC-02.3、AC-03.1、AC-03.2、AC-03.3、AC-03.4、AC-03.5、AC-03.6、AC-04.1、AC-04.2；依赖 T-01–T-05 和 Atlas T-03/T-04。
- [ ] **T-07 Atlas 实机模型验收与规格回写** — 对已入库 YOLO11n/640 OM 记录 SHA-256、CANN 转换参数，并实测板端加载、内存、推理延迟/帧率/NPU、授权视频输入/输出和断网恢复；固定测试集精度对比不作为本阶段使用前置条件，按需另行运行；覆盖 REQ-01、REQ-02、REQ-03 / AC-01.1、AC-01.2、AC-02.1、AC-02.2、AC-03.1；依赖 T-06 与设备模型包。
- [ ] **T-08 恢复课堂模型部署验收 demo** — `model_deployment/src/` + CMake；恢复可在 Atlas 200I DK A2 上独立构建的 AscendCL/H.264 视频推理 demo，类别顺序取自 `classes.txt`；只负责模型准备验收，不复制正式 runtime 的模型 catalog/安装/激活/规则与服务生命周期；覆盖 REQ-05 / AC-05.1、AC-05.3；参考车辆/仓储模型部署工程。源码已恢复，板端重新构建验收仍待执行。
- [x] **T-09 补齐三套模型研发流程复现文档** — `model_classroom_behavior`、`model_vehicle_detection`、`model_warehouse_detection` 的 train/export/convert/val/deployment 阶段 README.md SOP，说明固定数据来源/版本、环境、命令、产物和验收边界；模型根目录不新增总 README；覆盖 REQ-05 / AC-05.2。

## 验收记录

| REQ/AC | 检查方法 | 当前证据 | 状态 |
|---|---|---|---|
| REQ-01/AC-01.3 | JSON model_id/manifest/插件库引用检查、Atlas 插件动态加载身份测试 | `model_config_test` 和 `model_plugin_identity_test` 通过；配置 dry-run 通过 | 通过 |
| REQ-01/AC-01.1–01.2 | 活动模型类别映射与缺失/加载失败状态集成测试 | OM v1.0.0 SHA 与 manifest 一致，板端 catalog 报告 `installed`；runtime 已加载该模型并完成合成视频推理 | 部分通过：无真实课堂视频/精度验证 |
| REQ-02/AC-02.1–02.3 | 多 profile 隔离、活动模型状态、授权固定视频推理与故障恢复测试 | Atlas 310B4 非 root C++ runtime 对 640×480 H.264 合成源完成 75/75 帧短 smoke；另以 15 FPS 本机 UDP 合成输入运行 694 秒，输出 8997/9000 帧、采样 RSS 峰值 459.27 MiB、NPU 峰值 3017/3513 MiB；见[短 smoke 报告](../../../preparation/model_classroom_behavior/model_val/reports/release_20261020/atlas_runtime_smoke.json)和[持续测试报告](../../../preparation/model_classroom_behavior/model_val/reports/release_20261020/atlas_runtime_sustained.json) | 部分通过：非真实 RTSP；3 帧缺失原因未定，需授权课堂视频与重连验收 |
| REQ-03/AC-03.1–03.5 | 规则状态机、设备 ACK 与服务端规则校验集成测试 | 实现未开始 | 未执行 |
| REQ-04/AC-04.1–04.2 | 消息 schema、日志字段和证据访问/保留测试 | 实现未开始 | 未执行 |
| REQ-05/AC-05.1、AC-05.3 | demo 源码、CMake、类别映射和职责边界检查；Atlas 板端构建与 H.264 smoke | C++/CMake/媒体源码已恢复；生产 runtime 仍在 Atlas 正式工程树；板端重新构建尚未执行 | 部分通过：待 Atlas 构建/运行 |
| REQ-05/AC-05.2 | 三套模型各阶段 README 检查；命令和产物路径与脚本核对 | train/export/convert/val/deployment 五个阶段 README.md 均已补齐；模型根总 README 不保留；未运行模型训练或转换 | 通过：文档结构与命令静态核对 |

## 完成摘要

- 边缘运行时代码已进入正式 Atlas 工程；插件命名、配置交叉引用、插件 ABI、agent/runtime 和三项边缘 CTest 已在 Atlas ARM64 构建验证。系统服务的 CANN、MQTT/TLS 和云端心跳已实机通过。
- 用户已确认初始规则全关闭，并选择 YOLO11n/640 工作流；PT/ONNX/OM 已入库。OM 已装入板端模型目录，非 root runtime 完成 10 分钟实时合成流推理/OSD/编码；真实课堂 RTSP、重连、精度评估仍待授权视频源/测试集。
