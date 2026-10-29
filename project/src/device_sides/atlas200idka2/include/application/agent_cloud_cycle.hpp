#pragma once

#include "application/agent_applied_rule_acknowledger.hpp"
#include "application/agent_cloud_command_processor.hpp"
#include "application/agent_mqtt_session.hpp"
#include "application/ports/agent_status_reader.hpp"
#include "application/ports/durable_agent_event_outbox.hpp"
#include "application/ports/evidence_uploader.hpp"

#include <string>

namespace yunshao::application {

class AgentCloudCycle {
public:
    AgentCloudCycle(std::string device_id,
                    AgentMqttSession& mqtt_session,
                    AgentCloudCommandProcessor& command_processor,
                    AgentAppliedRuleAcknowledger& applied_rule_acknowledger,
                    ports::AgentStatusReader& status_reader,
                    ports::DurableAgentEventOutbox& event_outbox,
                    ports::EvidenceUploader& evidence_uploader);

    void run_if_connected();

private:
    std::string device_id_;
    AgentMqttSession& mqtt_session_;
    AgentCloudCommandProcessor& command_processor_;
    AgentAppliedRuleAcknowledger& applied_rule_acknowledger_;
    ports::AgentStatusReader& status_reader_;
    ports::DurableAgentEventOutbox& event_outbox_;
    ports::EvidenceUploader& evidence_uploader_;
};

}  // namespace yunshao::application
