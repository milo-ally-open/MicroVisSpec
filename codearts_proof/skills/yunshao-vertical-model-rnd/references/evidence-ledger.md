# 证据台账模板

放在 `project/preparation/model_<scenario>/model_deployment/run_records/<run-id>.md`。
每过一道门就补一次，不要等最后一次性回忆填写。**没有的一律写「待补」，不写空、不写推测值。**

```markdown
# <场景> 研发证据台账

- run-id：<scenario>_<yyyymmdd>
- 执行日期：YYYY-MM-DD
- 执行环境：OS / GPU / CUDA / Python / torch / ultralytics
- 板端：Atlas 200I DK A2（Ascend 310B4）/ CANN <版本> / runtime <版本>
- Git：branch <分支> @ <commit>
- 结论：G0–G6 <PASS / 部分待补>

## G0 立项

| 项 | 值 |
|---|---|
| 目标物体 | |
| 类别序 | |
| 数据源 | provider / project / version / license |
| 场景骨架 | 目录清单 |

## G1 数据

| 项 | 值 |
|---|---|
| 原始数量 / 重复数 / 清洗后 | |
| 划分（train/valid/test） | |
| data.yaml SHA-256 | |
| 标签与图像配对检查 | |
| 判定 | PASS / 待补 |

## G2 训练

| 项 | 值 |
|---|---|
| run-name | |
| 超参 | 基座 / epoch / batch / device / workers / seed / patience |
| best / last 权重 SHA-256 | |
| valid 指标 | 逐类 P / R / mAP50 / mAP50-95 |
| test 留出确认 | 未参与调参 |

## G3 ONNX

| 项 | 值 |
|---|---|
| 来源 checkpoint | |
| 输入形状 / opset / 类别序 | |
| ONNX SHA-256 | |
| export_manifest.json | 已生成 / 五要素核对 |

## G4 ATC

| 项 | 值 |
|---|---|
| 板端 run 目录 | |
| 上板 ONNX 哈希核对 | |
| ATC 命令与日志摘要 | |
| OM 大小与 SHA-256 | |
| SHA256SUMS 三件复验 | |

## G5 双端评估

| 项 | 值 |
|---|---|
| 共同条件 | 数据快照 / data.yaml / test 划分 / conf / IoU / metric-mode |
| ONNX 指标 | mAP50 / mAP50-95 / 逐类 AP50 |
| OM 指标 | 同上 |
| parity 降幅 | mAP50-95 / 逐类 AP50 / 判定 |
| 性能 | 开发机 CPU FPS / 板端 NPU FPS / 延迟分位 |
| 内存 | 进程峰值 RSS / npu-smi 前后 |
| 绝对质量门槛 | 项目阈值或「未指定」 |

## G6 demo

| 项 | 值 |
|---|---|
| 输入素材 | 真实流 / 合成烟测（来源与生成方式） |
| 运行统计 | 退出码 / 帧数 / FPS / 峰值 RSS |
| 输出 SHA-256 | |
| ffprobe 校验 | 编码 / 帧数 / 可解码 |
| 结论边界 | 软件链路烟测 / 真实场景验收 |

## 附录

- 命令与退出码清单
- 失败与重试记录（保留，不删）
- 截图索引（如 `codearts_proof/images/`，注意该目录对 CodeArts 实机证据另有约定）
```

## 填写规则

1. 先写表头再开工，边跑边填。
2. 哈希一律完整 64 位，不截断。
3. 数值带单位；FPS 注明测量位置（开发机 / 板端）。
4. 失败也是一条证据：写清失败现象、判定阶段、修复动作、重试 run-name。
5. 结论栏只写实测结论，不写预期结论。
