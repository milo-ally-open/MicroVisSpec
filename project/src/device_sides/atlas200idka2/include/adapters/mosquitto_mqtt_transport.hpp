#pragma once

#include "application/ports/mqtt_transport.hpp"

struct mosquitto;
struct mosquitto_message;

namespace yunshao::adapters {

class MosquittoMqttTransport final : public application::ports::MqttTransport {
public:
    MosquittoMqttTransport() = default;
    ~MosquittoMqttTransport() override;
    MosquittoMqttTransport(const MosquittoMqttTransport&) = delete;
    MosquittoMqttTransport& operator=(const MosquittoMqttTransport&) = delete;

    void start(const application::ports::MqttConnectionSettings& settings,
               application::ports::MqttCallbacks callbacks) override;
    void stop() override;
    bool subscribe(const std::string& topic, int qos) override;
    std::string publish(const std::string& topic, const std::string& payload, int qos) override;

private:
    ::mosquitto* client_ = nullptr;
    bool library_initialized_ = false;
    application::ports::MqttCallbacks callbacks_;

    static void on_connect(::mosquitto* client, void* userdata, int result);
    static void on_disconnect(::mosquitto* client, void* userdata, int result);
    static void on_message(::mosquitto* client, void* userdata,
                           const ::mosquitto_message* message);
};

}  // namespace yunshao::adapters
