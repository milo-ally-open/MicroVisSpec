# push-service 范围提案

> 状态：基线；实现状态以代码、部署记录和验收证据为准。
> 关联文档：[总目录](../README.md)、[产品需求](../../Analysis/PRD.md)、[总技术规格](../../Analysis/SPEC.md)、[系统架构](../../Design/STRUCTURE.md)、[服务规格](spec.md)。

## 目标

管理浏览器 Push 订阅及通知投递。

## 范围

PWA 订阅、VAPID、事件 inbox、可靠投递、失效清理和点击路由。

## 边界

事件事实、会话主数据、原生推送和用户已读回执。

## 文档关系

2026-10-22 本机联调补充：用户授权独立表单和逻辑后端，在 `src/tools/push-debug/` 使用专用模拟设备接入现有事件链路；不增加生产通知绕过接口，不改变账号归属。工具的 REQ/AC、状态及安全边界见 [CONTRACT](../../../src/tools/push-debug/CONTRACT.md)。手机和 CodeArts 实机证据按用户决定后补。

本提案记录长期范围；`spec.md` 是详细需求与验收合同，`design.md` 汇总技术边界，`tasks.md` 记录核验与实施进度。实现状态必须由代码、运行结果和验收证据判断，不能由本文件状态推断。
