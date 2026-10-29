#include "adapters/ai_model_plugin.hpp"

#include <dlfcn.h>

#include <stdexcept>
#include <string>

namespace yunshao {
namespace {

void close_library(void*& library) noexcept {
    if (library != nullptr) {
        dlclose(library);
        library = nullptr;
    }
}

}  // namespace

AIModelPlugin::AIModelPlugin(const ModelProfile& profile, int npu_device_id,
                             const std::filesystem::path& model_path,
                             const std::filesystem::path& plugin_path) {
    if (!std::filesystem::is_regular_file(model_path)) {
        throw std::runtime_error("active model file is missing for model_id=" + profile.model_id);
    }
    if (!std::filesystem::is_regular_file(plugin_path)) {
        throw std::runtime_error("inference plugin file is missing: " + plugin_path.string());
    }

    library_ = dlopen(plugin_path.c_str(), RTLD_NOW | RTLD_LOCAL);
    if (library_ == nullptr) {
        const char* loader_error = dlerror();
        throw std::runtime_error(std::string("cannot load inference plugin: ") +
                                 (loader_error ? loader_error : "unknown error"));
    }

    try {
        dlerror();
        const auto api_version = reinterpret_cast<GetAIModelPluginApiVersionFn>(
            dlsym(library_, "GetAIModelPluginApiVersion"));
        const char* symbol_error = dlerror();
        if (symbol_error != nullptr || api_version == nullptr) {
            throw std::runtime_error(std::string("inference plugin has no GetAIModelPluginApiVersion symbol: ") +
                                     (symbol_error ? symbol_error : "unknown error"));
        }
        const int plugin_api_version = api_version();
        if (plugin_api_version != kAIModelPluginApiVersion) {
            throw std::runtime_error("inference plugin API version mismatch: expected " +
                                     std::to_string(kAIModelPluginApiVersion) + ", got " +
                                     std::to_string(plugin_api_version));
        }

        dlerror();
        const auto create = reinterpret_cast<CreateAIModelFn>(dlsym(library_, "CreateAIModel"));
        symbol_error = dlerror();
        if (symbol_error != nullptr || create == nullptr) {
            throw std::runtime_error(std::string("inference plugin has no CreateAIModel symbol: ") +
                                     (symbol_error ? symbol_error : "unknown error"));
        }

        dlerror();
        destroy_ = reinterpret_cast<DestroyAIModelFn>(dlsym(library_, "DestroyAIModel"));
        symbol_error = dlerror();
        if (symbol_error != nullptr || destroy_ == nullptr) {
            throw std::runtime_error(std::string("inference plugin has no DestroyAIModel symbol: ") +
                                     (symbol_error ? symbol_error : "unknown error"));
        }

        model_ = create();
        if (model_ == nullptr) throw std::runtime_error("inference plugin factory returned null");
        const char* plugin_model_id = model_->name();
        if (plugin_model_id == nullptr || profile.model_id != plugin_model_id) {
            throw std::runtime_error("inference plugin name does not match configured model_id: " + profile.model_id);
        }
        model_->initialize(profile, npu_device_id, model_path.string());
    } catch (...) {
        if (model_ != nullptr) {
            model_->deinitialize();
            destroy_(model_);
            model_ = nullptr;
        }
        destroy_ = nullptr;
        close_library(library_);
        throw;
    }
}

AIModelPlugin::~AIModelPlugin() {
    if (model_ != nullptr) {
        model_->deinitialize();
        destroy_(model_);
        model_ = nullptr;
    }
    close_library(library_);
}

void AIModelPlugin::infer(const InferenceFrame& frame, double confidence_threshold,
                          double nms_threshold, std::vector<Detection>& output) {
    if (model_ == nullptr) throw std::runtime_error("inference model plugin is not initialized");
    model_->infer(frame, confidence_threshold, nms_threshold, output);
}

const char* AIModelPlugin::name() const noexcept {
    return model_ == nullptr ? "unloaded" : model_->name();
}

}  // namespace yunshao
