#pragma once

#include "application/ports/durable_agent_event_outbox.hpp"

#include <mutex>
#include <string>
#include <vector>

namespace yunshao::application {

class AgentMqttInbox {
public:
    AgentMqttInbox(ports::DurableAgentEventOutbox& event_outbox,
                   std::string command_topic);

    void receive(const std::string& topic, const std::string& payload);
    std::vector<nlohmann::json> take_commands();

private:
    ports::DurableAgentEventOutbox& event_outbox_;
    std::string command_topic_;
    std::mutex mutex_;
    std::vector<nlohmann::json> commands_;
};

}  // namespace yunshao::application
