#include "application/agent_mqtt_inbox.hpp"

#include <iostream>
#include <utility>

namespace yunshao::application {

AgentMqttInbox::AgentMqttInbox(ports::DurableAgentEventOutbox& event_outbox,
                               std::string command_topic)
    : event_outbox_(event_outbox), command_topic_(std::move(command_topic)) {}

void AgentMqttInbox::receive(const std::string& topic, const std::string& payload) {
    if (payload.empty()) return;
    try {
        if (topic == command_topic_) {
            if (payload.size() > 65536) return;
            const auto command = nlohmann::json::parse(payload);
            std::lock_guard<std::mutex> lock(mutex_);
            if (commands_.size() < 16) commands_.push_back(command);
            return;
        }
        std::string error;
        if (!event_outbox_.apply_ack(nlohmann::json::parse(payload), &error)) {
            std::cerr << "agent ignored application acknowledgement: " << error << '\n';
        }
    } catch (...) {
        std::cerr << "agent ignored malformed application acknowledgement\n";
    }
}

std::vector<nlohmann::json> AgentMqttInbox::take_commands() {
    std::lock_guard<std::mutex> lock(mutex_);
    std::vector<nlohmann::json> pending;
    pending.swap(commands_);
    return pending;
}

}  // namespace yunshao::application
