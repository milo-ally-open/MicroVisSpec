# media-service 范围提案

> 状态：基线；实现状态以代码、部署记录和验收证据为准。
> 关联文档：[总目录](../README.md)、[产品需求](../../Analysis/PRD.md)、[总技术规格](../../Analysis/SPEC.md)、[系统架构](../../Design/STRUCTURE.md)、[服务规格](spec.md)。

## 目标

管理实时流配置、观看授权、并发配额及 MediaMTX 对账。

## 范围

来源登记、播放授权/回收、WebRTC/HLS、状态和故障反馈。

## 本次变更：简化边缘视频来源配置

设备详情页只配置一个可达的 RTSP 地址；来源用户名/密码字段和独立 API 属性移除。media-service 仍独立保存来源配置并负责 MediaMTX 对账，只有已授权摄像头所属账号可读取和更新保存的 RTSP 地址。新地址禁止 URL userinfo（`user:pass@host`）；地址在 media schema 中继续加密保存。升级时将旧加密来源中的独立用户名/密码字段清除并仅保留 URL。Atlas 输出端改为无需认证的 RTSP 地址；仅面向受信任、可路由的园区网络。

## 边界

设备资产主数据、录像回放、后端统一转码和媒体帧处理。

当前服务规格的授权主体是单账号基线。三类用户目标下，租户资源按 `workspace_id` 隔离，播放主体仍是发起请求的账号/会话；media-service 通过 auth-service 校验 `stream.view` 设备授权并对每次信令/HLS 请求复核，撤权后新请求立即拒绝、服务健康时 30 秒内终止活动连接。共享行为与撤权合同见[授权规格](../multi_user_workspace_access/)。

## 文档关系

本提案记录长期范围；`spec.md` 是详细需求与验收合同，`design.md` 汇总技术边界，`tasks.md` 记录核验与实施进度。实现状态必须由代码、运行结果和验收证据判断，不能由本文件状态推断。
