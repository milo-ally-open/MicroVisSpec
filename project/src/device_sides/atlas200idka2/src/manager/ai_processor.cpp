#include "manager/ai_processor.hpp"

#include <stdexcept>

namespace yunshao {

AIProcessor::AIProcessor(const ModelProfile& profile, int npu_device_id,
                         const std::filesystem::path& model_path,
                         const std::filesystem::path& plugin_path)
    : profile_(profile),
      plugin_(std::make_unique<AIModelPlugin>(profile_, npu_device_id, model_path, plugin_path)) {}

AIProcessor::~AIProcessor() {
    std::lock_guard<std::mutex> lock(mutex_);
    plugin_.reset();
}

void AIProcessor::infer(const InferenceFrame& frame, double confidence_threshold,
                        double nms_threshold, std::vector<Detection>& output) {
    std::lock_guard<std::mutex> lock(mutex_);
    if (!plugin_) throw std::runtime_error("inference model is not initialized");
    plugin_->infer(frame, confidence_threshold, nms_threshold, output);
}

const char* AIProcessor::name() const noexcept { return plugin_ ? plugin_->name() : "unloaded"; }

}  // namespace yunshao
