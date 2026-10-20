# 测试视频

`classroom_behavior_test.webm` 是课堂媒体链路测试素材，不是模型精度测试集，也不包含行为标注。

- 来源：Wikimedia Commons [Acrobatics at school, 2009.webm](https://commons.wikimedia.org/wiki/File:Acrobatics_at_school,_2009.webm)
- 作者：H3lla3x
- 授权： [CC0 1.0](https://creativecommons.org/publicdomain/zero/1.0/)
- 原始规格：42.433 秒，320×240，VP9/Opus；SHA-256：`7d64250e27455368a4bc8e5f95a14bb700149ab1d9d3abdb4d812daba571b68c3`

端到端测试时，Windows FFmpeg 将视频实时编码为 640×360、15 fps 的 H.264/UDP 输入给 Atlas；Atlas 输出经鉴权 RTSP 提供给 Windows `ffplay`。测试会保留 4:3 画面比例并在左右补黑边。低清晰度是原始素材本身的限制。
