# device-service 设计索引

> 本文件汇总服务边界与设计方向，不复制详细合同；具体行为以[服务规格](spec.md)为准。

## 责任边界

管理账号拥有的设备、摄像头、算法能力、期望配置和运行实报。

事件处置、媒体播放授权和其他服务数据库读写。

## 设计方向

device schema 由本服务独占；激活票据一次性；MQTT ACL 按设备隔离；期望配置与实报分开。

## 决策记录

已明确的方向以服务规格为准。未记录的备选方案不推定为已评审结论；影响接口、数据所有权或部署方式的变化，应记录决定、依据和影响并同步规格。

### MQTT 服务连接地址与设备下发地址分离（2026-10-20）

- **决定**：`MQTT_BROKER_URL` 仅供 device-service 的容器内 MQTT 管理/消息连接；新增必需配置 `MQTT_ADVERTISED_BROKER_URL`，只用于 `/edge/v1/activate` 的 `mqtt.broker_url`。
- **依据**：容器内部 DNS（例如 `mosquitto`）仅在 Compose 网络内解析，Atlas 不可达；复用同一配置会使设备激活成功但后续 MQTT 连接失败。
- **安全与运维**：两个 URL 都必须为 `ssl://`。部署需保证 advertised host/port 路由到 broker TLS listener，并向设备提供信任 CA；缺配置或非法时服务拒绝启动，不回退到内部 URL。
- **影响**：新增环境变量及启动校验；激活 API JSON 字段不变；既有 ACL、TLS 和 topic 权限不变。回滚时需同时回滚服务镜像和环境变量合同。

### Atlas 顶层运行状态心跳（2026-10-20）

- **决定**：device-service 的 `devices/{deviceId}/state` 解码器接受 Atlas agent 上报的设备/摄像头/NPU/模型/流/MQTT、配置版本和 outbox 状态字段，并保留已有 camera/algorithm 数组语义。
- **依据**：实机 MQTT/TLS 连接已成功，但心跳被严格 JSON 解码以 `applied_config_version` 未知字段拒绝，导致有效设备长期没有 `last_seen_at`。
- **安全与数据处理**：所有新增字段具有明确类型和非负范围校验；未知字段仍拒绝，payload 中 `account_id` 仍不可信；仅完整校验后的报告更新 `reported_state` 和心跳时间。
- **影响**：扩展状态 DTO 与服务内集成测试，不改变 MQTT 主题、身份 ACL 或设备在线超时规则。

## 状态依据

设计描述目标与约束，不代表已实现；状态由[任务与证据清单](tasks.md)、仓库代码和部署记录确认。
