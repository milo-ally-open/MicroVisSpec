#pragma once

#include "application/agent_mqtt_session.hpp"
#include "application/ports/event_metadata.hpp"

#include <string>

namespace yunshao::application {

class AgentCommandAcknowledger {
public:
    AgentCommandAcknowledger(std::string device_id,
                             AgentMqttSession& mqtt_session,
                             ports::EventMetadata& metadata);

    void acknowledge(const nlohmann::json& command,
                     const std::string& status,
                     const std::string& reason = "");

private:
    std::string device_id_;
    AgentMqttSession& mqtt_session_;
    ports::EventMetadata& metadata_;
};

}  // namespace yunshao::application
