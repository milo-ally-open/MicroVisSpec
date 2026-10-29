#pragma once

#include "ai_interface.hpp"

#include <filesystem>
#include <vector>

namespace yunshao {

// Owns the platform dynamic-library boundary for one algorithm plugin.
class AIModelPlugin {
public:
    AIModelPlugin(const ModelProfile& profile, int npu_device_id,
                  const std::filesystem::path& model_path,
                  const std::filesystem::path& plugin_path);
    ~AIModelPlugin();

    AIModelPlugin(const AIModelPlugin&) = delete;
    AIModelPlugin& operator=(const AIModelPlugin&) = delete;

    void infer(const InferenceFrame& frame, double confidence_threshold,
               double nms_threshold, std::vector<Detection>& output);
    const char* name() const noexcept;

private:
    void* library_ = nullptr;
    DestroyAIModelFn destroy_ = nullptr;
    IAIModel* model_ = nullptr;
};

}  // namespace yunshao
