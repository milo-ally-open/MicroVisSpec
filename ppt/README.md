# 云哨产品汇报初版

产品定位：面向多行业的 AIoT 边缘视频智能平台。首发用教室行为验证平台链路，不能将场景扩展规划作为已经落地的能力。

## 文件

- `yunshao-platform-20261022-v3.pptx`：12 页可编辑汇报初版；文字与末页状态表为原生对象，系统截图使用嵌入图片。最终截图已基于整合远端 `28384dd` 后的单一主题重新采集。
- `outline.json`：逐页大纲、正文、配图与来源；可继续修改内容。
- `SCREENSHOTS.md`：素材用途、真实系统截图与场景示意的区分。
- `assets/screenshots/`：桌面与窄屏的完整系统截图。
- `assets/slides/`：直接从运行中的浏览器重新拍摄的演示配图，未通过图片生成工具改动系统状态。
- `build_deck.ps1`：Windows PowerPoint 自动化重建入口；不覆盖已有文件，重建时传入新的 `-OutputPath`。

## 大纲

1. 产品与定位
2. 应用场景
3. 云边端架构
4. 当前交付规模
5. 运行概览
6. 设备与规则
7. 边缘推理与待验收边界
8. 事件查询
9. 事件证据与处置
10. 基础统计
11. 推送调试
12. 验证结果与后续计划

## 能力与证据

本轮截图来自本机 HTTPS 正式构建，后端为真实 PostgreSQL、MQTT/TLS 和五个业务服务。设备、事件与证据图片是明确标注的模拟数据，不代表 Atlas 推理。前端 13 项测试、工具 10 项单测和 Push 10 项隔离数据库测试通过；Edge 30 项页面/视口检查、账号隔离、幂等和事件处置验证见 `project/docs/Specification/web_pwa_client/evidence/delivery_20261022.json`。

板端数据来自仓库已有报告，并在幻灯片说明未于本轮重新测量。手机实际通知、CodeArts 操作截图按用户决定后补；真实摄像头、完整媒体链路与长期运行继续独立验收。

内置 Artifact Session 两次返回 host request failed，且未提供本地 Artifact Tool 依赖。因此本机交付使用已安装 PowerPoint 16 原生创建、逐页 PNG 导出与检查，并运行可用的 PPTX 包/布局检查；不声称通过 Artifact Tool 导入验证。
