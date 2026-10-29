#include "application/agent_command_acknowledger.hpp"

#include "common/config.hpp"

#include <stdexcept>
#include <utility>

namespace yunshao::application {

AgentCommandAcknowledger::AgentCommandAcknowledger(
    std::string device_id,
    AgentMqttSession& mqtt_session,
    ports::EventMetadata& metadata)
    : device_id_(std::move(device_id)), mqtt_session_(mqtt_session), metadata_(metadata) {}

void AgentCommandAcknowledger::acknowledge(const nlohmann::json& command,
                                           const std::string& status,
                                           const std::string& reason) {
    const auto payload = nlohmann::json{{"schema_version", 1}, {"device_id", device_id_},
        {"command_id", command.value("command_id", "")}, {"sent_at", metadata_.utc_timestamp()},
        {"status", status}, {"reported_config_version", command.value("expected_version", 0)},
        {"failure_reason", reason}}.dump();
    if (!mqtt_session_.publish("command-acks", payload, 1).empty())
        throw std::runtime_error("cannot acknowledge cloud configuration");
}

}  // namespace yunshao::application
