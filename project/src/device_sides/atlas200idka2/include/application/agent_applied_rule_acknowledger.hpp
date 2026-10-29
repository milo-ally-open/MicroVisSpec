#pragma once

#include "application/agent_command_acknowledger.hpp"
#include "application/ports/applied_cloud_rule_state.hpp"

#include <mutex>
#include <string>

namespace yunshao::application {

class AgentAppliedRuleAcknowledger {
public:
    AgentAppliedRuleAcknowledger(ports::AppliedCloudRuleState& state,
                                 AgentCommandAcknowledger& acknowledger,
                                 std::mutex& configuration_mutex);

    void acknowledge_if_ready();

private:
    ports::AppliedCloudRuleState& state_;
    AgentCommandAcknowledger& acknowledger_;
    std::mutex& configuration_mutex_;
    std::string acknowledged_command_id_;
};

}  // namespace yunshao::application
