# media-service 实施与验收任务

> 关联合同：[spec.md](spec.md)；设计：[design.md](design.md)。

## 任务清单

- [ ] **T-01 服务骨架、media schema 与来源 URL 保护** — 迁移、健康检查、仅 RTSP URL 加密保存、camera_id 所有权校验及状态 URL 回显；覆盖 REQ-01 / AC-01.1、AC-01.4。
- [ ] **T-02 MediaMTX 期望配置与对账** — stream path、sourceOnDemand、版本化配置、重启恢复及 device.disabled 清理；覆盖 REQ-01 / AC-01.2。
- [ ] **T-03 播放鉴权、租约和并发配额** — Nginx 单次媒体 auth 子请求，media-service 使用服务间凭据向 auth-service 校验 Cookie、Origin、CSRF 和原始方法；仅信任 auth-service 返回的账号/会话 ID，再校验摄像头归属、有效租约及原子 12 源/24 连接配额，并处理续租/释放；覆盖 REQ-02 / AC-02.1、AC-02.2；REQ-04 / AC-04.1。
- [ ] **T-04 WebRTC/HLS 播放和错误分类** — WHEP/HLS 受保护入口、回退 URL、媒体/设备状态区分和容量错误；覆盖 REQ-03 / AC-03.1、AC-03.2。
- [ ] **T-05 撤销、过期租约与 MediaMTX 恢复** — session.revoked/device.disabled/device.activated inbox 幂等消费；设备生命周期通知按 device-service 当前状态抑制乱序操作，设备到期停止路径、续期恢复保留来源配置；覆盖 REQ-04 / AC-04.1、AC-04.2。
- [ ] **T-06 media-service 服务内集成测试** — PostgreSQL + MediaMTX 控制面 + 测试 RTSP 源 + auth/device stub，覆盖跨账号拒绝、摄像头删除/停用、URL 明文不泄漏、独立用户名/密码字段与 URL userinfo 拒绝、状态 URL 所有权、配置漂移修复、配额并发竞态、WebRTC 失败/HLS 回退、登出结束播放、租约过期/重启释放；覆盖 REQ-01、REQ-02、REQ-03、REQ-04 / AC-01.1–01.4、AC-02.1、AC-02.2、AC-03.1、AC-03.2、AC-04.1、AC-04.2；依赖 T-01–T-05。
- [ ] **T-07 Atlas/园区媒体容量验收** — 单路教室处理后 RTSP 接入、协议与浏览器验证、12 路源与 24 个连接负载实测；记录 CPU、内存、带宽和失败状态；覆盖 REQ-02 / AC-02.2；依赖边缘视频输出。
- [ ] **T-08 历史 RTSP 来源兼容清理** — 启动时将旧密文解密为 RTSP URL-only、清除旧独立认证属性并重加密；迁移幂等，密钥错误/解密失败时服务不可就绪且不覆盖原记录；覆盖 REQ-01 / AC-01.3、AC-01.4；依赖 T-01。

- [~] **T-09 workspace 播放授权与成员撤权** — 建立/续租播放 lease 及每次 Nginx 媒体授权请求均通过 auth-service 检查 session 对设备的 `stream.view` grant；viewer/session 继续记录真实用户主体。lease 为 25 秒、到期扫描每秒触发，最多 24 条活动租约并发 kick。`workspace_id` 迁移、owner 来源配置 UI/API、成员撤销事件触发即时 kick、真实 MediaMTX/WHEP/HLS 的 30 秒上界仍未验收；当前只有 Go build 证据。覆盖本组 REQ-02 / AC-02.1、REQ-04 / AC-04.1、AC-04.2，并满足共享授权规格定义的跨工作空间访问与撤权合同；依赖 multi_user_workspace_access T-04–T-06。

## 验收证据

| REQ/AC | 检查方法 | 当前证据 | 状态 |
|---|---|---|---|
| REQ-01/AC-01.1–01.2 | 来源授权/凭据加密/MediaMTX 对账集成测试 | `go test ./...` 与隔离 PostgreSQL integration suite 通过，覆盖来源密钥与配置逻辑；真实 MediaMTX API 对账待部署验证 | 部分通过 |
| REQ-02/AC-02.1–02.2 | 越权播放、并发配额竞态和容量记录 | handler/数据库集成测试覆盖跨账号授权与租约配额；园区多路容量实测仍待执行 | 部分通过 |
| REQ-03/AC-03.1–03.2 | WebRTC/HLS 浏览器与协议回退测试 | WHEP/HLS lease 授权单测/集成 stub 通过；原生 MediaMTX 和浏览器实际播放/回退待验证 | 部分通过 |
| REQ-04/AC-04.1–04.2 | 撤销、租约超时、重启恢复测试 | 隔离 PostgreSQL + MediaMTX stub 覆盖 session.revoked 和租约释放；进程崩溃重启及真实 MediaMTX kick 待验收 | 部分通过 |
| REQ-01/AC-01.3–01.4 | 仅 URL 写入/回显测试；旧密文迁移与失败安全测试 | 待本次实现 | 待执行 |

## 环境与执行入口

- `go test ./...`；服务内集成测试 `go test -tags=integration ./...`，需隔离 PostgreSQL、MediaMTX、RTSP fixture 和 auth/device stub。
- Atlas 实机和园区容量是单独系统验收，不能由模拟 RTSP fixture 替代。
