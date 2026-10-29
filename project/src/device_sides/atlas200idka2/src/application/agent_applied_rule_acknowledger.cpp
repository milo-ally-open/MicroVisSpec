#include "application/agent_applied_rule_acknowledger.hpp"

#include <utility>

namespace yunshao::application {

AgentAppliedRuleAcknowledger::AgentAppliedRuleAcknowledger(
    ports::AppliedCloudRuleState& state,
    AgentCommandAcknowledger& acknowledger,
    std::mutex& configuration_mutex)
    : state_(state), acknowledger_(acknowledger), configuration_mutex_(configuration_mutex) {}

void AgentAppliedRuleAcknowledger::acknowledge_if_ready() {
    std::lock_guard<std::mutex> lock(configuration_mutex_);
    const auto command = state_.prepare_applied_ack(acknowledged_command_id_);
    if (!command) return;

    acknowledger_.acknowledge(*command, "applied");
    state_.mark_ready(*command);
    acknowledged_command_id_ = command->at("command_id").get<std::string>();
}

}  // namespace yunshao::application
