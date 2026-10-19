#include "manager/inference_manager.hpp"

#include <stdexcept>

namespace yunshao {

InferenceManager::InferenceManager(std::size_t max_loaded_models, int npu_device_id,
                                   std::filesystem::path model_root,
                                   std::filesystem::path plugin_directory)
    : max_loaded_models_(max_loaded_models), npu_device_id_(npu_device_id),
      model_root_(std::move(model_root)), plugin_directory_(std::move(plugin_directory)) {
    if (max_loaded_models_ == 0) throw std::invalid_argument("max_loaded_models must be positive");
}

void InferenceManager::load(const ModelProfile& profile) {
    if (!profile.enabled) throw std::invalid_argument("cannot load disabled model profile: " + profile.model_id);
    if (processors_.find(profile.model_id) != processors_.end()) return;
    if (processors_.size() >= max_loaded_models_) {
        throw std::runtime_error("max_loaded_models capacity reached");
    }
    const auto model_path = model_root_ / "active" / profile.model_id / profile.model_file;
    const auto plugin_path = plugin_directory_ / profile.plugin_library;
    auto processor = std::make_unique<AIProcessor>(profile, npu_device_id_, model_path, plugin_path);
    processors_.emplace(profile.model_id, std::move(processor));
}

void InferenceManager::infer(const std::string& model_id, const InferenceFrame& frame,
                             double confidence_threshold, double nms_threshold,
                             std::vector<Detection>& output) {
    const auto found = processors_.find(model_id);
    if (found == processors_.end()) throw std::invalid_argument("model is not loaded: " + model_id);
    found->second->infer(frame, confidence_threshold, nms_threshold, output);
}

bool InferenceManager::is_loaded(const std::string& model_id) const {
    return processors_.find(model_id) != processors_.end();
}

}  // namespace yunshao
