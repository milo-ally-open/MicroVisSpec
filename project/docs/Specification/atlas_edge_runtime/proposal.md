# 提案：Atlas 200I DK A2 边缘运行时与云边代理

> 状态：可实现；真实模型产物待提供、端云实机验收待执行。<br>
> 路径：跨边界/高风险。<br>
> 负责人：边缘端。<br>
> 上游：[PRD](../../Analysis/PRD.md) §4–§7、[总 SPEC](../../Analysis/SPEC.md) §2–§3、[系统结构](../../Design/STRUCTURE.md) §3–§5、[教室检测规格](../classroom_behavior_detection/spec.md)。

## 背景与问题

设备侧目录 `project/src/device_sides/atlas200idka2/` 只有占位；现有课堂 C++/AscendCL Demo 位于 `project/preparation/model_classroom_behavior/model_deployment/`，其 CLI 处理视频并输出检测叠加，但没有常驻服务生命周期、云端身份、配置同步或事件断网重传。首发需将 Atlas 200I DK A2 接入云端 device/event/media/push 服务，同时保持用户控制告警类别、初始全部关闭。

## 目标与成功观察

- 设备可安全激活并取得独立可撤销的 MQTT 身份；agent 原样使用激活响应的固定 client_id；心跳能区分设备、视频、NPU、模型、云连接状态。
- 云端配置以版本方式下发，边缘回报期望版本是否实际生效；配置初始关闭全部课堂规则。
- 命中已启用课堂规则时，边缘可靠缓存并以稳定 ID 上报；收到 event-service 应用级 ACK 后才清理本地待传项。
- 设备重启和断网后恢复配置、模型与未确认事件，不静默丢失或伪造成功。
- 模型以校验过的 OM 版本本地入库和激活；模型缺失/失败时服务可诊断但 readiness 不通过。

## 范围

### 本次包含

- Atlas 设备激活、独立凭据、心跳、命令/配置版本 ACK、健康状态和安全日志。
- 本地模型目录、清单、哈希校验、暂存安装、活动版本切换与回滚；模型行为及课堂规则详见课堂检测组。
- 将 AscendCL classroom-runtime、YOLO 后处理与 Atlas 视频/OSD 处理代码并入 `src/device_sides/atlas200idka2/` 正式边缘工程；从 `preparation/` 迁入后按模块组织，准备目录只保留实验/转换和验收工具。
- 参考 [device_side_ax650](https://gitee.com/milo-ally-intern/manhole-cover-detection/tree/master/model_deployment/device_side_ax650) 的插件式 AI 接口、manager 分层、按视频流配置模型和独立告警规则组织边缘应用；硬件后端使用 Atlas/CANN/AscendCL，不复用 AX650 的芯片 API。
- 多模型 `config/models.json` profiles、`config/streams_config.json` 流绑定及 `config/alarm_rules.json` 规则配置；按 model_id 隔离模型版本及推理参数。初始只配置已定首发的课堂 YOLO11n，所有告警规则关闭。
- agent 与本机 AscendCL 推理进程通过受限 IPC 通信；两个设备进程均由 systemd 管理，支持独立重启与状态检查。
- MQTT QoS 1 事件传输、应用 ACK、磁盘持久队列、重连补传和证据 HTTPS 上传。
- 授权 H.264 教室输入及算法画面接入云端实时视频链路；具体推流协议须按 Atlas 实测确认。

### 本次不包含 / 保持不变

- 消防通道首发支持、MCP、SSH 业务消息和跨场景算法编排。
- 边缘设备自行决定账号归属；`account_id` 必须来自云端设备登记。
- 未启用规则的普通检测上报为云端事件或保存为历史检测数据。

## 现状依据与约束

- Atlas 200I DK A2、Ascend 310B4、Ubuntu 22.04 aarch64、CANN 7.0.RC1；板上已有 CMake 3.22.1、GCC 11.3、OpenCV 4.5.4、FFmpeg/libavformat 58.76.100、SQLite 3.37.2；没有 Go。
- 当前板端检查未发现 `libmosquitto` pkg-config 项；边缘运行时需将兼容的 MQTT 客户端依赖作为明确构建/部署依赖验证，不得假设已存在。
- 模型流程由用户选为 [YOLO11n/640 WORKFLOW](../../../preparation/model_classroom_behavior/WORKFLOW.md)；权重已在用户提供目录，尚需整理入项目并完成 CANN/Atlas 转换和运行验收。
- 模拟 ACK、mock NPU 或桌面编译不能当作设备验收。

## 方案选项

| 方案 | 收益 | 成本/风险 |
|---|---|---|
| 在板端部署 Go 代理并调用 C++ runtime | 控制面协议实现选择丰富 | Atlas 未安装 Go，增加交叉构建、升级和双运行栈依赖 |
| C++ agent 与独立 C++/AscendCL 推理进程 | 复用现有 C++/NPU 运行环境、systemd 可独立恢复进程 | 需提供/验证 aarch64 MQTT 与 TLS 客户端；需明确 IPC 合同 |

建议采用 C++ 代理与 C++ 推理进程；经板端构建验证后固定依赖及服务单元。采用本机 SQLite 持久队列，目录/服务权限限定为设备服务账号。

## 影响面、依赖与风险

- device-service 负责账号归属、激活、配置期望态和 MQTT 身份；agent 负责配置回报与本地执行；event-service 负责事件持久化 ACK；media-service 管播放授权；push-service 只接收云端事件 outbox。
- 断网缓存需有磁盘上限、积压观测和恢复重试；证据可能晚于事件，继续遵守 event-service 现有证据状态合同。
- MQTT、HTTPS 与实时视频要求内网连通和可诊断错误；凭据轮换、模型更新、数据库迁移须有回滚路径。
- 模型权重缺失阻塞真实推理、精度和性能验收，不阻塞先行完成服务框架与配置协议。
- 多模型配置的 profiles 数量不等同于硬件可并发加载数量；默认 NPU 同时加载上限为 1，需使用目标 OM 实测内存与时延后才能提高。

## 未决问题

| 问题 | 影响 | 负责人/验证方式 | 状态 |
|---|---|---|---|
| Atlas 目标目录中的 MQTT TLS 客户端包及 systemd 部署脚本 | 影响板端构建与安全连接 | 构建时固定 aarch64 依赖，板端安装后实测 | 待验证 |
| RTSP 输入到 MediaMTX 预览的输出方式和帧率预算 | 影响端到端实时预览及 12 路容量 | 有授权源的 Atlas 实机原型/性能记录 | 待验证 |
| YOLO11n 权重及 CANN ATC OM 文件 | 影响 NPU 装载与模型验收 | 模型提供方后续交付，固定哈希 | 待提供 |

## 变更记录

| 日期 | 内容 | 决定/依据 |
|---|---|---|
| 2026-10-18 | 建立 Atlas 边缘运行时与可靠云边接入提案 | 用户要求；首发场景决策见课堂行为规格 |
| 2026-10-19 | 将真实课堂推理实现归入 Atlas 产品工程，并补多模型 JSON profile 与硬件加载上限合同 | 用户指出正式边缘工程缺推理代码和多模型配置 |
| 2026-10-19 | 按 AX650 示例采用插件 + manager + 逐流/模型/告警 JSON 分层；芯片后端适配 AscendCL | 用户指定参考仓库 |
