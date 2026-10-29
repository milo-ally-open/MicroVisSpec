#pragma once

#include "adapters/ai_model_plugin.hpp"

#include <filesystem>
#include <memory>
#include <mutex>
#include <string>
#include <vector>

namespace yunshao {

class AIProcessor {
public:
    AIProcessor(const ModelProfile& profile, int npu_device_id,
                const std::filesystem::path& model_path,
                const std::filesystem::path& plugin_path);
    ~AIProcessor();
    AIProcessor(const AIProcessor&) = delete;
    AIProcessor& operator=(const AIProcessor&) = delete;

    void infer(const InferenceFrame& frame, double confidence_threshold,
               double nms_threshold, std::vector<Detection>& output);
    const ModelProfile& profile() const noexcept { return profile_; }
    const char* name() const noexcept;

private:
    ModelProfile profile_;
    std::unique_ptr<AIModelPlugin> plugin_;
    mutable std::mutex mutex_;
};

}  // namespace yunshao
