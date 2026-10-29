#pragma once

#include <functional>
#include <string>

namespace yunshao::application::ports {

struct MqttConnectionSettings {
    std::string host;
    std::string client_id;
    std::string username;
    std::string password;
    std::string ca_path;
    int port = 8883;
};

struct MqttCallbacks {
    std::function<void(int)> on_connected;
    std::function<void(int)> on_disconnected;
    std::function<void(const std::string&, const std::string&)> on_message;
};

// Application-facing transport contract for the device agent's cloud link.
class MqttTransport {
public:
    virtual ~MqttTransport() = default;

    virtual void start(const MqttConnectionSettings& settings, MqttCallbacks callbacks) = 0;
    virtual void stop() = 0;
    virtual bool subscribe(const std::string& topic, int qos) = 0;
    // Returns an empty string on success, or a transport error on failure.
    virtual std::string publish(const std::string& topic, const std::string& payload, int qos) = 0;
};

}  // namespace yunshao::application::ports
