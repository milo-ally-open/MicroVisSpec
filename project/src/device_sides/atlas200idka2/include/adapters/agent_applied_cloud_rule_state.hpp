#pragma once

#include "application/ports/applied_cloud_rule_state.hpp"
#include "application/ports/durable_agent_event_outbox.hpp"
#include "common/config.hpp"

#include <filesystem>
#include <optional>

namespace yunshao::adapters {

class AgentAppliedCloudRuleState final
    : public application::ports::AppliedCloudRuleState {
public:
    AgentAppliedCloudRuleState(EdgeAppConfig& config,
                               application::ports::DurableAgentEventOutbox& outbox,
                               std::filesystem::path config_directory,
                               std::filesystem::path runtime_state_directory);

    std::optional<Json> prepare_applied_ack(
        const std::string& previously_acknowledged_command_id) override;
    void mark_ready(const Json& command) override;

private:
    EdgeAppConfig& config_;
    application::ports::DurableAgentEventOutbox& outbox_;
    std::filesystem::path config_directory_;
    std::filesystem::path runtime_state_directory_;
};

}  // namespace yunshao::adapters
