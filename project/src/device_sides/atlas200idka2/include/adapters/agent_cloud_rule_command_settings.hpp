#pragma once

#include "application/ports/cloud_rule_command_settings.hpp"
#include "common/core.hpp"

#include <filesystem>
#include <mutex>
#include <string>

namespace yunshao::adapters {

class AgentCloudRuleCommandSettings final
    : public application::ports::CloudRuleCommandSettings {
public:
    AgentCloudRuleCommandSettings(EdgeAppConfig& config,
                                  ModelCatalog& models,
                                  std::mutex& config_mutex,
                                  std::filesystem::path config_directory,
                                  std::string device_id);

    void apply(const Json& command) override;

private:
    EdgeAppConfig& config_;
    ModelCatalog& models_;
    std::mutex& config_mutex_;
    std::filesystem::path config_directory_;
    std::string device_id_;
};

}  // namespace yunshao::adapters
