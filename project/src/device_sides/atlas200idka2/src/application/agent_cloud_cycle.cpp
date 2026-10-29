#include "application/agent_cloud_cycle.hpp"

#include "common/cloud_config.hpp"

#include <utility>

namespace yunshao::application {

AgentCloudCycle::AgentCloudCycle(
    std::string device_id,
    AgentMqttSession& mqtt_session,
    AgentCloudCommandProcessor& command_processor,
    AgentAppliedRuleAcknowledger& applied_rule_acknowledger,
    ports::AgentStatusReader& status_reader,
    ports::DurableAgentEventOutbox& event_outbox,
    ports::EvidenceUploader& evidence_uploader)
    : device_id_(std::move(device_id)), mqtt_session_(mqtt_session),
      command_processor_(command_processor), applied_rule_acknowledger_(applied_rule_acknowledger),
      status_reader_(status_reader), event_outbox_(event_outbox),
      evidence_uploader_(evidence_uploader) {}

void AgentCloudCycle::run_if_connected() {
    if (!mqtt_session_.ready()) return;

    command_processor_.process_pending();
    applied_rule_acknowledger_.acknowledge_if_ready();
    mqtt_session_.publish("state", status_reader_.status().dump(), 1);
    for (const auto& event : event_outbox_.due_events()) {
        const std::string payload = yunshao::cloud_event_payload(event).dump();
        const std::string error = mqtt_session_.publish("events", payload, 1);
        event_outbox_.note_publish_attempt(event.at("event_id").get<std::string>(), error);
    }
    evidence_uploader_.upload_acknowledged(device_id_);
}

}  // namespace yunshao::application
