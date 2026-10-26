# device-service 范围提案

> 状态：基线；实现状态以代码、部署记录和验收证据为准。
> 关联文档：[总目录](../README.md)、[产品需求](../../Analysis/PRD.md)、[总技术规格](../../Analysis/SPEC.md)、[系统架构](../../Design/STRUCTURE.md)、[服务规格](spec.md)。

## 目标

管理账号拥有的设备、摄像头、算法能力、期望配置和运行实报。

## 范围

资产登记、一次性激活、MQTT 身份、心跳、配置版本/回执及归属查询。

## 本次变更：边缘可达的 MQTT 接入地址

Atlas 激活响应不得把仅供后端使用的 MQTT 地址下发给设备。device-service 使用 `MQTT_BROKER_URL` 连接 Mosquitto，并用独立的 `MQTT_ADVERTISED_BROKER_URL` 配置激活响应中的设备接入地址；两者均使用 TLS。运维负责将 advertised 地址路由到 broker TLS listener。该变化只影响激活响应与部署配置，不改变 MQTT topic/ACL 和设备身份合同。

## 本次变更：Atlas 状态心跳字段兼容

Atlas agent 上报设备、相机、NPU、模型、流、MQTT、配置版本和本地队列状态。device-service 必须识别并校验这些顶层状态字段，同时继续支持现有 `cameras`、`algorithms` 数组；有效报告更新 `last_seen_at` 并原样存入 `reported_state`。未知字段或无效值仍拒绝，以维持严格 schema 校验。

## 本次变更：设备详情中的 RTSP 地址配置入口

设备详情页为设备下登记的摄像头提供一个 RTSP 地址配置入口，方便用户从设备工作区完成边缘视频接入。摄像头及账号归属仍由 device-service 校验；来源 URL、状态与 MediaMTX 对账继续由 media-service 持有。界面和写请求不提供来源用户名或密码。

## 本次变更：设备授权期限与自动停用

网页激活请求可携带使用时长：`duration_seconds: null` 表示无限期，正整数表示自激活成功后开始计算的自定义时长。到期后 device-service 将设备设为 `expired`、停用业务访问、写入 `device.disabled` outbox 并撤销 MQTT 身份；过期设备保留在账号设备列表。用户在网页提交续期后，已预置可信身份的 Atlas agent 自动轮询领取新凭据；服务端写 `device.activated` outbox 恢复已保存媒体来源。浏览器不接收票据或 MQTT 密码；首次设备部署仍需管理员预置 agent、CA 与可信设备身份。

## 本次变更：网页引导首次 Atlas 接入

园区业务用户可能只有管理员提供的 SSH 登录账号，不应要求其复制票据或操作 Atlas 终端。设备详情页提供首次接入向导：用户确认登记设备的 SSH 主机指纹、输入有权使用的板端账号密码并选择授权期限；device-service 以一次性 SSH 会话检查/安装 agent、部署 TLS CA、通过现有一次性票据协议写入 MQTT 身份并启动 agent。SSH 密码只用于当前会话，不保存到数据库/任务记录，不写入命令参数或日志。设备端仍验证 HTTPS 与 MQTT TLS；浏览器不取得票据或 MQTT 凭据。

首次安装从 device-service 镜像内的 Atlas agent 源码包在目标板构建 agent-only 产物；目标板必须已安装 Atlas CANN 运行环境并允许访问其配置的软件包源。主机指纹按设备固定；改变或重装 SSH 主机密钥时须通过受控的设备维护流程重新建立信任。SSH 只连接账号名下登记的私网设备 IP、固定端口 22，并使用固定的仓库部署脚本，不提供远端命令输入。

失败时任务只保留安全阶段、错误代码与结果，不保留密码、票据、MQTT 密码或原始 SSH 输出；服务清理远端临时包及证书暂存文件。板端依赖缺失、网络不可达或 SSH 权限不足时，页面给出对应处理提示，已安装的 agent 保留以便重试。

## 边界

事件处置、媒体播放授权和其他服务数据库读写。

## 文档关系

本提案记录长期范围；`spec.md` 是详细需求与验收合同，`design.md` 汇总技术边界，`tasks.md` 记录核验与实施进度。实现状态必须由代码、运行结果和验收证据判断，不能由本文件状态推断。
