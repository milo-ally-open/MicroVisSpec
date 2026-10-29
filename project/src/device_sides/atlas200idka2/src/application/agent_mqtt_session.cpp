#include "application/agent_mqtt_session.hpp"

#include <iostream>
#include <utility>

namespace yunshao::application {

AgentMqttSession::AgentMqttSession(ports::MqttTransport& transport,
                                   ports::MqttConnectionSettings settings,
                                   AgentMqttInbox& inbox,
                                   std::string device_id)
    : transport_(transport), settings_(std::move(settings)), inbox_(inbox),
      device_id_(std::move(device_id)) {}

AgentMqttSession::~AgentMqttSession() {
    stop();
}

void AgentMqttSession::start() {
    if (settings_.host.empty()) return;
    ports::MqttCallbacks callbacks;
    callbacks.on_connected = [this](int result) { handle_connected(result); };
    callbacks.on_disconnected = [this](int result) { handle_disconnected(result); };
    callbacks.on_message = [this](const std::string& received_topic,
                                  const std::string& payload) {
        inbox_.receive(received_topic, payload);
    };
    transport_.start(settings_, std::move(callbacks));
    std::cout << "agent MQTT client started for device " << device_id_ << '\n';
}

void AgentMqttSession::stop() {
    ready_.store(false);
    transport_.stop();
}

bool AgentMqttSession::ready() const noexcept {
    return ready_.load();
}

std::string AgentMqttSession::publish(const std::string& suffix,
                                      const std::string& payload,
                                      int qos) {
    auto error = transport_.publish(topic(suffix), payload, qos);
    if (!error.empty()) ready_.store(false);
    return error;
}

std::string AgentMqttSession::topic(const std::string& suffix) const {
    return "devices/" + device_id_ + "/" + suffix;
}

void AgentMqttSession::handle_connected(int result) {
    ready_.store(result == 0);
    if (result == 0) {
        if (!transport_.subscribe(topic("event-acks"), 1)) {
            std::cerr << "agent failed to subscribe event acknowledgements\n";
            ready_.store(false);
            return;
        }
        if (!transport_.subscribe(topic("commands"), 1)) {
            ready_.store(false);
            return;
        }
        std::cout << "agent MQTT connected\n";
    } else {
        std::cerr << "agent MQTT connection failed; reconnect will be retried\n";
    }
}

void AgentMqttSession::handle_disconnected(int result) {
    ready_.store(false);
    if (result != 0) {
        std::cerr << "agent MQTT disconnected; pending events remain in SQLite\n";
    }
}

}  // namespace yunshao::application
