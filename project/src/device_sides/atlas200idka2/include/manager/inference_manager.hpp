#pragma once

#include "manager/ai_processor.hpp"

#include <filesystem>
#include <map>
#include <memory>
#include <string>

namespace yunshao {

class InferenceManager {
public:
    InferenceManager(std::size_t max_loaded_models, int npu_device_id,
                     std::filesystem::path model_root,
                     std::filesystem::path plugin_directory);

    void load(const ModelProfile& profile);
    void infer(const std::string& model_id, const InferenceFrame& frame,
               double confidence_threshold, double nms_threshold,
               std::vector<Detection>& output);
    bool is_loaded(const std::string& model_id) const;
    std::size_t loaded_model_count() const noexcept { return processors_.size(); }

private:
    std::size_t max_loaded_models_;
    int npu_device_id_;
    std::filesystem::path model_root_;
    std::filesystem::path plugin_directory_;
    std::map<std::string, std::unique_ptr<AIProcessor>> processors_;
};

}  // namespace yunshao
