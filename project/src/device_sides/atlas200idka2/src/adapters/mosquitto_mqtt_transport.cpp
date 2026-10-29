#include "adapters/mosquitto_mqtt_transport.hpp"

#include <mosquitto.h>

#include <cstddef>
#include <stdexcept>
#include <utility>

namespace yunshao::adapters {

MosquittoMqttTransport::~MosquittoMqttTransport() { stop(); }

void MosquittoMqttTransport::start(const application::ports::MqttConnectionSettings& settings,
                                   application::ports::MqttCallbacks callbacks) {
    if (settings.host.empty()) throw std::invalid_argument("MQTT host is required");
    if (settings.client_id.empty() || settings.username.empty() || settings.password.empty() ||
        settings.ca_path.empty()) {
        throw std::runtime_error("MQTT TLS identity is incomplete; credentials are required in the environment");
    }
    if (client_ || library_initialized_) throw std::logic_error("MQTT transport is already started");

    callbacks_ = std::move(callbacks);
    const int init_result = mosquitto_lib_init();
    if (init_result != MOSQ_ERR_SUCCESS) {
        callbacks_ = {};
        throw std::runtime_error("cannot initialize MQTT library");
    }
    library_initialized_ = true;
    try {
        client_ = mosquitto_new(settings.client_id.c_str(), false, this);
        if (!client_) throw std::runtime_error("cannot initialize MQTT client");
        mosquitto_connect_callback_set(client_, &MosquittoMqttTransport::on_connect);
        mosquitto_message_callback_set(client_, &MosquittoMqttTransport::on_message);
        mosquitto_disconnect_callback_set(client_, &MosquittoMqttTransport::on_disconnect);
        if (mosquitto_int_option(client_, MOSQ_OPT_PROTOCOL_VERSION, MQTT_PROTOCOL_V311) != MOSQ_ERR_SUCCESS ||
            mosquitto_username_pw_set(client_, settings.username.c_str(), settings.password.c_str()) != MOSQ_ERR_SUCCESS ||
            mosquitto_tls_set(client_, settings.ca_path.c_str(), nullptr, nullptr, nullptr, nullptr) != MOSQ_ERR_SUCCESS ||
            mosquitto_tls_opts_set(client_, 1, "tlsv1.2", nullptr) != MOSQ_ERR_SUCCESS) {
            throw std::runtime_error("cannot configure MQTT TLS client");
        }
        const int result = mosquitto_connect_async(client_, settings.host.c_str(), settings.port, 30);
        if (result != MOSQ_ERR_SUCCESS) throw std::runtime_error("cannot connect MQTT client");
        if (mosquitto_loop_start(client_) != MOSQ_ERR_SUCCESS) {
            throw std::runtime_error("cannot start MQTT network loop");
        }
    } catch (...) {
        stop();
        throw;
    }
}

void MosquittoMqttTransport::stop() {
    if (client_) {
        mosquitto_disconnect(client_);
        mosquitto_loop_stop(client_, true);
        mosquitto_destroy(client_);
        client_ = nullptr;
    }
    if (library_initialized_) {
        mosquitto_lib_cleanup();
        library_initialized_ = false;
    }
    callbacks_ = {};
}

bool MosquittoMqttTransport::subscribe(const std::string& topic, int qos) {
    if (!client_) return false;
    return mosquitto_subscribe(client_, nullptr, topic.c_str(), qos) == MOSQ_ERR_SUCCESS;
}

std::string MosquittoMqttTransport::publish(const std::string& topic,
                                            const std::string& payload,
                                            int qos) {
    if (!client_) return "MQTT client is not initialized";
    const int result = mosquitto_publish(client_, nullptr, topic.c_str(),
        static_cast<int>(payload.size()), payload.data(), qos, false);
    if (result == MOSQ_ERR_SUCCESS) return {};
    return mosquitto_strerror(result);
}

void MosquittoMqttTransport::on_connect(::mosquitto*, void* userdata, int result) {
    auto* self = static_cast<MosquittoMqttTransport*>(userdata);
    if (self->callbacks_.on_connected) self->callbacks_.on_connected(result);
}

void MosquittoMqttTransport::on_disconnect(::mosquitto*, void* userdata, int result) {
    auto* self = static_cast<MosquittoMqttTransport*>(userdata);
    if (self->callbacks_.on_disconnected) self->callbacks_.on_disconnected(result);
}

void MosquittoMqttTransport::on_message(::mosquitto*, void* userdata,
                                        const ::mosquitto_message* message) {
    auto* self = static_cast<MosquittoMqttTransport*>(userdata);
    if (!message || !message->topic || !message->payload || message->payloadlen <= 0) return;
    if (self->callbacks_.on_message) {
        const auto* payload = static_cast<const char*>(message->payload);
        self->callbacks_.on_message(message->topic,
            std::string(payload, static_cast<std::size_t>(message->payloadlen)));
    }
}

}  // namespace yunshao::adapters
