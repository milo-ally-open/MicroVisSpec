# event-service 实施与验收任务

> 关联合同：[spec.md](spec.md)；设计：[design.md](design.md)。

## 任务清单

- [ ] **T-01 服务骨架、event schema、迁移和健康检查** — events/evidence/dispositions/outbox 表、约束、私有证据卷；覆盖 REQ-01、REQ-02、REQ-03、REQ-04 / AC-01.1、AC-02.1、AC-03.1、AC-04.1。
- [ ] **T-02 MQTT 接收、设备/课堂规则校验、幂等和应用 ACK** — schema/version/topic 校验、device-service 内部查询、课堂规则配置版本/阈值校验、事务落库后 accepted ACK、永久拒绝 ACK 和暂时故障不终结；覆盖 REQ-01 / AC-01.1、AC-01.2、AC-01.3。
- [ ] **T-03 证据上传、鉴权、24 小时过期与清理** — 设备认证 HTTPS 上传、图片校验、私有文件写入、重复上传/冲突、孤儿文件回收；覆盖 REQ-02 / AC-02.1、AC-02.2。
- [ ] **T-04 账号级查询、状态流转、游标与统计** — 列表/详情、版本并发控制、终态结果、时间范围和事件聚合；覆盖 REQ-03 / AC-03.1、AC-03.2。
- [ ] **T-05 event.created outbox 和通知投递恢复** — 新事件与 outbox 同事务、内部 inbox 凭据、重试/租约/积压观测；只为有效规则事件创建通知意图；覆盖 REQ-04 / AC-04.1、AC-04.2。
- [ ] **T-06 event-service 服务内集成测试** — PostgreSQL + Mosquitto + 可控 device-service/push-service stub + 临时证据卷，覆盖重复 MQTT/ACK 丢失、未授权/停用设备、禁用/未应用/越阈规则、截图到期/丢失、跨账号查询与处置、并发版本冲突、时区统计、push 故障后补投；覆盖 REQ-01、REQ-02、REQ-03、REQ-04 / AC-01.1、AC-01.2、AC-01.3、AC-02.1、AC-02.2、AC-03.1、AC-03.2、AC-04.1、AC-04.2；依赖 T-01–T-05。
- [ ] **T-07 数据恢复、镜像和可观测性** — 数据库权限、私卷容量、每日/每小时维护任务、服务重启及日志/指标；覆盖 REQ-02、REQ-04 / AC-02.2、AC-04.2；依赖 T-01–T-06。

- [~] **T-08 workspace 事件授权与真实操作者记录** — 已按 auth-service `event.read` grant 过滤移动列表/统计，在详情/证据/处置入口逐次检查设备能力，处置记录保存可信 `actor_account_id`。event/event.created outbox 增加 `workspace_id` nullable 扩展列，并按 owner-account/workspace ID 规则幂等回填及在新事件事务中双写；Push outbox 包含可信 device ID。旧库迁移、孤儿核对、真实跨服务撤权/证据访问和集成验收未完成；本轮仅 Go build 编译证据。覆盖本组 REQ-02 / AC-02.1、AC-02.2、REQ-03 / AC-03.1、REQ-04 / AC-04.1，并满足共享授权规格定义的事件、证据、处置授权合同；依赖 multi_user_workspace_access T-04、T-05、T-07。

## 验收证据

| REQ/AC | 检查方法 | 当前证据 | 状态 |
|---|---|---|---|
| REQ-01/AC-01.1–01.3 | MQTT 重复、永久拒绝、临时依赖故障及业务 ACK 测试 | `go test ./...` 和隔离 PostgreSQL integration suite 通过，覆盖重复摄取/ACK、设备依赖 stub、拒绝与重试；实际 Mosquitto 链路待部署验收 | 部分通过 |
| REQ-02/AC-02.1–02.2 | 上传授权、内容校验、到期与清理测试 | 数据库/临时证据集成测试通过；设备凭据实际 HTTPS 上传及 24 小时运行清理待目标主机验证 | 部分通过 |
| REQ-03/AC-03.1–03.2 | 多账号、非法/并发处置、统计集成测试 | API integration suite 的多账号查询、处置和统计用例通过；完整浏览器与跨服务流程待联调 | 部分通过 |
| REQ-04/AC-04.1–04.2 | outbox 原子性、重复投递、恢复测试 | PostgreSQL/outbox 与 push stub 集成用例通过；systemd 重启和真实 push-service 补投待部署验证 | 部分通过 |

## 环境与执行入口

- `go test ./...`；服务内集成测试 `go test -tags=integration ./...`，需隔离 PostgreSQL、Mosquitto、HTTP stub 和证据临时目录。
- 规则校验依赖 device-service 返回历史已应用规则版本；该依赖不可用时必须不 ACK，不可回退为仅按 event_type 信任。
