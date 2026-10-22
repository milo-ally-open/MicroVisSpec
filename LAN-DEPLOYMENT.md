# 当前局域网部署

本机平台地址：**https://172.20.10.4:19080/**。Atlas 地址为 `172.20.10.2`。

## 登录与客户端证书

- 演示账号：`lan_demo`。密码保存在本机 `.secret-lan-access.json`，该文件已被 Git 忽略。
- 当前 Windows 用户已信任演示 CA。其他电脑或手机首次使用时，复制 `.assistant/lan-20261022/yunshao-lan-ca.crt` 并导入受信任根证书。
- 其他 Windows 电脑导入公开 CA 的命令：`certutil -user -addstore Root <证书文件路径>`。无需分发私钥。
- 浏览器直接访问 HTTPS 地址。HTTP `18080` 会跳转到 HTTPS `19080`。

## 网络与运行位置

| 部件 | 当前运行位置 / 入口 |
| --- | --- |
| Web、认证、设备、事件、媒体、推送、PostgreSQL、Mosquitto | Docker Desktop；现有 Compose 项目 `yunshao-local`、`yunshao-local-web` |
| Compose 源码和私有配置 | `E:\Yunshao-dev\project\src\services`、`E:\Yunshao-dev\project\src\clients\web` |
| MediaMTX | `E:\视频制作\Yunshao-local-demo\project\src\services\media-service`，Windows 后台进程 |
| 网页和板端 HTTPS | `172.20.10.4:19080` → 本机 `127.0.0.1:443` |
| 板端 MQTT TLS | `172.20.10.4:18883` → 本机 `127.0.0.1:8883` |
| WebRTC 媒体 | 电脑 `172.20.10.4:8189/UDP`；播放器支持 HLS 回退 |
| Atlas RTSP 输出 | `rtsp://172.20.10.2:8554/classroom-demo` |
| Atlas 运行配置 | `/var/lib/yunshao/lan-config`，由 `yunshao` 服务账号维护 |
| Atlas 凭据、视频源环境变量 | `/etc/yunshao/agent.env`、`/etc/yunshao/classroom-runtime.env` |

本地密码、证书私钥和激活凭据不提交仓库。原配置备份在电脑的
`C:\Users\Rain\AppData\Local\Yunshao\lan-20261022\backups` 和板卡
`/root/yunshao-lan-backup-20261022`。

## 网络入口脚本

已启用 Windows 持久端口转发。重新配置时，在普通 PowerShell 执行以下整行，接受 Windows 管理员提示：

```powershell
Start-Process powershell -Verb RunAs -ArgumentList '-NoProfile -ExecutionPolicy Bypass -NoExit -File E:\Yunshao\Enable-Lan.ps1'
```

脚本只允许当前 `172.20.10.0/28` 网段访问网页，MQTT 仅允许 Atlas `172.20.10.2`。保持当前热点或路由器地址不变；更换网络后需同步更新证书 SAN、平台 Origin、MediaMTX、转发、防火墙和板端连接地址。

## 验证流程

注册/登录 → 设备激活 → 真实 MQTT 心跳 → 摄像头来源配置 → 测试视频进入 Atlas → NPU 推理和带框视频 → 网页下发一条演示规则 → 板端加载并回执 → 事件入库 → 同帧截图上传 → 查看、处置、统计。

课堂的举手、阅读和书写是行为标签。演示规则只用于验证告警链路，不表示这些行为本身异常。后台 Web Push 依赖浏览器推送服务，需与局域网网页、视频和事件功能分别验收。

## 重新启动与推送 DNS

Docker Desktop 启动后，在普通 PowerShell 执行 `& 'E:\Yunshao\Start-LanDemo.ps1'`，启动平台、MediaMTX 和测试视频。该脚本可以重复运行；Atlas 两个服务已设为随板卡启动。电脑休眠、Docker Desktop 退出或热点断开期间，局域网服务会中断。

本机 Clash DNS 会给微软推送域名返回 `198.18.x.x` Fake-IP；推送服务的公网地址校验会拒绝它。`Refresh-PushDns.ps1` 通过 HTTPS DNS 解析已登记的标准浏览器推送域名，并生成只用于 push-service 的 Compose override。启动脚本会刷新这份配置。新手机订阅了不同的推送域名后，可执行：

```powershell
& 'E:\Yunshao\Refresh-PushDns.ps1'
Set-Location E:\Yunshao-dev\project\src\services
docker compose -f docker-compose.yaml -f E:\Yunshao\.assistant\lan-20261022\lan-push.compose.yaml up -d --no-build --no-deps push-service
docker restart yunshao-local-web-web-1
```

这些 DNS 映射是当前联调用的公开地址快照。提供方变更地址时需重新刷新；长期部署应使用能返回真实公网地址的 DNS。当前板卡原有 NTP 未能同步；联调时已将板端时间对齐电脑，长期使用需保证双方持续校时。

HLS 已兼容 MediaMTX 的 cookie-check 重定向和 session 参数，鉴权仍检查当前登录会话与所属观看租约。相关上游行为见 [MediaMTX v1.21.2 HLS 实现](https://github.com/bluenviron/mediamtx/blob/v1.21.2/internal/servers/hls/http_server.go)。

## 本次验收结果

- 真实 Atlas OM v1.0.0 推理；电脑循环输入仓库课堂视频，网页通过 WebRTC 和 HLS 均播放 1280×720 带框画面，实测解码帧数持续增长。
- HTTPS 登录、设备激活、MQTT TLS 心跳、摄像头来源登记通过；边缘算法状态来自近期实际处理帧。
- 网页规则下发、运行时加载、应用回执通过；最终规则版本 v6 已在板端应用，三类演示告警均关闭，视频继续运行。
- 共生成 9 条真实模型触发的测试事件；已验证一条事件从待处置到处理中、已解除的完整记录，以及 1280×720 JPEG 证据读取与 24 小时保存期限。
- 未登录访问设备和截图均返回 401；账号内事件统计能查询真实事件与处置结果。
- 浏览器通知订阅已保存，6 条通知已被微软推送服务接受；自动化浏览器在 60 秒内未观察到通知展示，手机弹窗与通知点击回跳仍需在实际客户端确认。服务商接受不等同于用户已看到通知。
- Atlas CTest 4/4 通过（Release 中断言保持开启）；媒体服务 Go 测试通过；Web API 测试 12/12 通过；Web 类型检查、生产构建、PWA 构建及 Nginx 配置检查通过。

结构化记录：[局域网验收证据](project/docs/Specification/platform_deployment/evidence/lan_20261022.json)。本机截图保存在 `.assistant/lan-20261022/`。
