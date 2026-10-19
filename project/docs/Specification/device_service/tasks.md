# device-service 实施与验收任务

> 关联合同：[spec.md](spec.md)；设计：[design.md](design.md)。

## 任务清单

- [ ] **T-01 服务骨架、device schema 与资产 API** — 迁移、健康检查、设备/摄像头登记修改/停用、账号隔离及统计；覆盖 REQ-01 / AC-01.1、AC-01.2；依赖：无。
- [ ] **T-02 一次性激活与设备级 MQTT 身份** — 票据哈希、过期/消费、Dynamic Security 身份与 ACL、轮换/撤销/对账；分别配置服务内 broker URL 与边缘 advertised URL，验证端侧使用激活响应的固定 client ID、不泄露内部 DNS 并可由 Atlas TLS 连通；覆盖 REQ-02 / AC-02.1–02.4。
- [ ] **T-03 心跳、算法能力与版本化配置** — 心跳过期、Atlas 顶层设备运行状态与 camera/algorithm 数组、运行状态拆分、期望/实报版本、命令和 ACK；课堂类别 allowlist、规则全关默认及参数校验依 [spec §5.2.1](spec.md)；覆盖 REQ-03 / AC-03.1、AC-03.2、AC-03.3。
- [ ] **T-04 最小权限内部查询与跨服务恢复** — event/media 归属查询、课堂规则版本校验接口、device.disabled outbox、broker 撤销重试及依赖故障分类；覆盖 REQ-04 / AC-04.1、AC-04.2。
- [ ] **T-05 device-service 服务内集成测试** — 隔离 PostgreSQL + 测试 Mosquitto Dynamic Security，覆盖 A/B 账号资产隔离、重复/过期激活、topic ACL、标准及 Atlas 实机状态 payload、未知字段拒绝、心跳离线、配置版本 ACK、规则默认关闭/拒绝越界/校验历史规则版本、停用后身份撤销与 outbox 恢复；覆盖 REQ-01、REQ-02、REQ-03、REQ-04 / AC-01.1、AC-01.2、AC-02.1、AC-02.2、AC-03.1、AC-03.2、AC-03.3、AC-04.1、AC-04.2；依赖 T-01–T-04。
- [ ] **T-06 镜像、凭据与运维恢复** — 容器构建、迁移、broker 与数据库恢复对账、指标/日志脱敏；覆盖 REQ-01、REQ-04 / AC-01.2、AC-04.2；依赖 T-01–T-05。

## 验收证据

| REQ/AC | 检查方法 | 当前证据 | 状态 |
|---|---|---|---|
| REQ-01/AC-01.1–01.2 | 资产/摄像头账号隔离及停用测试 | 隔离 PostgreSQL 集成测试覆盖双账号资产隔离、摄像头唯一性、停用幂等和单条 outbox；`go test ./...`、`go test -tags=integration ./...` 通过 | 部分通过 |
| REQ-02/AC-02.1–02.4 | 票据、Mosquitto ACL、边缘 advertised broker 和固定 client ID 集成测试 | 隔离 PostgreSQL 集成测试及 broker URL 校验通过；Atlas 已通过 TLS/MQTT 发布心跳并刷新 `last_seen_at`。激活接口到设备凭据下发、轮换/撤销的实机闭环仍待验收 | 部分通过 |
| REQ-03/AC-03.1–03.2 | 心跳与期望/实报版本测试 | PostgreSQL/handler 集成测试通过；Atlas agent 已经由真实 TLS/MQTT 将心跳写入云端并刷新 `last_seen_at`；设备配置 ACK 仍待实测 | 部分通过 |
| REQ-03/AC-03.3 | Atlas systemd agent 状态 payload + PostgreSQL 集成测试；未知字段/非法类型负向用例 | 隔离 PostgreSQL `go test -tags=integration -count=1 ./...` 通过；Atlas 上报字段被接受，云端持久化 `mqtt_state=connected`、`model_state=installed_not_loaded`、`stream_state=not_configured` | 通过 |
| REQ-04/AC-04.1–04.2 | 服务凭据、规则配置校验和恢复测试 | 规则配置和内部访问集成测试通过；broker 故障恢复及下游 media 清理重试待真实容器验证 | 部分通过 |

## 环境与执行入口

- 开发/服务测试：`go test ./...`；服务内集成测试：`go test -tags=integration ./...`。
- 集成环境必须隔离 PostgreSQL、Mosquitto 及 Dynamic Security 配置；不连 Atlas 实机的测试不得声称板端规则已生效。
