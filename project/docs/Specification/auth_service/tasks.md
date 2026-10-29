# auth-service 实施与验收任务

> 关联合同：[spec.md](spec.md)；设计：[design.md](design.md)。每项任务应在 Windows Go 开发机可复现；未执行项不得标记通过。

## 任务清单

- [ ] **T-01 服务骨架、auth schema 与健康检查** — 配置加载、迁移、日志/request_id、`/health/live` 与 `/health/ready`；数据库账号只访问 auth schema；覆盖 REQ-01 / AC-01.1、AC-01.2；依赖：无。
- [ ] **T-02 账号注册、登录、退出与改密** — Argon2id、用户名规范化/唯一、限流、Cookie 属性、CSRF、会话过期/撤销 API；覆盖 REQ-02 / AC-02.1、AC-02.2。
- [ ] **T-03 Nginx 会话校验与账号信任边界** — 实现内部校验接口、清理/验证身份头和可信代理；拒绝伪造 account/session 头；覆盖 REQ-03 / AC-03.1、AC-03.2。
- [~] **T-04 session.revoked outbox 与可靠发布** — 账号停用、退出/改密及工作空间成员撤销均在撤销 session 的同一事务写 outbox；稳定事件 ID、退避重试和内部凭据已实现，投递 media-service/push-service inbox 并按下游幂等消费。单元与 stub 重试测试通过；本机无隔离 `TEST_DATABASE_URL`，数据库事务集成未执行。覆盖 REQ-04 / AC-04.1、AC-04.2。
- [ ] **T-05 auth-service 服务内集成测试** — 临时 PostgreSQL + HTTP/Nginx 身份模拟，覆盖重复用户名/并发注册、登录失败不泄露、CSRF、旧密码错误/改密撤销、跨账号隔离、过期与 outbox 重启补投；覆盖 REQ-01、REQ-02、REQ-03、REQ-04 / AC-01.1、AC-01.2、AC-02.1、AC-02.2、AC-03.1、AC-03.2、AC-04.1、AC-04.2；依赖 T-01–T-04。
- [ ] **T-06 原生运行配置与恢复说明** — 单文件 Go 构建、systemd 非 root 运行、密钥文件注入、迁移与备份/恢复说明；完成构建与健康探针验收；覆盖 REQ-01、REQ-04 / AC-01.1、AC-04.2；依赖 T-01–T-05。

- [~] **T-07 三类账号与工作空间授权扩展** — 已新增 `user_type/status` 字段并维持旧账号默认兼容；注册固定创建 PC 并事务创建以 owner UUID 为 ID 的 workspace；启动时幂等回填既有 PC workspace。登录和会话校验拒绝非 active 账号，`/me`/login 返回服务端 `user_type`，内部会话验证返回可信 `X-User-Type`。新增 workspace/member/invitation/device grant/audit 模型，24 小时单次邀请创建/兑换事务、设备服务在线归属校验和受控内部设备能力判定；PC owner 可从 API 更新工作空间名称，事务内写审计；PC 成员管理/撤销以及 admin bootstrap、账号管理、健康和审计 API/UI 已实现。成员撤销在事务中撤销其所有 session 并写 revocation outbox。仍缺隔离 PostgreSQL HTTP 集成、完整审计矩阵和服务端故障验收；Go 单测通过，数据库集成因无隔离测试库而跳过。workspace 是资源租户边界，session/account ID 仍表示真实主体。覆盖本组 REQ-01 / AC-01.2、REQ-02 / AC-02.1、REQ-03 / AC-03.1、AC-03.2、REQ-04 / AC-04.1、AC-04.2；依赖 multi_user_workspace_access T-01、T-03。

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
