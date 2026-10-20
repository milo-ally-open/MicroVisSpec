# media-service 设计索引

> 本文件汇总服务边界与设计方向，不复制详细合同；具体行为以[服务规格](spec.md)为准。

## 责任边界

管理实时流配置、观看授权、并发配额及 MediaMTX 对账。

设备资产主数据、录像回放、后端统一转码和媒体帧处理。

## 设计方向

media schema 独占；仅来源 RTSP URL 加密保存；摄像头归属由 device-service 核验；播放按租约管理。RTSP URL 不含 userinfo，不单独支持来源用户名/密码。Nginx 将媒体请求交给 media-service 单次鉴权；media-service 使用服务间凭据向 auth-service 校验原始会话 Cookie 与请求元数据，再用返回的可信账号和会话 ID 校验观看租约。浏览器提供的身份头不参与授权。

## 决策记录

已明确的方向以服务规格为准。Nginx 嵌套 `auth_request` 的上游身份头在实际部署中未传至 media-service，导致合法租约播放被拒；采用 media-service 直连 auth-service 会话校验后，身份验证和租约验证在同一个授权处理器中完成。未记录的备选方案不推定为已评审结论；影响接口、数据所有权或部署方式的变化，应记录决定、依据和影响并同步规格。

### 来源配置仅保存 RTSP URL（2026-10-21）

- **决定**：设备详情配置一个 RTSP URL；来源 API 不再接收独立用户名/密码，URL userinfo 仍拒绝。状态接口仅向已授权的摄像头所有者回显保存的 URL；播放接口不返回 URL。URL 仍以现有 media source key 加密存储。
- **依据**：用户要求简化设备配置；Atlas 输出也改为无需认证的园区 RTSP 地址。
- **兼容**：启动迁移将历史加密 JSON 中的 URL 保留、移除独立认证字段并重加密；之后 MediaMTX 对账使用 URL 原值。回滚旧版本不会恢复已移除的源账号密码，需要为旧版重新配置可访问的无认证 RTSP 源。
- **边界**：地址仅在同账号状态接口可读，日志继续不打印完整 URL。无需改变 `media.streams.encrypted_source` 的 SQL 列或来源加密密钥。

## 状态依据

设计描述目标与约束，不代表已实现；状态由[任务与证据清单](tasks.md)、仓库代码和部署记录确认。
