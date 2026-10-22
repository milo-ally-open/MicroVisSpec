# 云哨本机交付审查 · 2026-10-22

初始基线：AtomGit `dev`，`5f5504c`；收尾时整合远端新增的 `28384dd845391cfa252fc7c6732d892af5b6bd6b`（仅快进到现有上游提交，没有创建新提交）。初次交付按用户要求在 `E:/Yunshao-dev` 保留本地改动；用户随后授权再次审查，通过后提交并推送 dev。`E:/Yunshao` 的原有未提交工作未覆盖。

用户补充决定：先完成本机交付，手机与 CodeArts 实机证据后补。

## 本轮完成

| 交付 | 实际结果 | 证据 |
|---|---|---|
| 产品现状与定位 | 明确多行业 AIoT 平台定位；区分首发课堂场景、现有基础统计、模型产物、MCP/原生 App 规划 | README、PRD、展示页文案 |
| 工作台视觉 | 暖纸底色、墨色导航、森林绿、衬线标题、细边框和圆角按钮；复用上游单一主题，不叠加重复主题；本轮不修改业务接口或路由 | `web_pwa_client/evidence/delivery_20261022.json` |
| 推送调试工具 | 类型、地点、时间、等级、两测试账号；专用模拟设备与 MQTT 身份；规则配置 ACK、事件入库、截图和详情关联 | `src/tools/push-debug/` 与浏览器验收记录 |
| 投递状态 | 只读观察 event outbox / Push inbox / delivery；零订阅时明确没有实际投递，不声称手机收到 | 浏览器记录中的两账号 jobs |
| PPT | 12 页可编辑初版、逐页大纲、完整截图和演示用截图、配图清单、Windows 重建脚本 | `ppt/yunshao-platform-20261022-v3.pptx`、`ppt/README.md` |
| CodeArts 证据 | 已写真实证据补录清单，没有伪造使用截图 | `codearts_proof/images/README.md` |

## 验证结果

- Web：13/13 Vitest，通过 Vue/service worker 类型检查和生产构建；Web 镜像重新构建并运行。
- 调试工具：10/10 unittest；覆盖未知账号/类型、时间、未知字段、控制字符、幂等、重启、只读观察 SQL 及 Host/Origin/CSRF。
- 提交前复查：补齐 MQTT 首次失败后恢复、可选证据异常保留事件回执、非法 Origin 与非 ASCII CSRF 的回归；证据处理未完成时页面继续轮询，避免提前显示旧状态。当前部署曾短暂出现 MQTT CA 不一致，随后核对容器与主机证书链一致、TLS 校验通过；复核使用部署当前 `PUBLIC_ORIGIN`，未更改平台网络配置或关闭 TLS 验证。新浏览器结果见 `web_pwa_client/evidence/commit_review_20261022.json`。
- Push：`go test -count=1 -v -tags=integration ./...`，10/10、无跳过，使用独立 `push_debug_it_20261022` 数据库；不截断生产表。结果见 `push_service/evidence/local_integration_20261022.json`。
- 真实本机链路：两个账号分别创建模拟设备、接收规则并 ACK、通过 TLS MQTT 上报事件；event outbox 已投递、Push inbox 已接收；模拟证据上传可读；同 request_id 返回同一事件。
- 数据隔离：A/B 双向事件和证据读取拒绝，账号通过既有平台登录验证，不接受浏览器指定任意 account_id。
- 浏览器：Edge 1440/1024/390/360px，30 项工作台页面检查，无横向溢出及未捕获脚本错误；登录拒绝/成功/安全回跳和事件待处置→处理中→已解除通过。
- PPT：PowerPoint 16 原生生成并逐页导出检查；12 页、16:9、末页原生可编辑表；PPTX 包完整性与布局检查无 finding/warning。内置 Artifact Session 不可用，因此没有声称通过 Artifact Tool 导入验证。

## 本机访问和恢复

- 工作台：使用当前部署的 `PUBLIC_ORIGIN`；首轮为 `https://localhost`，提交前复核使用当前受信 HTTPS 入口。沿用原本机测试账号，凭据不写入交付文档。
- 推送调试：`http://127.0.0.1:9088`。只选已配置的测试账号；首次发送创建专用模拟设备。
- Docker 后端/前端及原生 MediaMTX 已恢复。机器重启后先启动 Docker Desktop，在 `project/src/services` 和 `project/src/clients/web` 分别执行 `docker compose up -d`；原生 MediaMTX 仍使用原有本机私有配置。
- 本轮工具启动命令（配置在 Git 忽略的 build 目录）：

```powershell
& 'E:/视频制作/yunshao-local-runtime/.browser-venv/Scripts/python.exe' 'E:/Yunshao-dev/project/src/tools/push-debug/server.py' --config 'E:/Yunshao-dev/build/delivery-runtime/config.local.json'
```

只运行一个工具实例，Ctrl+C 可停止其心跳。模拟设备、事件、截图和独立测试数据库保留供复核；未删除已有数据或卷。私有配置、设备凭据、CA 与测试环境文件均不纳入 Git。

## 验收边界与后续

1. 当前有效浏览器订阅为 0，本轮没有实际手机投递。需要在可达且证书受信的 HTTPS 环境登录目标账号、主动开启通知，再观察真实弹出与点击回跳。
2. CodeArts 实际使用截图按用户决定后补；不能以技能目录或系统截图代替。
3. 真实摄像头、完整 MediaMTX/WHEP/HLS 播放、Atlas 课堂精度、长期运行和最多 12 路并发继续独立验收。PPT 中板端数据引用既有合成流报告，非本轮重新实测。
4. MCP 和原生 App 尚未实现。既有离线/更新等未经本轮覆盖的 PWA 验收不因本机结果自动完成。
5. 自定义事件时间仍服从正式服务的规则历史校验；默认使用规则确认后的当前时间。工具仅支持现有三类课堂事件，不引入任意事件绕过入口。
