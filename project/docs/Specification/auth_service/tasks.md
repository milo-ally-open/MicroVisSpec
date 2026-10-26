# auth-service 实施与验收任务

> 关联合同：[spec.md](spec.md)；设计：[design.md](design.md)。每项任务应在 Windows Go 开发机可复现；未执行项不得标记通过。

## 任务清单

- [ ] **T-01 服务骨架、auth schema 与健康检查** — 配置加载、迁移、日志/request_id、`/health/live` 与 `/health/ready`；数据库账号只访问 auth schema；覆盖 REQ-01 / AC-01.1、AC-01.2；依赖：无。
- [ ] **T-02 账号注册、登录、退出与改密** — Argon2id、用户名规范化/唯一、限流、Cookie 属性、CSRF、会话过期/撤销 API；覆盖 REQ-02 / AC-02.1、AC-02.2。
- [ ] **T-03 Nginx 会话校验与账号信任边界** — 实现内部校验接口、清理/验证身份头和可信代理；拒绝伪造 account/session 头；覆盖 REQ-03 / AC-03.1、AC-03.2。
- [ ] **T-04 session.revoked outbox 与可靠发布** — 撤销同事务写 outbox、稳定事件 ID、退避重试、内部服务凭据；覆盖 REQ-04 / AC-04.1、AC-04.2。
- [ ] **T-05 auth-service 服务内集成测试** — 临时 PostgreSQL + HTTP/Nginx 身份模拟，覆盖重复用户名/并发注册、登录失败不泄露、CSRF、旧密码错误/改密撤销、跨账号隔离、过期与 outbox 重启补投；覆盖 REQ-01、REQ-02、REQ-03、REQ-04 / AC-01.1、AC-01.2、AC-02.1、AC-02.2、AC-03.1、AC-03.2、AC-04.1、AC-04.2；依赖 T-01–T-04。
- [ ] **T-06 原生运行配置与恢复说明** — 单文件 Go 构建、systemd 非 root 运行、密钥文件注入、迁移与备份/恢复说明；完成构建与健康探针验收；覆盖 REQ-01、REQ-04 / AC-01.1、AC-04.2；依赖 T-01–T-05。

## 验收证据

| REQ/AC | 检查方法 | 当前证据 | 状态 |
|---|---|---|---|
| REQ-01/AC-01.1–01.2 | schema 权限、配置错误与健康接口集成测试 | `go test ./...` 与 `go test -tags=integration ./...` 在临时隔离 PostgreSQL 上通过；systemd 角色权限/缺失机密启动检查待运行 | 部分通过 |
| REQ-02/AC-02.1–02.2 | API 测试、Cookie/CSRF 浏览器请求、会话数据库核对 | 数据库集成测试覆盖注册/登录/me/CSRF/退出；真实浏览器 Cookie 与 Nginx 流程待联调 | 部分通过 |
| REQ-03/AC-03.1–03.2 | 伪造身份头、跨账号资源测试 | auth 内部会话校验的伪造凭据测试通过；device/event/media/push 跨服务账号联测待运行 | 部分通过 |
| REQ-04/AC-04.1–04.2 | 撤销事务/outbox 重复及服务重启测试 | 隔离 PostgreSQL 集成测试验证稳定事件 ID、重试和双消费者 stub；systemd 重启与真实下游仍待验证 | 部分通过 |

## 环境与执行入口

- 开发单元/服务测试：`go test ./...`。
- 服务内集成测试：`go test -tags=integration ./...`，使用隔离的 PostgreSQL，不复用开发/生产数据。
- 最近执行：`go test ./...` 与 `go test -count=1 -tags=integration ./...` 均通过；集成命令使用一次性 Docker PostgreSQL `yunshao_it`，测试后已停止并删除容器。完整 Nginx/五服务端到端验收仍待部署 smoke test。
