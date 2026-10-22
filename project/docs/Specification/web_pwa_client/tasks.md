# Web 与 PWA 客户端实施任务

> 关联合同：[spec.md](spec.md)；设计：[design.md](design.md)。T 编号仅在本规格组内唯一。Windows 开发机已提供 Node.js 环境。

## 任务清单

- [x] **T-14 本机交付与业务回归** — 整合 `28384dd` 的单一主题，覆盖 AC-06.1、AC-06.5、AC-06.6 的业务页和认证页范围。13/13 Vitest、类型检查、生产构建通过；Edge 1440/1024/390/360px 的 30 项工作台检查无横向溢出和未捕获脚本错误。两个账号模拟事件接入真实 MQTT/Push inbox；双向隔离、幂等与浏览器处置通过。完整截图见 `ppt/assets/screenshots/`；证据见 [delivery_20261022.json](evidence/delivery_20261022.json)。离线/更新完整生命周期、真实媒体、手机和 Atlas 不由该结果证明。

- [ ] **T-01 应用壳、路由与安全 API 基础** — `src/App.vue`、`src/router/`、`src/api/`、认证状态；公开/受保护路由、Cookie/CSRF、错误映射、401 清理/安全回跳；覆盖 REQ-01 / AC-01.1、AC-01.2、AC-01.3；依赖 auth-service。
- [ ] **T-02 设备管理和课堂规则配置页** — 设备登记/编辑/停用、摄像头/算法状态、期望/实报版本、三类告警默认关闭、启用规则参数校验及版本 ACK 状态；覆盖 REQ-02 / AC-02.1、AC-02.3、AC-02.4；依赖 device-service。
- [ ] **T-03 教室实时监控页** — MediaMTX WebRTC/HLS 播放、租约心跳/释放、来源/设备/模型/容量错误提示、桌面网格和窄屏非监控导航；覆盖 REQ-02 / AC-02.2、AC-02.4；依赖 media-service、边缘视频输出。
- [ ] **T-04 异常事件查询、详情和处置** — 稳定游标筛选、行为事件显示、证据状态、状态流转和 409 冲突刷新；普通检测不作事件；覆盖 REQ-03 / AC-03.1、AC-03.2、AC-03.3、AC-03.4；依赖 event-service。
- [ ] **T-05 概览与基础统计** — 设备/事件卡片、today/7d/30d、趋势/等级/类型/位置/处置分布、分别显示数据更新时间和空状态；覆盖 REQ-04 / AC-04.1；依赖 device/event-service。
- [ ] **T-06 Push 设置、通知与 PWA 生命周期** — 用户手势订阅、VAPID 公钥、权限状态、service worker `showNotification`/click route、应用壳离线、更新提示和不缓存敏感数据；覆盖 REQ-04 / AC-04.2、AC-04.3、REQ-05 / AC-05.1、AC-05.2；依赖 push-service、HTTPS/VAPID。
- [ ] **T-07 前端组件/API/PWA 集成验证** — 建立并锁定 Vue-compatible 测试依赖及 `npm test` 脚本；覆盖路由认证、CSRF、错误/空态、规则默认关闭、事件处置、Push 点击回跳、缓存策略和桌面/窄屏浏览器流程；覆盖 REQ-01、REQ-02、REQ-03、REQ-04、REQ-05 / AC-01.1、AC-01.2、AC-01.3、AC-02.1、AC-02.2、AC-02.3、AC-02.4、AC-03.1、AC-03.2、AC-03.3、AC-03.4、AC-04.1、AC-04.2、AC-04.3、AC-05.1、AC-05.2；依赖 T-01–T-06。
- [ ] **T-10 设备详情 RTSP 来源表单精简** — 加载 media-service status 中已保存的 RTSP URL；仅保留 URL 输入、校验和保存状态，移除来源用户名/密码字段；验证凭据/非法 URL 拒绝、跨账号错误展示和来源正常应用；覆盖 REQ-02 / AC-02.5；依赖 media-service T-01/T-02 与 device-service T-07。
- [ ] **T-11 Web/PWA 品牌统一** — 使用仓库项目 Logo 替换品牌区占位标记，并在桌面/手机工作台及登录/注册页显示完整座右铭；覆盖 REQ-06 / AC-06.5。
- [ ] **T-12 Web/PWA 展品视觉统一** — 以 `readme/yunshao-exhibit.html` 为视觉参照，统一暖纸色、墨色导航、森林绿强调色和标题层级；检查业务页、认证页、离线提示及更新提示，保持页面行为不变；覆盖 REQ-06 / AC-06.6。
- [ ] **T-13 Web/PWA 局域网入口配置** — Web Compose 绑定 Wi‑Fi 地址并以 override 保留 Atlas 直连接口；HTTPS 端口跳转、证书 SAN、PUBLIC_ORIGIN、MediaMTX 来源/WebRTC candidates 与部署防火墙说明保持一致；覆盖 REQ-07 / AC-07.1–07.3。

## 验收记录

- [ ] **T-12 Web/PWA 展品视觉统一** — 样式实现已完成；`npm run build`（含 Vue 与 service worker 类型检查）通过，桌面登录页目测符合暖纸色、墨色按钮与衬线标题层级。业务页、注册页、离线/更新提示及窄屏尚未逐页目测，待补验收后关闭；路由、API、数据与交互逻辑未改。
- [ ] **T-13 Web/PWA 局域網入口配置** — Web 與認證容器已啟動；`curl.exe` 從本機 Wi‑Fi 地址 `172.50.53.131` 取得 HTTPS 首頁 `200 OK`，HTTP `18081` 回應 `308` 並跳轉至 `19080`；TLS 使用現有開發 CA 驗證。Windows 防火牆規則只允許 `172.50.53.0/24` 存取 Web 入口；本機 Windows 系統代理繞過已加入此 Wi‑Fi IP。其他客戶端代理例外／匯入 CA、Atlas 激活、媒體播放、客戶端隔離及外部網段拒絕存取尚未驗證；`media-service` 目前仍為 unhealthy。

- [x] **T-09 工作台视觉与窄屏检查** — 统一共享样式、导航/图标、概览和登录注册；验证密码显示切换、真实日期及统计范围标题；覆盖 REQ-06 / AC-06.1、AC-06.2、AC-06.3、AC-06.4。现有单测 10/10、类型检查及生产构建通过；Edge 在 1440/1024/390/360px 下完成 30 项页面检查，无横向溢出与未捕获脚本错误；密码切换、上海日期/问候、统计范围标题、360px 设备登记表单及事件筛选共 5 项交互检查通过，登录流程 4 项回归通过。已部署到本机 localhost；脱敏记录见 [视觉检查证据](evidence/frontend_visual_20261020.json)。窄屏检查使用桌面 Edge 视口模拟，真实移动设备、Atlas、实时视频与 Push/PWA 全链路不属于本次视觉验收范围。

- [x] **T-08 登录失败提示修正** — 修复登录 401 被误显示为会话失效的问题；覆盖 AC-01.4，并回归 AC-01.1。10/10 Vitest、TypeScript/生产构建通过；Edge 在真实 localhost 后端验证错误密码提示、固定测试凭据登录、刷新后保持会话，以及既有待处置事件/授权截图访问。测试密码仅保存在本机私有文件，不入库；登录和账号数据隔离继续使用原认证接口。

| REQ/AC | 检查方法 | 当前证据 | 状态 |
|---|---|---|---|
| REQ-06/AC-06.6 | 生产构建；桌面/窄屏浏览业务页、认证页、离线提示与更新提示；检查无远程字体及行为回归 | 生产构建与 Vue/service worker 类型检查通过；本机 Vite 登录页已目测暖纸底、墨色主按钮、森林绿品牌强调和衬线标题。业务页、注册页、窄屏、离线/更新提示仍待目测；样式 diff 未改页面结构、路由、API 或处理逻辑 | 部分通过 |
| REQ-07/AC-07.1–07.3 | Compose 静态校验；HTTPS/Origin/证书 SAN 审查；团队 Wi‑Fi 浏览器、媒体、Atlas 激活与 MQTT；外部网段拒绝访问 | Compose 靜態校驗通過；HTTPS 首頁 `200 OK`、HTTP 跳轉 `308`，Wi‑Fi 防火牆來源限制為 `172.50.53.0/24`；憑證 SAN 涵蓋 Wi‑Fi、Atlas、localhost。其他客戶端信任 CA、Atlas/媒體端到端及客戶端隔離尚未驗證；media-service unhealthy | 部分通過 |
| REQ-01/AC-01.1–01.3 | 组件/API 认证测试与浏览器账号流程 | 9 个 Vitest 用例通过，覆盖 CSRF、401 状态清理、安全回跳和错误映射；生产 TypeScript 检查通过；完整浏览器账号流程待联调 | 部分通过 |
| REQ-02/AC-02.1–02.4 | 设备状态/规则 API 测试、媒体真实浏览器联调 | 三类规则默认关闭/阈值校验用例通过；生产构建成功；设备/媒体 API 与浏览器播放待后端栈联调 | 部分通过 |
| REQ-02/AC-02.5 | 设备详情 URL 回显/提交、URL 校验、账号隔离集成和浏览器验证 | 待本次实现 | 待执行 |
| REQ-06/AC-06.5 | 桌面及手机工作台、登录/注册页品牌区检查 | 代码已更新；本次未启动浏览器或运行 UI 检查 | 待验收 |
| REQ-03/AC-03.1–03.4 | 列表筛选、截图状态、处置流程 UI 测试 | 页面及生产 bundle 构建成功；事件服务端分页、证据状态和并发处置的浏览器流程尚未运行 | 部分通过 |
| REQ-04/AC-04.1–04.3、REQ-05/AC-05.1–05.2 | 统计/push/service worker/离线浏览器测试 | Vitest 覆盖通知安全路由和 PWA 缓存 denylist；injectManifest 生产构建成功；真实 Push、离线刷新与更新提示浏览器验收待执行 | 部分通过 |

## 环境与执行入口

- 最近执行：隔离副本 `npm ci`、`npm run test`（9/9）、`npm run build`（含 Vue 与 service worker type-check）均通过；Vite 开发进程占用工作区原生 Rolldown 文件，故验证副本未覆盖工作区依赖目录。
- 当前 `package.json` 已定义 `test`；2026-10-20 在 Docker Web 构建环境执行 `npm test`，10/10 通过，含登录失败文案回归；生产构建包含 Vue/service worker TypeScript 检查。本次真实浏览器登录相关 4 项检查通过；详细本机记录为仓库外 `E:/视频制作/yunshao-local-runtime/login-smoke-report.json`。
- 真实 Push 仍需 HTTPS、VAPID 和支持的浏览器；mock/API 测试不替代浏览器推送验收。
