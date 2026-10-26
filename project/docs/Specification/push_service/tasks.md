# push-service 实施与验收任务

> 关联合同：[spec.md](spec.md)；设计：[design.md](design.md)。

## 任务清单

- [ ] **T-01 服务骨架、push schema 和机密管理** — 迁移、健康检查、VAPID key、订阅密钥加密、租户内数据库权限；覆盖 REQ-01 / AC-01.1。
- [ ] **T-02 订阅、查询、账号切换与解绑 API** — CSRF/会话验证、endpoint 归属/重绑、当前账号过滤、幂等删除；覆盖 REQ-01 / AC-01.1、AC-01.2。
- [ ] **T-03 服务间 inbox 与撤销处理** — producer/service 身份校验、schema_version、inbox 去重、event.created 仅按可信账号路由、session.revoked 撤销及乱序处理；覆盖 REQ-02 / AC-02.1、AC-02.2。
- [ ] **T-04 Web Push 投递、错误分类和重试** — 租约领取、发送前复核订阅状态、VAPID 加密、临时退避/永久失效清理/过期停止及准确 accepted 语义；覆盖 REQ-03 / AC-03.1、AC-03.2。
- [ ] **T-05 push-service 服务内集成测试** — PostgreSQL + 可控 Web Push endpoint/mock + auth/event producer stub，覆盖账号切换 endpoint 防串发、重复 inbox、乱序撤销、并发领取、404/410 清理、429/5xx 重试、过期事件与重启恢复、秘密不泄漏；覆盖 REQ-01、REQ-02、REQ-03、REQ-04 / AC-01.1、AC-01.2、AC-02.1、AC-02.2、AC-03.1、AC-03.2、AC-04.1、AC-04.2。
- [ ] **T-06 PWA 通知接口联调与运维** — VAPID 配置、delivery 指标和 Web/PWA 订阅/点击/解绑验收；只发送已启用课堂规则事件；覆盖 REQ-04 / AC-04.1、AC-04.2。

## 验收证据

### 2026-10-22 本机补充验收

- 既有 push-service 生产代码未修改。本轮重新运行 `go test -count=1 -v -tags=integration ./...`，在独立的 `push_debug_it_20261022` PostgreSQL 数据库中 10/10 通过、无跳过，覆盖 endpoint 重绑、撤销、重复 inbox、并发领取、429 重试、410 停用、过期及密钥保护。见 [local_integration_20261022.json](evidence/local_integration_20261022.json)。
- 独立工具 `src/tools/push-debug/` 已实现表单、两账号专用模拟设备、规则 ACK、MQTT 事件和只读投递观察。两个账号的事件均进入真实 Push inbox，event outbox 已投递；双向事件/证据跨账号访问被拒绝，同一 request_id 重试复用事件。
- 当前测试账号有效订阅数为 0，没有伪造订阅或实际手机投递。10 项服务端测试中的可控 sender 不代表真实浏览器 Push。本轮用户明确将手机显示/点击与 CodeArts 实机证据后补。工具合同与本机测试入口见 [README](../../../src/tools/push-debug/README.md) 与 [CONTRACT](../../../src/tools/push-debug/CONTRACT.md)。

| REQ/AC | 检查方法 | 当前证据 | 状态 |
|---|---|---|---|
| REQ-01/AC-01.1–01.2 | 账号/endpoint 绑定、权限及密钥保护测试 | `go test ./...` 与隔离 PostgreSQL integration suite 通过，覆盖 endpoint 重绑、跨账号隔离与秘密处理 | 部分通过 |
| REQ-02/AC-02.1–02.2 | inbox 去重、撤销和乱序恢复集成测试 | 数据库集成测试覆盖重复 event、session.revoked 和订阅取消；真实 producer 服务投递待联调 | 部分通过 |
| REQ-03/AC-03.1–03.2 | mock endpoint 错误分类、重试和 accepted 语义测试 | fake Web Push sender 测试覆盖 429/410、退避、过期和 accepted 状态；外部浏览器 Push 仍待执行 | 部分通过 |
| REQ-04/AC-04.1–04.2 | service worker 点击路由与通知载荷审查 | Web Vitest 覆盖通知路由/敏感路径策略；真实 service worker/browser Push 生命周期待手工验收 | 部分通过 |

## 环境与执行入口

- 最近执行：`go test ./...` 与 `go test -count=1 -tags=integration ./...` 均通过；集成测试使用临时隔离 PostgreSQL 和可控 push sender，未代替真实浏览器 Push 验收。
- 浏览器真实 Push 验收另需 HTTPS、安全上下文、VAPID 和支持的浏览器；mock endpoint 不能替代实际浏览器验收。
