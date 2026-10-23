# device-service 设计索引

> 本文件汇总服务边界与设计方向，不复制详细合同；具体行为以[服务规格](spec.md)为准。

## 责任边界

管理账号拥有的设备、摄像头、算法能力、期望配置和运行实报。设备详情可聚合 media-service 的摄像头来源配置入口，但 device-service 不复制或存储 RTSP URL。

事件处置、媒体播放授权和其他服务数据库读写。

## 设计方向

device schema 由本服务独占；激活票据一次性；MQTT ACL 按设备隔离；期望配置与实报分开。

## 决策记录

已明确的方向以服务规格为准。未记录的备选方案不推定为已评审结论；影响接口、数据所有权或部署方式的变化，应记录决定、依据和影响并同步规格。

### 设备详情配置摄像头 RTSP 地址（2026-10-21）

- **决定**：设备详情页直接呈现摄像头 RTSP 地址配置；浏览器仍调用 media-service 的来源 API，保存与 MediaMTX 对账不进入 device-service。
- **依据**：用户期望在设备工作区配置边缘设备，同时保持 device/media 两个服务的现有数据所有权。
- **影响**：device-service 继续负责摄像头账号归属；前端单一 RTSP URL 字段；媒体来源更新由 media-service 校验归属后保存。

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

### 设备授权租期与到期撤权（2026-10-23）

- **决定**：设备激活请求保存 `duration_seconds`；激活成功后从服务器时间计算 `activation_expires_at`。`null` 表示无限期。device-service 每 15 秒检查到期设备，并在单个事务内将设备置为 `expired`、写 `device.disabled` outbox；随后立即尝试撤销 MQTT 身份，失败继续由现有撤销对账循环重试。
- **网页激活与续期**：已预置可信身份的设备由所有者在网页提交不含秘密值的请求，Atlas agent 后台轮询并领取 MQTT 密码；用户日常操作不需要登录板端。请求消费后，页面保持 `syncing`，直到 agent 用新身份再次轮询确认保存成功。若凭据响应丢失，服务端可用现有 broker 管理机密和请求 ID 重放相同密码，不将明文密码写入数据库；设备确认后清除旧密码哈希。`expired` 资产仍可续期并写 `device.activated` outbox，media-service 恢复保留来源配置的流；人工停用的 `disabled` 设备不会被续期入口匹配。首次安装 agent、CA、HTTPS Origin 和可信设备身份仍由管理员部署流程预置。
- **依据**：用户要求自定义或无限设备授权期限，并明确要求到期自动停用和撤销 MQTT 权限。
- **影响**：PostgreSQL 自动迁移新增设备到期时间和票据期限字段；列表/详情仅额外显示 `expired`；业务鉴权仍以 `active` 和激活态为准；到期与重新激活分别使用 media outbox，并沿用 broker 撤销恢复路径。

## 状态依据

设计描述目标与约束，不代表已实现；状态由[任务与证据清单](tasks.md)、仓库代码和部署记录确认。
