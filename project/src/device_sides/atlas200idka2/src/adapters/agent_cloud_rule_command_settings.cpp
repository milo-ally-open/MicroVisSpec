#include "adapters/agent_cloud_rule_command_settings.hpp"

#include "common/cloud_config.hpp"

#include <stdexcept>
#include <utility>

namespace yunshao::adapters {

AgentCloudRuleCommandSettings::AgentCloudRuleCommandSettings(
    EdgeAppConfig& config,
    ModelCatalog& models,
    std::mutex& config_mutex,
    std::filesystem::path config_directory,
    std::string device_id)
    : config_(config), models_(models), config_mutex_(config_mutex),
      config_directory_(std::move(config_directory)), device_id_(std::move(device_id)) {}

void AgentCloudRuleCommandSettings::apply(const Json& command) {
    std::lock_guard<std::mutex> lock(config_mutex_);
    bool supported = false;
    for (const auto& profile : config_.models) {
        if (!profile.enabled || profile.algorithm_key != command.value("algorithm_key", "")) continue;
        const auto model = models_.status(profile.model_id);
        if (model.value("model_state", "") != "installed")
            throw std::invalid_argument("model is not installed");
        yunshao::apply_cloud_rules(config_directory_, device_id_, profile.model_id,
            model.value("model_version", ""), command);
        supported = true;
        break;
    }
    if (!supported) throw std::invalid_argument("algorithm is not installed");
    config_ = EdgeAppConfig::load(config_directory_);
}

}  // namespace yunshao::adapters
