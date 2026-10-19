#include "ai_interface.hpp"

#include <dlfcn.h>

#include <iostream>
#include <stdexcept>
#include <string>

int main(int argc, char** argv) {
    if (argc != 2 && argc != 5) {
        throw std::invalid_argument("expected plugin path, optionally followed by --load model.om config-dir");
    }
    void* library = dlopen(argv[1], RTLD_NOW | RTLD_LOCAL);
    if (library == nullptr) throw std::runtime_error(std::string("cannot load model plugin: ") + dlerror());

    auto create = reinterpret_cast<yunshao::CreateAIModelFn>(dlsym(library, "CreateAIModel"));
    auto destroy = reinterpret_cast<yunshao::DestroyAIModelFn>(dlsym(library, "DestroyAIModel"));
    if (create == nullptr || destroy == nullptr) {
        dlclose(library);
        throw std::runtime_error("model plugin factory ABI is incomplete");
    }
    yunshao::IAIModel* model = create();
    try {
        if (model == nullptr || model->name() == nullptr ||
            std::string(model->name()) != "model_classroom_behavior_detection") {
            throw std::runtime_error("plugin name does not match model_classroom_behavior_detection");
        }
        if (argc == 5) {
            if (std::string(argv[2]) != "--load") {
                throw std::invalid_argument("expected --load before the model file");
            }
            const auto config = yunshao::EdgeAppConfig::load(argv[4]);
            const auto& profile = config.require_model(model->name());
            model->initialize(profile, config.npu_device_id, argv[3]);
            model->deinitialize();
        }
    } catch (...) {
        if (model != nullptr) {
            model->deinitialize();
            destroy(model);
        }
        dlclose(library);
        throw;
    }
    destroy(model);
    dlclose(library);
    std::cout << "model plugin identity matches its model_id\n";
}
