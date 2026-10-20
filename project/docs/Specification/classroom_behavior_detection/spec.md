# 教室行为检测行为规格

> 关联提案：[proposal.md](proposal.md)；边缘运行时：[atlas_edge_runtime](../atlas_edge_runtime/spec.md)；云端事件：[event_service](../event_service/spec.md)。<br>
> 状态：运行时/插件/配置已进入 Atlas 产品树；PT、ONNX、OM 已在 preparation，OM 已安装到 Atlas 并完成合成视频推理 smoke；独立模型部署 demo 源码与复现文档已补齐，Atlas 上重新构建待验收；真实课堂流及精度评估仍待验收。<br>
> 来源：用户决定与仓库现有模型流程。

## 范围和术语

首发使用 Atlas 200I DK A2（Ascend 310B4）上的 YOLO11n/640 模型，模型流程当前列出 `hand-raising`、`reading`、`writing` 三类。**检测**表示单帧/视频画面中模型输出的类别、边界框与置信度。**异常规则**是用户为行为类别启用的业务条件；只有规则命中才成为平台**事件/告警**。行为标签本身不意味着异常或危险。

该模型在设备端的标识为 `model_classroom_behavior_detection`，插件实现为 `ModelClassroomBehaviorDetection`。`algorithm_key` 仍为 `classroom_behavior_detection`，用于云端算法语义映射；`yolo11` 仅表示模型族。其他模型按边缘运行时 Spec 规定的 `model_` 前缀、领域名和任务后缀命名。

## 需求与验收条件

### REQ-01：课堂模型语义与运行状态

**需求**：课堂运行时消费由 [Atlas 边缘运行时](../atlas_edge_runtime/spec.md) 验证并激活的模型版本；对外报告活动模型 ID、版本、输入尺寸和有序类别映射。模型包清单、哈希、设备兼容、安装、激活及回滚只由 Atlas 边缘运行时规格定义。

**验收条件**

- **AC-01.1** Given 有活动课堂模型，When 查询课堂算法状态，Then 报告模型 ID、版本、输入尺寸 640，类别顺序为 `hand-raising`、`reading`、`writing`。
- **AC-01.2** Given 模型文件缺失或活动模型加载失败，When 查询课堂算法状态，Then 分别报告 `model_missing` 或 `model_load_failed`，算法不得显示 ready，且不得生成伪造检测结果。
- **AC-01.3** Given 注册课堂模型 profile，When 模型目录、插件或流/告警配置引用该模型，Then 全部使用 `model_classroom_behavior_detection`；插件实现类为 `ModelClassroomBehaviorDetection`，算法键保持 `classroom_behavior_detection`。

**验证方式**：类别映射与状态集成测试；模型包校验、激活和回滚证据见 Atlas 边缘运行时 REQ-03。

### REQ-02：课堂推理与展示

**需求**：模型运行时应由视频流 manager 按 stream 配置选择 AscendCL AI 插件，对授权 H.264 教室视频运行 YOLO11n/640 推理，并向受权实时预览输出可见类别、边界框和置信度；状态接口区分摄像头、解码、NPU、模型和云连接状态。

**验收条件**

- **AC-02.1** Given 有效模型与可解码视频，When 开始推理，Then 检测类别依据模型清单映射到 `hand-raising`、`reading`、`writing`，叠加预览不改变原始类别含义。
- **AC-02.2** Given 视频断开、NPU/模型不可用或输入编码不支持，When 运行时故障，Then 状态报告对应失败原因并可恢复重连；不得将设备在线等同于算法运行正常。
- **AC-02.3** Given `models.json` 注册多个模型 profile，When 摄像头配置选定一个 `model_id`，Then 推理输出、类别映射、置信度/NMS 参数和状态均来自该 profile 与它自己的活动清单；不能串用另一个模型的标签或阈值。并发加载数受 Atlas 运行时 Spec 定义的硬件上限约束。
- **AC-02.4** Given `streams_config.json` 的多个 stream 分别引用有效 model_id，When manager 构建处理链，Then 每路流的模型插件与阈值独立；缺失或禁用模型时该流明确未就绪，不影响其他流诊断。

**验证方式**：有授权固定样例视频的板端实测；模拟故障与恢复集成测试。

### REQ-03：告警规则可配置且默认关闭

**需求**：每个模型类别的告警规则都须显式启用，并配置置信度阈值、持续条件和事件等级；初始配置中所有类别禁用。未启用规则或未满足条件的检测不能生成云端业务事件、截图证据或推送意图。

**验收条件**

- **AC-03.1** Given 新设备/模型首次运行，When 没有用户规则配置，Then 三类规则均为 disabled，检测可用于实时叠加，但 MQTT 业务事件数为零。
- **AC-03.6** Given `alarm_rules.json` 存在 `model_classroom_behavior_detection` 的默认规则，When 首次载入，Then hand-raising、reading、writing 三条规则显式 disabled；文件缺失、损坏或规则不完整时安全拒绝加载并保持告警关闭。
- **AC-03.2** Given 一类规则启用且阈值、持续条件、等级完整，When 连续观测满足该规则，Then 只生成一个稳定 `event_id` 的事件；同一触发窗口内重复帧不产生重复事件。
- **AC-03.3** Given 规则禁用、置信度未达标、持续条件不满足或配置过期，When 检测到行为，Then 不创建事件；配置更新通过版本 ACK 后才生效。
- **AC-03.4** Given 配置请求缺少启用规则所需参数或使用不支持类别，When 保存，Then 返回校验错误并保留已生效配置。
- **AC-03.5** Given 边缘上报课堂事件，When event-service 调用 device-service 校验配置版本，Then 只有设备曾回报应用且事件时间/类别/置信度/持续条件/等级匹配的版本被接受；禁用或未知规则得到明确拒绝，不生成事件和 outbox。

**验证方式**：规则状态机单元测试、边缘模拟视频集成测试、设备服务配置 ACK 测试。

### REQ-04：课堂数据最小化

**需求**：检测/事件载荷只包含场景类别、置信度、必要的边界框/区域标识、规则和模型版本、事件时间及设备/摄像头引用；不识别/跟踪个人、不推断成绩或专注度。证据只在已启用规则触发时生成，并遵守上游 24 小时保留合同。

**验收条件**

- **AC-04.1** Given 边缘或云端结构化载荷，When 检查字段，Then 不包含学生姓名、面部特征、身份 ID、跨帧跟踪 ID、成绩或专注度字段。
- **AC-04.2** Given 规则命中产生的截图，When 上传并到期，Then 图片只经账号鉴权读取，上传成功 24 小时后返回过期状态；事件元数据按既有服务合同保留。

**验证方式**：载荷 schema 检查、访问控制集成测试、过期证据接口测试。

### REQ-05：模型部署验收 demo 与研发复现

**需求**：课堂模型研发目录必须保留可独立构建的 Atlas AscendCL 视频推理 demo，用于验证指定 OM、类别顺序、H.264 输入、推理后处理和 H.264 输出。该 demo 不负责模型包安装/激活、产品服务生命周期、云端事件或告警；这些行为由 Atlas 产品 runtime 与 agent 唯一负责。课堂、车辆和仓储模型工作流的训练、导出、转换、评估、部署阶段目录分别提供 README.md SOP，说明依赖、输入、命令、输出和已知限制；不要求在模型根目录增加总 README。

**验收条件**

- **AC-05.1** Given Ascend 310B4 兼容 OM、与模型类别顺序一致的 labels 文件和 H.264 文件/RTSP 输入，When 构建并运行 `model_deployment` demo，Then demo 校验模型输入/输出与类别数、逐帧运行检测及 OSD，并输出 H.264 和处理/推理统计；错误输入或输出形状不匹配时明确失败。
- **AC-05.2** Given 课堂模型开发或复现人员，When 按训练、导出、转换、评估、部署各阶段目录中的 README.md SOP 操作，Then 可定位固定数据集版本、模型参数、产物路径、转换命令、评估方法和设备要求；文档不含真实密钥/密码，也不把烟测描述成精度验收。
- **AC-05.3** Given 产品发布或服务运维，When 使用正式边缘服务，Then 生产推理仍由 `project/src/device_sides/atlas200idka2/` 内的 `classroom-runtime` 执行；研发 demo 不替代或重复模型 catalog、安装、激活、规则上报或 systemd 服务。

**验证方式**：源码布局与配置静态检查；Atlas 200I DK A2/CANN 实机 CMake 构建及有效模型 H.264 输入输出 smoke。每次结果须记录板卡、CANN、模型哈希和实际完成帧数；开发机文档检查不能代替板端构建证据。

## 不变项与兼容要求

- 全局事件采用通用 `event_type`、`event_id`、设备归属及版本化 MQTT 合同；教室类别不得固化进通用事件表/API 路径。
- 设备账号归属由云端设备登记决定；边缘不提交可信 `account_id`。
- 云端只对通过启用规则生成的事件创建通知 outbox；`push-service` 不直接读取边缘检测流。
- 模型包或配置升级失败可回滚；缓存事件使用原 `event_id` 重传。

## 数据处理和保留

- 模型包：边缘本地版本化文件、清单与哈希；模型运行需记录激活版本。
- 实时检测：画面叠加所需的短期内存结果，不持久化普通检测历史、不上传云端。
- 告警事件：仅规则命中后上传最小必要行为元数据；截图按 event-service 的 24 小时规则。

## 明确不做

- 消防通道占用检测作为本期首发场景。
- 学生身份识别、跟踪、成绩评价、专注度评价以及未授权教室数据采集。
- 默认开启任何课堂行为告警规则。

## 需求追踪

| 上游条款 | REQ | 模块/服务 | 任务 ID | AC | 验收证据/状态 |
|---|---|---|---|---|---|
| PRD §4：课堂模型和规则默认关闭 | REQ-01、REQ-02、REQ-03 | `src/device_sides/atlas200idka2/` agent/runtime、device-service | T-01/T-02/T-03/T-04/T-06/T-07 | AC-01.1、AC-01.2、AC-01.3、AC-02.1、AC-02.2、AC-02.3、AC-03.1、AC-03.2、AC-03.3、AC-03.4、AC-03.5 | 插件/配置/ABI、模型安装、Atlas 合成视频推理/OSD/编码和云端心跳已通过；精度/真实课堂流仍待验收 |
| PRD §5：最小化课堂数据 | REQ-04 | 边缘、event-service、media-service | T-05/T-06 | AC-04.1、AC-04.2 | 待执行 |
| 总 SPEC §2–§3：设备身份、MQTT 幂等、通知 outbox | REQ-03、REQ-04 | device/event/push services | T-03/T-04/T-05/T-06 | AC-03.1、AC-03.2、AC-03.3、AC-03.4、AC-03.5、AC-04.1、AC-04.2 | 待执行 |

## 假设与未决问题

- 用户选择 `WORKFLOW.md` 记录的 YOLO11n/640 流程；PT、ONNX 和 Ascend 310B4 OM 已在 preparation 入库并核对 OM SHA-256；OM 已在 Atlas 安装并完成合成视频实机推理。未要求完整测试集对比作为运行时集成前置条件；当前未对课堂精度或 ONNX/OM 精度等价作结论。
- `[待核验]` Atlas 上的实际摄像头编码、模型帧率与推流方案；以实机记录为准，不把桌面 Demo 结果当板端验收。

## 变更记录

| 日期 | 变更 |
|---|---|
| 2026-10-18 | 建立首发教室行为检测、禁用优先的告警规则及课堂数据边界合同。 |
| 2026-10-19 | 确认设备模型 ID、插件库及清单采用 `model_classroom_behavior_detection`；插件类为 `ModelClassroomBehaviorDetection`，云端算法键仍为 `classroom_behavior_detection`。 |
