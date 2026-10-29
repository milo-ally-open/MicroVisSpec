# auth-service 设计索引

> 本文件汇总服务边界与设计方向，不复制详细合同；具体行为以[服务规格](spec.md)为准。

## 责任边界

统一管理 PC/mobile/admin 身份、账号状态、密码、浏览器会话、owner workspace、mobile member/invitation/grants、平台 admin bootstrap 与审计；不持有业务服务资源明细。

密码找回及设备/事件/媒体/Push 数据读写；业务服务保留资源写入权，平台 admin 不绕过业务授权。

## 设计方向

账号与会话由本服务独占；浏览器使用同源 Cookie；业务服务按可信 account_id 再授权；机密来自部署环境。

三类用户目标：auth-service 持有 `user_type/status`、workspace/member/invitation/device grant 与审计；会话及操作者 `account_id` 始终标识真实用户主体，workspace 是资源授权边界。成员、设备授权变更由在线内部授权接口提供，依赖不可用时拒绝访问。详细合同见 [共享授权规格](../multi_user_workspace_access/)。

授权接口包括 `POST /internal/v1/authorization/check`、分页的 `/authorization/devices` 和 `/authorization/recipients`。调用方使用按服务凭据认证，并提交由 Nginx 会话校验结果取得的 account/session ID；auth-service 重查活动会话、账号、workspace owner、活动成员关系和精确 capability。设备归属由 device-service 内部接口核实，依赖超时或响应无效时拒绝授权。管理入口是 `/api/v1/admin/*`；首次 admin 仅经配置了独立一次性 bootstrap secret 的内部入口创建，admin Web UI 不经通用客户资源授权。

## 决策记录

已明确的方向以服务规格为准。未记录的备选方案不推定为已评审结论；影响接口、数据所有权或部署方式的变化，应记录决定、依据和影响并同步规格。

## 状态依据

设计描述目标与约束，不代表已实现；状态由[任务与证据清单](tasks.md)、仓库代码和部署记录确认。
