#include "application/agent_cloud_command_processor.hpp"

#include <exception>

namespace yunshao::application {

AgentCloudCommandProcessor::AgentCloudCommandProcessor(
    AgentMqttInbox& inbox,
    ports::CloudRuleCommandSettings& settings,
    AgentCommandAcknowledger& acknowledger)
    : inbox_(inbox), settings_(settings), acknowledger_(acknowledger) {}

void AgentCloudCommandProcessor::process_pending() {
    for (const auto& command : inbox_.take_commands()) {
        try {
            settings_.apply(command);
        } catch (const std::exception& error) {
            acknowledger_.acknowledge(command, "rejected", error.what());
        }
    }
}

}  // namespace yunshao::application
