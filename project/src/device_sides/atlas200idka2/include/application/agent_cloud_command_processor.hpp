#pragma once

#include "application/agent_command_acknowledger.hpp"
#include "application/agent_mqtt_inbox.hpp"
#include "application/ports/cloud_rule_command_settings.hpp"

namespace yunshao::application {

class AgentCloudCommandProcessor {
public:
    AgentCloudCommandProcessor(AgentMqttInbox& inbox,
                               ports::CloudRuleCommandSettings& settings,
                               AgentCommandAcknowledger& acknowledger);

    void process_pending();

private:
    AgentMqttInbox& inbox_;
    ports::CloudRuleCommandSettings& settings_;
    AgentCommandAcknowledger& acknowledger_;
};

}  // namespace yunshao::application
