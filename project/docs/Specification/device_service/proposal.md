# device-service 范围提案

> 状态：基线；实现状态以代码、部署记录和验收证据为准。
> 关联文档：[总目录](../README.md)、[产品需求](../../Analysis/PRD.md)、[总技术规格](../../Analysis/SPEC.md)、[系统架构](../../Design/STRUCTURE.md)、[服务规格](spec.md)。

## 目标

管理账号拥有的设备、摄像头、算法能力、期望配置和运行实报。

## 范围

资产登记、一次性激活、MQTT 身份、心跳、配置版本/回执及归属查询。

## 本次变更：边缘可达的 MQTT 接入地址

Atlas 激活响应不得把 Compose 内部 DNS 地址下发给设备。device-service 继续用 `MQTT_BROKER_URL` 连接 Docker 网络内的 broker，并新增 `MQTT_ADVERTISED_BROKER_URL` 配置激活响应中的设备接入地址；两者均使用 TLS。运维负责将 advertised 地址路由到 broker TLS listener。该变化只影响激活响应与部署配置，不改变 MQTT topic/ACL 和设备身份合同。

## 本次变更：Atlas 状态心跳字段兼容

Atlas agent 上报设备、相机、NPU、模型、流、MQTT、配置版本和本地队列状态。device-service 必须识别并校验这些顶层状态字段，同时继续支持现有 `cameras`、`algorithms` 数组；有效报告更新 `last_seen_at` 并原样存入 `reported_state`。未知字段或无效值仍拒绝，以维持严格 schema 校验。

## 边界

事件处置、媒体播放授权和其他服务数据库读写。

## 文档关系

本提案记录长期范围；`spec.md` 是详细需求与验收合同，`design.md` 汇总技术边界，`tasks.md` 记录核验与实施进度。实现状态必须由代码、运行结果和验收证据判断，不能由本文件状态推断。
