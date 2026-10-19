#include "manager/ai_processor.hpp"

#include <dlfcn.h>

#include <stdexcept>

namespace yunshao {

AIProcessor::AIProcessor(const ModelProfile& profile, int npu_device_id,
                         const std::filesystem::path& model_path,
                         const std::filesystem::path& plugin_path)
    : profile_(profile) {
    if (!std::filesystem::is_regular_file(model_path)) {
        throw std::runtime_error("active model file is missing for model_id=" + profile_.model_id);
    }
    if (!std::filesystem::is_regular_file(plugin_path)) {
        throw std::runtime_error("inference plugin file is missing: " + plugin_path.string());
    }
    library_ = dlopen(plugin_path.c_str(), RTLD_NOW | RTLD_LOCAL);
    if (library_ == nullptr) throw std::runtime_error(std::string("cannot load inference plugin: ") + dlerror());
    dlerror();
    const auto create = reinterpret_cast<CreateAIModelFn>(dlsym(library_, "CreateAIModel"));
    const char* symbol_error = dlerror();
    if (symbol_error != nullptr || create == nullptr) {
        dlclose(library_);
        library_ = nullptr;
        throw std::runtime_error(std::string("inference plugin has no CreateAIModel symbol: ") + (symbol_error ? symbol_error : "unknown error"));
    }
    dlerror();
    destroy_ = reinterpret_cast<DestroyAIModelFn>(dlsym(library_, "DestroyAIModel"));
    symbol_error = dlerror();
    if (symbol_error != nullptr || destroy_ == nullptr) {
        dlclose(library_);
        library_ = nullptr;
        throw std::runtime_error(std::string("inference plugin has no DestroyAIModel symbol: ") + (symbol_error ? symbol_error : "unknown error"));
    }
    model_ = create();
    if (model_ == nullptr) {
        dlclose(library_);
        library_ = nullptr;
        throw std::runtime_error("inference plugin factory returned null");
    }
    try {
        const char* plugin_model_id = model_->name();
        if (plugin_model_id == nullptr || profile_.model_id != plugin_model_id) {
            throw std::runtime_error("inference plugin name does not match configured model_id: " + profile_.model_id);
        }
        model_->initialize(profile_, npu_device_id, model_path.string());
    } catch (...) {
        destroy_(model_);
        model_ = nullptr;
        dlclose(library_);
        library_ = nullptr;
        throw;
    }
}

AIProcessor::~AIProcessor() {
    std::lock_guard<std::mutex> lock(mutex_);
    if (model_ != nullptr) {
        model_->deinitialize();
        destroy_(model_);
        model_ = nullptr;
    }
    if (library_ != nullptr) {
        dlclose(library_);
        library_ = nullptr;
    }
}

void AIProcessor::infer(const InferenceFrame& frame, double confidence_threshold,
                        double nms_threshold, std::vector<Detection>& output) {
    std::lock_guard<std::mutex> lock(mutex_);
    if (model_ == nullptr) throw std::runtime_error("inference model is not initialized");
    model_->infer(frame, confidence_threshold, nms_threshold, output);
}

const char* AIProcessor::name() const noexcept { return model_ == nullptr ? "unloaded" : model_->name(); }

}  // namespace yunshao
