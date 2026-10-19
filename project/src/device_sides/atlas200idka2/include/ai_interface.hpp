#pragma once

#include "common/config.hpp"

#include <cstddef>
#include <string>
#include <vector>

namespace yunshao {

struct InferenceFrame {
    const void* nv21_device = nullptr;
    std::size_t bytes = 0;
    int width = 0;
    int height = 0;
    int stride_width = 0;
    int stride_height = 0;
    int source_width = 0;
    int source_height = 0;
    int target_width = 0;
    int target_height = 0;
    float letterbox_scale = 1.0F;
    int letterbox_left = 0;
    int letterbox_top = 0;
};

struct Detection {
    int class_id = -1;
    std::string label;
    float confidence = 0.0F;
    int x = 0;
    int y = 0;
    int width = 0;
    int height = 0;
};

class IAIModel {
public:
    virtual ~IAIModel() = default;
    virtual void initialize(const ModelProfile& profile, int npu_device_id,
                            const std::string& model_path) = 0;
    virtual void infer(const InferenceFrame& frame, double confidence_threshold,
                       double nms_threshold, std::vector<Detection>& output) = 0;
    virtual void deinitialize() noexcept = 0;
    virtual const char* name() const noexcept = 0;
};

using CreateAIModelFn = IAIModel* (*)();
using DestroyAIModelFn = void (*)(IAIModel*);

}  // namespace yunshao

extern "C" yunshao::IAIModel* CreateAIModel();
extern "C" void DestroyAIModel(yunshao::IAIModel* model);
