# 云哨本机推送联调

独立的 Python 后端 + 静态表单。选择显式配置的测试账号，填写事件类型、地点、时间和等级，产生真实平台事件并经过现有 Push 链路。模拟设备名称、协议版本及页面均标明测试性质，不声称执行 Atlas 推理。

## 启动

使用 Python 3.10+ 创建虚拟环境，安装 `requirements.txt`。将 `config.example.json` 复制为 `config.local.json`，填写已存在的测试账号和本机平台 CA；不要使用生产账号。然后在本目录运行：

```powershell
python -m venv .venv
.venv/Scripts/python.exe -m pip install -r requirements.txt
.venv/Scripts/python.exe server.py --config config.local.json
```

访问 `http://127.0.0.1:9088`。服务只监听本机，禁止通过公网代理开放。平台自身仍使用 HTTPS；工具不关闭 TLS 验证，也不修改 Windows 证书信任。

配置可放在仓库外。相对 CA 和证据路径建议改成绝对路径。每个 profile 必须有不同的 `id`、清晰的 `label` 及测试用户名/密码。账号需预先注册。首次发送会在该账号下登记、激活专用模拟设备和摄像头；后续复用其身份和规则版本，持续发送明确为 simulated 的心跳。地点写入这台专用模拟设备，不修改其他设备。

## 投递观察（可选）

本机 Docker PostgreSQL 可在私有配置中增加：

```json
"observer": {
  "container": "your-local-postgres-container",
  "user": "your-local-read-authorized-role",
  "database": "yunshao"
}
```

该角色需读取 `event.events`、`event.outbox`、`push.inbox`、`push.deliveries`、`push.subscriptions`。观察器使用 `BEGIN READ ONLY`，只查询当前工具任务产生的已解析 UUID，并返回有效订阅数量、是否接入及投递状态，不返回 endpoint 或密钥。不配置时仍可生成事件和打开详情，但显示投递状态“未观察”。Docker socket 权限只用于本机操作员，绝不能把此工具作为生产服务。

可用绝对路径 `evidence_file` 指定一张**明确标注模拟证据**的 PNG；上传失败单独展示，不冒充模型截图。不要使用真实个人影像作为调试素材。

可选证据文件缺失、过大或网络上传失败时，已入库的事件 ID、详情地址和投递状态仍保留，不重新发布事件。MQTT 首次连接失败会释放客户端，后续新请求可重新连接。

## 如何验收

1. 目标账号在 HTTPS 云哨工作台的“账号与通知”主动开启通知。
2. 表单选择该测试账号，提交一条事件；核对规则 ACK、事件入库和详情地址。
3. provider `accepted` 只表示推送服务商接受，不表示手机弹出或已读。需要在接收设备上观察通知，点击后核对同一事件和重新鉴权。
4. 使用另一个账号打开事件地址，应被拒绝；另一个账号的模拟设备与订阅独立。
5. 网络重试沿用相同 request_id，不重复创建事件；“准备新事件”才生成新的 ID。重启不自动重发未确认任务。

表单仅支持现有 event-service 的举手/阅读/书写规则事件；扩展其他行业类型需先扩展正式事件合同。页面不提供绕过规则的通用通知入口。

发生时间默认取规则确认之后的当前时间，避免首次启用规则前的时间被 `rule_config_not_active_at_event_time` 拒绝。自定义时间必须处于规则生效之后且在最近 24 小时内；更早的历史事件按正式服务规则拒绝。选择不同类别/等级会更新这台专用模拟设备的规则，重复相同规则则复用已应用版本。

## 停止、清理和私有数据

Ctrl+C 停止工具及 MQTT 心跳；新建的测试事件与模拟设备留在测试账号下，便于追溯。不会删除数据库、卷或用户设备。运行状态在配置文件旁 `.state/`，含设备凭据，应和配置一起保存在私有位置。仓库内默认配置与 `.state/` 已忽略，不要强制加入 Git。

单元测试：`python -m unittest discover -s . -p 'test_*.py'`。需求和证据追踪见 `CONTRACT.md`。手机和 CodeArts 实机证据按本轮用户决定后补。
