#include "adapters/agent_applied_cloud_rule_state.hpp"

#include "common/cloud_config.hpp"

#include <ctime>
#include <utility>

namespace yunshao::adapters {

AgentAppliedCloudRuleState::AgentAppliedCloudRuleState(
    EdgeAppConfig& config,
    application::ports::DurableAgentEventOutbox& outbox,
    std::filesystem::path config_directory,
    std::filesystem::path runtime_state_directory)
    : config_(config), outbox_(outbox), config_directory_(std::move(config_directory)),
      runtime_state_directory_(std::move(runtime_state_directory)) {}

std::optional<Json> AgentAppliedCloudRuleState::prepare_applied_ack(
    const std::string& previously_acknowledged_command_id) {
    const auto command = yunshao::read_json_file(config_directory_ / "alarm_rules.json")
        .value("cloud_command", Json::object());
    if (command.empty() ||
        command.value("command_id", "") == previously_acknowledged_command_id) return std::nullopt;

    bool found = false;
    for (const auto& stream : config_.streams) {
        if (!stream.enabled || !stream.enable_ai ||
            config_.require_model(stream.model_id).algorithm_key != command.value("algorithm_key", "")) continue;
        found = true;
        const auto state = yunshao::fresh_runtime_state(
            runtime_state_directory_ / (stream.stream_id + ".json"), stream.model_id);
        if (state.value("cloud_command_id", "") != command.value("command_id", "")) return std::nullopt;
    }
    if (!found) return std::nullopt;

    outbox_.replace_config(config_);
    return std::optional<Json>(std::in_place, command);
}

void AgentAppliedCloudRuleState::mark_ready(const Json& command) {
    yunshao::write_json_atomic(config_directory_ / "cloud-ready.json",
        Json{{"command_id", command.at("command_id")}, {"updated_at_unix", std::time(nullptr)}});
}

}  // namespace yunshao::adapters
