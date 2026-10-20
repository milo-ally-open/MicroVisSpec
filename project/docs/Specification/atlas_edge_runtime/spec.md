# Atlas 200I DK A2 边缘运行时与云边代理行为规格

> 关联提案：[proposal.md](proposal.md)；课堂推理与规则语义：[classroom_behavior_detection](../classroom_behavior_detection/spec.md)；云端合同：[总 SPEC](../../Analysis/SPEC.md)。

## 范围和术语

边缘设备为 Atlas 200I DK A2/Ascend 310B4。**agent**负责云边身份、状态、配置与事件队列；**期望配置**是云端发出的带单调版本号完整配置；**实报配置**为 agent 从运行中的推理服务读取并确认的版本。**应用级 ACK**表示 event-service 已可靠接收事件，MQTT QoS PUBACK 不等于应用 ACK。课堂行为只有规则命中才成为业务事件，规则初始全部关闭。

## 需求与验收条件

### REQ-01：设备身份与安全激活

**需求**：设备完成一次性激活后使用独立设备身份连接云端；设备凭据、主题权限和归属由 device-service 建立及撤销。

**验收条件**

- **AC-01.1** Given 有效且未使用的激活凭据，When 设备激活，Then 云端返回与唯一 device_id 绑定的独立凭据并使激活凭据不可重用。
- **AC-01.2** Given 凭据无效、已撤销或主题 device_id 与凭据身份不符，When MQTT/HTTPS 请求到达，Then 拒绝访问并记录无敏感值的诊断信息。
- **AC-01.3** Given 任意边缘消息体，When 云端确定账号归属，Then 只使用 device-service 的登记关系，不采信设备自报 `account_id`。
- **AC-01.4** Given 激活响应包含 MQTT `client_id`、username、password 和 TLS broker 信息，When agent 建立 MQTT 连接，Then 必须逐项使用激活返回的 `client_id` 与设备凭据；client_id 缺失时失败关闭，不按 device_id 自行派生。

**验证方式**：设备激活与主题 ACL 集成测试、凭据撤销测试。

### REQ-02：心跳、配置版本和算法状态

**需求**：边缘周期报告设备、相机、模型/NPU、视频链路、云连接及配置状态；服务必须区分在线状态与算法/媒体就绪状态。

**验收条件**

- **AC-02.1** Given agent 正常运行但模型缺失/相机离线/NPU 加载失败，When 心跳上报，Then 设备连接状态可为在线，但对应模型/相机状态为未就绪或失败。
- **AC-02.2** Given 云端下发配置版本 N，When 推理服务成功应用，Then agent 仅在读取确认 N 后回报 `applied_version=N`；失败则保留前一实报版本并返回错误码。
- **AC-02.3** Given 重复、过期或乱序配置，When 收到配置命令，Then 重复版本幂等确认，旧版本不覆盖新版本，格式错误配置不替换有效配置。
- **AC-02.4** Given 多模型 profiles 与多个摄像头/算法绑定，When runtime 上报状态，Then 每个绑定独立报告 model_id、model_version、加载状态和错误码；一个模型加载失败不覆盖其他模型的诊断状态。
- **AC-02.5** Given agent 向 `devices/{deviceId}/state` 发布状态，When 序列化心跳，Then 报文包含 schema_version/device_id/sent_at 与设备、摄像头、NPU、模型目录、流、MQTT、配置版本及 outbox 状态字段，符合 device-service 状态合同；服务端持久化并刷新 last_seen。

**验证方式**：配置版本状态机集成测试、心跳/API/MQTT 端到端模拟。

### REQ-03：多模型配置、模型包入库、激活与回滚

**需求**：`project/src/device_sides/atlas200idka2/` 必须包含可构建的 C++ AscendCL 推理运行时代码，不得只保留模型入库代理而把唯一推理实现留在 `preparation/` Demo。工程采用插件式通用 AI 接口、manager 调度和硬件媒体 pipeline：运行时按 profile 装载 Atlas AI 插件，流管理器按 stream 配置绑定 model_id，告警过滤器独立消费检测结果。配置拆分为 `config/models.json`（多模型 profiles 与本机支持能力）、`config/streams_config.json`（流到 model_id/阈值绑定）和 `config/alarm_rules.json`（类别到事件类型/等级的告警规则）。每个模型 profile 定义唯一 `model_id`、算法键、模型文件格式/名称、目标 SoC、插件/后端/模型族/解码器、输入尺寸/像素格式/预处理、有序类别、置信度/NMS/最大检测数及启用状态。profile 的模型类别、芯片、尺寸、格式、插件和规则映射须从配置与模型清单读取；通用 catalog/配置解析不得固化 classroom class、类别数量或课堂 event type。配置可登记多个模型，未知支持能力、重复 ID、非法尺寸/阈值和与模型清单不一致的类别映射均须拒绝。模型包清单是哈希、版本和模型文件的事实来源，并须与所选 profile 一致。模型按 `models/{model_id}/{version}/` 入库、按 `active/{model_id}` 独立激活；安装/回滚一个模型不得覆盖其他模型。推理进程按配置选用 model_id，并按 `max_loaded_models` 限额运行；默认值为 1，只有实机内存/时延验证后才提高。模型 ID、版本、格式、类别映射、输入尺寸和哈希须对云端可观测。

模型及插件统一采用 `model_` 前缀、以 `_` 分隔的小写英文领域段，并以任务类型结尾；末尾任务段只能是 `detection`、`segmentation` 或 `classification`。例如课堂行为检测的 `model_id` 为 `model_classroom_behavior_detection`，插件源文件为 `plugins/model_classroom_behavior_detection.cpp`，共享库为 `libmodel_classroom_behavior_detection.so`，实现类为 `ModelClassroomBehaviorDetection`。插件类实现 `IAIModel`，工厂导出符号保持 ABI 合同；插件 `name()` 与其 profile 的 `model_id` 相同。`algorithm_key` 是云端业务算法键，不受此命名约束，课堂场景继续使用 `classroom_behavior_detection`；`model_family`（例如 `yolo11`）描述模型/输出族，不用作模型 ID 或插件名。新模型的源文件、库、类、profile、清单和引用该模型的流/规则必须使用同一模型标识。

Atlas 媒体公共接口放在 `include/media/`，实现放在 `src/media/`；每个 `.hpp` 都有同基名的 `.cpp`（如 `media_pipeline.hpp` / `media_pipeline.cpp`），不再使用 `common/media/`。

边缘端 `include/common/` 中的公共头文件也必须与 `src/common/` 实现文件按基名配对：`core.hpp` ↔ `core.cpp`、`config.hpp` ↔ `config.cpp`。产品头源文件不得因历史命名保留不同基名。

**验收条件**

- **AC-03.1** Given YOLO11n/640、Ascend 310B4 OM 模型包和有效清单，When 安装，Then 先在临时目录校验格式、类别顺序、目标芯片及 SHA-256，再原子切换活动版本。
- **AC-03.2** Given 哈希不匹配、模型不兼容或 NPU 加载失败，When 安装/启动，Then 不切换为活动版本，旧版本保持可用，设备报告 `model_load_failed` 或等价错误。
- **AC-03.3** Given 磁盘模型文件与云端清单哈希不同，When 心跳/模型状态上报，Then 云端显示不一致并禁止将算法状态报告为 ready。
- **AC-03.4** Given `models.json` 含至少两个合法 profile 且流配置绑定其中一个 model_id，When manager 构建指定 stream 的推理 pipeline，Then 插件、输入/解码/阈值/类别配置仅来自该模型 profile；profile 所选能力不在该配置声明的支持集合、ID 重复、字段非法或类别/格式/SoC/尺寸与活动清单不一致时拒绝应用，并保留前一配置。
- **AC-03.5** Given 两个 model_id 分别安装不同版本，When 单独激活或回滚其中一个 ID，Then 仅对应 `active/{model_id}` 变化，其他模型活动版本及运行状态不变；加载数量不得超过 `max_loaded_models`，默认限制为 1。
- **AC-03.6** Given 新设备首次启动且未收到云端告警规则，When 运行本地 `alarm_rules.json`，Then 所有规则均为 disabled；推理仍可输出实时检测用于 OSD，但不会创建事件或证据。
- **AC-03.7** Given 配置文件为某模型声明了类别、芯片、输入尺寸、后端、解码器和告警事件映射，When 通用配置解析与模型 catalog 校验，Then 这些值均从 profile/清单/规则读取；将 classroom 类别替换为另一组有效类别时，公共配置与 catalog 仍按新配置校验，不依赖课堂类别常量。
- **AC-03.8** Given 新增 detection、segmentation 或 classification 模型，When 注册 profile 并构建插件，Then `model_id`、插件源文件/共享库、实现类及对应流/规则/清单遵循 `model_` + 领域名 + 任务类型的命名并保持一致；`IAIModel::name()` 等于 profile 的 `model_id`，`algorithm_key` 与 `model_family` 各自保留业务语义和模型族语义。
- **AC-03.9** Given Atlas 媒体模块参与构建，When 检查产品源码布局，Then 媒体头文件位于 `include/media/`、实现位于 `src/media/`，每对 `.hpp`/`.cpp` 的基名完全一致，且不存在 `common/media/` 实现目录。
- **AC-03.10** Given Atlas 公共核心/配置模块参与构建，When 检查头源文件布局，Then `include/common/core.hpp` 与 `src/common/core.cpp`、`include/common/config.hpp` 与 `src/common/config.cpp` 同名配对，且不再引用 `edge_core.hpp` 或 `model_config.hpp`。

**验证方式**：多模型 JSON 解析/负向测试、使用非课堂类别的泛化 fixture、模型清单校验器单元/集成测试、隔离 model_id 的激活/回滚测试、损坏/错芯片模型测试、Atlas 实机加载证据。

### REQ-04：事件持久缓存、上报与确认

**需求**：边缘仅缓存 classroom 规则命中的事件；MQTT 断线、进程重启或 ACK 丢失时，使用原 `event_id` 重发并在应用 ACK 后才标记完成。

**验收条件**

- **AC-04.1** Given 启用规则命中并生成事件，When 写入本地持久队列，Then 队列项包含稳定 ID、事件类型、模型/规则版本、创建时间及重试状态。
- **AC-04.2** Given MQTT QoS PUBACK 但没有 event-service 应用 ACK，When 服务重启/网络中断，Then 事件仍待传且重试不更换 ID。
- **AC-04.3** Given event-service 返回 `accepted`/`duplicate` 且应用 ACK 与设备/事件 ID 匹配，When agent 处理回执，Then 对应队列项方可完成；重复 ACK 幂等，错设备/错 ID ACK 不清除数据。
- **AC-04.4** Given event-service 返回带稳定 reason_code 的永久 `rejected`，When agent 处理回执，Then 项目移入本地 quarantine 并报告告警，不无限重发、不静默删除。
- **AC-04.5** Given 云端依赖暂时不可用或没有应用 ACK，When 重启/网络恢复，Then 事件仍待传且以相同 ID 退避重试。
- **AC-04.6** Given 云端或本地队列长期故障、磁盘接近上限，When 无法继续可靠缓存，Then 报告告警状态并保留未确认记录，不返回伪造成功。

**验证方式**：SQLite 持久化与进程重启测试、MQTT 断网/ACK 丢失/重复场景集成测试。

### REQ-05：授权视频与证据上传

**需求**：边缘接入已授权 H.264 教室源，输出供 MediaMTX/前端播放的处理后视频；只有已启用规则触发时才生成并上传截图。

**验收条件**

- **AC-05.1** Given 可用输入流，When runtime 处理视频，Then 按该 stream 配置的输出尺寸、帧率和目标码率生成检测叠加预览，并通过无需 RTSP 用户名/密码的 Atlas RTSP server 提供园区内可达 RTSP 源；media-service 以单一 `rtsp_url` 将该源登记为 MediaMTX `sourceOnDemand` 路径，浏览器只使用鉴权后的 WHEP/HLS 地址且不获得来源 URL。
- **AC-05.2** Given live 输入流断开/读超时、RTSP server 启动失败或媒体客户端断开，When runtime 检测到故障，Then 以不含凭据的独立来源/发布错误状态和日志报告；live 输入读超时必须有界，RTSP 客户端断开不得阻塞推理或编码，慢客户端最多产生配置时限内的有界排队，超限时可丢弃预览帧；服务故障须由 systemd 恢复。无故障的本地文件 EOF 可正常结束。
- **AC-05.3** Given 未启用规则的普通检测，When 运行模型，Then 不截图、不调用证据上传；命中规则的图片使用设备授权 HTTPS 接口并受 event-service 的大小/格式/24 小时合同约束。
- **AC-05.4** Given MediaMTX 或园区内受信任客户端连接配置的 RTSP mount，When 未提供 RTSP Authorization 头时拉取媒体，Then Atlas RTSP server 可直接提供 H.264；runtime 配置、环境文件和 systemd unit 不包含 RTSP 用户名/密码引用。Atlas RTSP 端口只部署在可路由且受控的园区网络，不向公网开放。

**验证方式**：匿名 Atlas RTSP 拉流、MediaMTX `sourceOnDemand` 拉流和断开测试、真实相机视频/证据测试；固定配置的输出尺寸/帧率/码率、断流超时与重连测试；编码、推流和长时资源预算记录。

RTSP 模式默认不保存本地 H.264 连续文件；仅显式传入诊断输出路径时才落盘，防止常驻流持续占满设备存储。

### REQ-06：Atlas CANN 运行环境与 systemd 启动

**需求**：边缘服务必须在 systemd 的干净环境中加载 Atlas CANN/驱动动态库和算子目录；不能依赖交互式 shell、Conda 或手工导出的环境变量。

**验收条件**

- **AC-06.1** Given systemd 启动 agent 和 classroom-runtime，When 两服务加载 CANN，Then 两个单元均读取由运维部署的 `/etc/yunshao/cann.env`；该文件提供与板上 CANN 安装匹配的运行库/算子路径，缺失时 agent/runtime 不得以不完整环境假装就绪。
- **AC-06.2** Given 服务以非 root 的 `yunshao` 账号读取 CANN 环境文件，When systemd 启动服务，Then 配置文件权限仅允许 root 与 `yunshao` 服务账号读取；干净登录环境下动态依赖可解析，AscendCL 初始化结果进入服务状态/日志。
- **AC-06.3** Given runtime 使用 Atlas DVPP/VPSS/VENC/OSD 设备节点，When udev 创建设备节点且 systemd 启动服务，Then 仅将所需节点（包括 `/dev/rgn`）授权给 `HwHiAiUser` 设备组，service 单元显式加入 `HwHiAiUser`/`HwBaseUser`/`video`；不将设备节点设为 world-writable，非 root runtime 可创建 Region 并完成媒体帧处理。
- **AC-06.4** Given Atlas CANN/ACL 在 systemd 下初始化，When 服务单元应用文件系统隔离选项，Then 仅启用已在目标板验证兼容 CANN/驱动 IPC 的选项；未经实机验证不得启用 `ProtectSystem`、`ProtectHome`、`PrivateTmp` 或 `ReadWritePaths`，并保留非 root 用户、`NoNewPrivileges` 和最小硬件设备组约束。

**验证方式**：使用最小 `PATH` 的环境检查 agent/runtime 依赖；通过 systemd 启停两项服务并验证 MQTT 连接及 AscendCL 初始化。runtime 的 systemd 验证须处理固定视频并检查退出/帧数；任何新增隔离选项须在 Atlas 实机完成相同验证后才可纳入正式 unit。

## 不变项与兼容要求

- 设备账号归属由 device-service 管理；设备 ID 与边缘事件 ID 稳定且跨重试不变。
- MQTT 消息版本未知时拒绝/隔离；事件仍遵守全局 schema_version、QoS 1、主题 ACL、应用 ACK 和时间合同。
- 三类课堂告警规则默认 disabled；行为语义详见课堂检测规格。
- 推理运行时不得依赖板上 Go；构建必须记录 CANN、编译器及依赖版本。
- 设备退网、凭据撤销或回滚不能删除已持久化但未确认的事件。

## 数据处理和保留

- 模型清单和版本目录保存在设备本地；配置快照按版本存档，密钥文件限服务账号读取。
- 未命中规则的帧和检测结果只在实时处理内存中使用，不写本地队列或上传云端。
- 未确认事件在本地持久保存至应用 ACK；磁盘配额和清理仅可作用于已确认项。
- 证据不进入 MQTT 负载，云端保存规则触发截图 24 小时。

## 明确不做

- 消防通道算法、SSH 业务通道、边缘自行管理云账号权限。
- 普通检测云端历史、学生身份跟踪或自动启用告警规则。
- 模型产物交付前宣称 Atlas 推理验收通过。

## 需求追踪

| 上游条款 | REQ | 模块/服务 | 任务 ID | AC | 验收证据/状态 |
|---|---|---|---|---|---|
| 总 SPEC §2：设备身份与配置版本 | REQ-01、REQ-02 | device-service、agent | T-01/T-02/T-06/T-07/T-08 | AC-01.1、AC-01.2、AC-01.3、AC-02.1、AC-02.2、AC-02.3、AC-02.5 | Atlas TLS/MQTT 持续连接；device-service 已接受 agent 心跳并更新 `last_seen_at`，配置 ACK 仍待实测 |
| 总 SPEC §3：事件幂等与 ACK | REQ-04 | agent、event-service | T-04/T-07/T-08 | AC-04.1、AC-04.2、AC-04.3、AC-04.4、AC-04.5、AC-04.6 | 待执行 |
| PRD §4：Atlas、教室视频与模型 | REQ-03、REQ-05 | Atlas plugin/manager runtime、model catalog、media-service | T-03/T-05/T-06/T-07/T-08 | AC-03.1–03.10、AC-05.1、AC-05.2、AC-05.3、AC-05.4 | 完整工程覆盖部署并在板端 CMake build/CTest 3/3 通过；OM SHA 与清单一致；非 root systemd runtime 对合成 H.264 完成 75/75 帧推理/OSD/编码；新匿名 RTSP 合同待实机验收 |
| 运维部署：systemd 运行时依赖 | REQ-06 | Atlas agent、classroom-runtime、systemd、udev | T-01/T-06/T-08 | AC-06.1、AC-06.2、AC-06.3、AC-06.4 | 最小环境依赖解析、权限受限 `/dev/rgn`、CANN 环境文件通过；agent active/enabled 且云端心跳新鲜；正式 runtime unit 保留非 root、硬件组和 `NoNewPrivileges`，未使用未经验证的文件系统隔离选项，合成 H.264 75/75 帧通过 |

## 假设与未决问题

- `[待核验]` MQTT aarch64 TLS 客户端依赖及打包方法；板端当前未发现 libmosquitto 开发包。
- `[待核验]` RTSP 输入、处理后视频发布路径和实际帧率/内存预算。
- `[待验证]` 已有 YOLO11n/640 PT、ONNX 和 Ascend 310B4 OM 文件；当前 OM SHA-256 为 `e91d9f26cf5342228fd33de18c894686d91c6e1d385634e76abc381dd5ce9564`。需完成模型清单入库及 Atlas 实机加载/运行指标确认。用户当前要求直接使用现有模型，不以完整测试集对比作为代码实现前置条件。

## 变更记录

| 日期 | 变更 |
|---|---|
| 2026-10-18 | 定义 Atlas 边缘代理、模型生命周期、状态配置、断网事件恢复和媒体接入合同。 |
| 2026-10-19 | 明确正式 Atlas 源码树须包含 AscendCL 推理运行时代码；增加多模型 JSON profiles、按 model_id 隔离活动版本和资源上限合同。 | 用户指出推理实现未并入边缘工程，且多模型配置缺失 |
| 2026-10-19 | 明确采用插件接口、manager/pipeline、逐流模型绑定和独立告警 JSON 的组织方式，适配 Atlas AscendCL。 | 用户指定参考 `device_side_ax650` 仓库；仅借鉴架构，不复用 AX 芯片 API |
| 2026-10-19 | 将产品进程、源码目录、环境变量、IPC socket 和 systemd 单元的名称统一为 `agent`。 | 用户要求 `edge_agent` 全量替换为 `agent` |
| 2026-10-19 | 统一模型插件、模型 ID 和任务类型的命名；课堂模型标识为 `model_classroom_behavior_detection`，算法键继续为 `classroom_behavior_detection`。 | 用户要求插件遵循 detection/segmentation/classification 命名规范 |
| 2026-10-19 | 媒体接口与实现目录统一为 `include/media/` 和 `src/media/`，头源文件按相同基名配对。 | 用户要求将 Atlas `common/media/` 并入 include 且不增加 common 层 |
| 2026-10-19 | 将 Atlas `include/common/` 头文件改为与 `src/common/` 实现同基名：`core.hpp`/`core.cpp`、`config.hpp`/`config.cpp`。 | 用户指出公共头文件遵循头源文件基名一致规范 |
| 2026-10-20 | 要求 agent 与 classroom-runtime 显式读取受限权限的 CANN 运行环境文件，避免 systemd 无法解析 AscendCL 动态库或依赖交互式 shell 环境。 | Atlas 实机 systemd 启动以 127 失败，干净环境检查发现 AscendCL/媒体动态库缺失 |
