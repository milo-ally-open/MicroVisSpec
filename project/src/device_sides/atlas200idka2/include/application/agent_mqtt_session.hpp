#pragma once

#include "application/agent_mqtt_inbox.hpp"
#include "application/ports/mqtt_transport.hpp"

#include <atomic>
#include <string>

namespace yunshao::application {

class AgentMqttSession {
public:
    AgentMqttSession(ports::MqttTransport& transport,
                     ports::MqttConnectionSettings settings,
                     AgentMqttInbox& inbox,
                     std::string device_id);
    ~AgentMqttSession();

    AgentMqttSession(const AgentMqttSession&) = delete;
    AgentMqttSession& operator=(const AgentMqttSession&) = delete;

    void start();
    void stop();
    bool ready() const noexcept;
    std::string publish(const std::string& suffix,
                        const std::string& payload,
                        int qos);

private:
    ports::MqttTransport& transport_;
    ports::MqttConnectionSettings settings_;
    AgentMqttInbox& inbox_;
    std::string device_id_;
    std::atomic_bool ready_{false};

    std::string topic(const std::string& suffix) const;
    void handle_connected(int result);
    void handle_disconnected(int result);
};

}  // namespace yunshao::application
