#include "manager/config_service.hpp"

#include <utility>

namespace yunshao {

ConfigService::ConfigService(std::filesystem::path directory) : directory_(std::move(directory)) {
    config_ = EdgeAppConfig::load(directory_);
}

EdgeAppConfig ConfigService::snapshot() const {
    std::lock_guard<std::mutex> lock(mutex_);
    return config_;
}

void ConfigService::reload() {
    EdgeAppConfig candidate = EdgeAppConfig::load(directory_);
    std::lock_guard<std::mutex> lock(mutex_);
    config_ = std::move(candidate);
}

}  // namespace yunshao
