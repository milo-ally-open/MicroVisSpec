> model_train -> model_export -> model_convert -> model_val -> model_deployment

# model train 

模型训练过程的完整可复现代码的路径。


# model export 

模型导出为onnx通用格式的代码路径。(需要的时候需要有裁剪逻辑)

# model convert 

使用对应的工具链转换模型为执行格式的路径。

# model val 

模型端侧验证的路径。(含模型精度测试, 模型内存评估等)

# model deployment 

迁移工程目录, 内涵单个场景下的最小cpp demo工程(单个工程可以对应单个目录)

# 注意

- 对于级联方案: 模型训练完毕之后, 且后处理算法设计完毕之后, model_train目录下即可用一个面向对象封装完善的(工具类, 状态机等)单python脚本demo作为产出。
- 对于端到端方案: 可以完成任务的端到端模型训练完毕之后, model_train路径下同样可以用一个面向对象封装完善的(工具类等)单python脚本demo作为产出物。


