#include "common/core.hpp"

#include <mosquitto.h>

#include <sys/socket.h>
#include <sys/stat.h>
#include <sys/un.h>
#include <unistd.h>
#include <poll.h>

#include <csignal>
#include <atomic>
#include <ctime>
#include <cerrno>
#include <chrono>
#include <cstdlib>
#include <cstring>
#include <fstream>
#include <functional>
#include <iomanip>
#include <iostream>
#include <sstream>
#include <stdexcept>
#include <string>
#include <thread>

namespace {

using yunshao::EventOutbox;
using yunshao::EdgeAppConfig;
using yunshao::Json;
using yunshao::ModelCatalog;

std::atomic_bool stopping{false};

std::string env(const char* name, const std::string& fallback = "") {
    const char* value = std::getenv(name);
    return value && *value ? value : fallback;
}

void stop_signal(int) { stopping.store(true); }

std::string utc_now() {
    const auto now = std::chrono::system_clock::now();
    const auto time = std::chrono::system_clock::to_time_t(now);
    std::tm utc{};
    gmtime_r(&time, &utc);
    std::ostringstream result;
    result << std::put_time(&utc, "%Y-%m-%dT%H:%M:%SZ");
    return result.str();
}

class Agent {
public:
    Agent(std::string device_id, const std::filesystem::path& db_path, const std::filesystem::path& model_root,
          EdgeAppConfig config)
        : device_id_(std::move(device_id)), config_(std::move(config)), outbox_(db_path, device_id_, config_),
          models_(model_root, config_) {}

    void start_mqtt() {
        const std::string host = env("YUNSHAO_MQTT_HOST");
        if (host.empty()) return;
        const std::string client_id = env("YUNSHAO_MQTT_CLIENT_ID");
        const std::string username = env("YUNSHAO_MQTT_USERNAME");
        const std::string password = env("YUNSHAO_MQTT_PASSWORD");
        const std::string ca = env("YUNSHAO_MQTT_CA");
        const int port = std::stoi(env("YUNSHAO_MQTT_PORT", "8883"));
        if (client_id.empty() || username.empty() || password.empty() || ca.empty()) {
            throw std::runtime_error("MQTT TLS identity is incomplete; credentials are required in the environment");
        }
        mosquitto_lib_init();
        client_ = mosquitto_new(client_id.c_str(), false, this);
        if (!client_) throw std::runtime_error("cannot initialize MQTT client");
        mosquitto_connect_callback_set(client_, &Agent::on_connect);
        mosquitto_message_callback_set(client_, &Agent::on_message);
        mosquitto_disconnect_callback_set(client_, &Agent::on_disconnect);
        if (mosquitto_int_option(client_, MOSQ_OPT_PROTOCOL_VERSION, MQTT_PROTOCOL_V311) != MOSQ_ERR_SUCCESS ||
            mosquitto_username_pw_set(client_, username.c_str(), password.c_str()) != MOSQ_ERR_SUCCESS ||
            mosquitto_tls_set(client_, ca.c_str(), nullptr, nullptr, nullptr, nullptr) != MOSQ_ERR_SUCCESS ||
            mosquitto_tls_opts_set(client_, 1, "tlsv1.2", nullptr) != MOSQ_ERR_SUCCESS) {
            throw std::runtime_error("cannot configure MQTT TLS client");
        }
        const int result = mosquitto_connect_async(client_, host.c_str(), port, 30);
        if (result != MOSQ_ERR_SUCCESS) throw std::runtime_error("cannot connect MQTT client");
        if (mosquitto_loop_start(client_) != MOSQ_ERR_SUCCESS) throw std::runtime_error("cannot start MQTT network loop");
        std::cout << "agent MQTT client started for device " << device_id_ << '\n';
    }

    ~Agent() {
        if (client_) {
            mosquitto_disconnect(client_);
            mosquitto_loop_stop(client_, true);
            mosquitto_destroy(client_);
            mosquitto_lib_cleanup();
        }
    }

    Json status() {
        const Json model = models_.status();
        const Json counts = outbox_.counts();
        std::string model_state = "model_missing";
        bool found_installed = false;
        for (const auto& item : model.value("models", Json::array())) {
            const auto state = item.value("model_state", "model_missing");
            if (state == "model_load_failed") model_state = "model_load_failed";
            else if (state == "installed") found_installed = true;
        }
        if (found_installed && model_state == "model_missing") model_state = "installed_not_loaded";
        return Json{{"schema_version", 1}, {"device_id", device_id_}, {"device_online", true},
                    {"camera_state", "unknown"}, {"npu_state", "not_ready"},
                    {"model_catalog", model}, {"model_state", model_state},
                    {"stream_state", "not_configured"}, {"mqtt_state", connected_.load() ? "connected" : "disconnected"},
                    {"desired_config_version", 0}, {"applied_config_version", 0}, {"queue_depth", counts["pending"]},
                    {"outbox", counts}, {"sent_at", utc_now()}};
    }

    bool enqueue(const Json& event, std::string* error) { return outbox_.enqueue(event, error); }

    void run() {
        start_mqtt();
        while (!stopping.load()) {
            if (connected_.load()) {
                publish_heartbeat();
                publish_due_events();
            }
            std::this_thread::sleep_for(std::chrono::seconds(5));
        }
    }

private:
    std::string device_id_;
    EdgeAppConfig config_;
    EventOutbox outbox_;
    ModelCatalog models_;
    mosquitto* client_ = nullptr;
    std::atomic_bool connected_{false};

    std::string topic(const char* suffix) const { return "devices/" + device_id_ + "/" + suffix; }

    static void on_connect(mosquitto* client, void* userdata, int result) {
        auto* self = static_cast<Agent*>(userdata);
        self->connected_.store(result == 0);
        if (result == 0) {
            const auto ack_topic = self->topic("event-acks");
            if (mosquitto_subscribe(client, nullptr, ack_topic.c_str(), 1) != MOSQ_ERR_SUCCESS) {
                std::cerr << "agent failed to subscribe event acknowledgements\n";
                self->connected_.store(false);
                return;
            }
            std::cout << "agent MQTT connected\n";
        } else {
            std::cerr << "agent MQTT connection failed; reconnect will be retried\n";
        }
    }

    static void on_disconnect(mosquitto*, void* userdata, int result) {
        auto* self = static_cast<Agent*>(userdata);
        self->connected_.store(false);
        if (result != 0) std::cerr << "agent MQTT disconnected; pending events remain in SQLite\n";
    }

    static void on_message(mosquitto*, void* userdata, const mosquitto_message* message) {
        auto* self = static_cast<Agent*>(userdata);
        if (!message || !message->payload || message->payloadlen <= 0) return;
        try {
            const std::string payload(static_cast<const char*>(message->payload), static_cast<std::size_t>(message->payloadlen));
            std::string error;
            if (!self->outbox_.apply_ack(Json::parse(payload), &error)) {
                std::cerr << "agent ignored application acknowledgement: " << error << '\n';
            }
        } catch (...) {
            std::cerr << "agent ignored malformed application acknowledgement\n";
        }
    }

    void publish_heartbeat() {
        const std::string payload = status().dump();
        const auto state_topic = topic("state");
        const int result = mosquitto_publish(client_, nullptr, state_topic.c_str(), static_cast<int>(payload.size()), payload.data(), 1, false);
        if (result != MOSQ_ERR_SUCCESS) connected_.store(false);
    }

    void publish_due_events() {
        for (const auto& event : outbox_.due_events()) {
            const std::string payload = event.dump();
            const auto events_topic = topic("events");
            const int result = mosquitto_publish(client_, nullptr, events_topic.c_str(), static_cast<int>(payload.size()), payload.data(), 1, false);
            std::string error;
            if (result != MOSQ_ERR_SUCCESS) error = mosquitto_strerror(result);
            outbox_.note_publish_attempt(event.at("event_id").get<std::string>(), error);
            if (result != MOSQ_ERR_SUCCESS) connected_.store(false);
        }
    }
};

Json handle_request(Agent& agent, const Json& request) {
    if (!request.is_object()) throw std::invalid_argument("request must be a JSON object");
    const std::string command = request.value("command", "");
    if (command == "status") return agent.status();
    if (command == "enqueue_event") {
        if (!request.contains("event")) throw std::invalid_argument("event is required");
        std::string error;
        if (!agent.enqueue(request.at("event"), &error)) return Json{{"accepted", false}, {"error", error}};
        return Json{{"accepted", true}, {"event_id", request.at("event").value("event_id", "")}};
    }
    throw std::invalid_argument("unsupported command");
}

void serve_socket(Agent& agent, const std::filesystem::path& path) {
    std::filesystem::create_directories(path.parent_path());
    struct stat existing{};
    if (lstat(path.c_str(), &existing) == 0) {
        if (!S_ISSOCK(existing.st_mode)) throw std::runtime_error("IPC path exists and is not a Unix socket");
        if (unlink(path.c_str()) != 0) throw std::runtime_error("cannot replace stale IPC socket");
    }
    const int fd = socket(AF_UNIX, SOCK_STREAM | SOCK_CLOEXEC, 0);
    if (fd < 0) throw std::runtime_error("cannot create local IPC socket");
    sockaddr_un address{};
    address.sun_family = AF_UNIX;
    const std::string path_string = path.string();
    if (path_string.size() >= sizeof(address.sun_path)) {
        close(fd);
        throw std::runtime_error("IPC socket path is too long");
    }
    std::strncpy(address.sun_path, path_string.c_str(), sizeof(address.sun_path) - 1);
    if (bind(fd, reinterpret_cast<sockaddr*>(&address), sizeof(address)) != 0 || chmod(path.c_str(), 0660) != 0 || listen(fd, 32) != 0) {
        close(fd);
        unlink(path.c_str());
        throw std::runtime_error("cannot initialize local IPC socket");
    }
    std::cout << "agent IPC listening at " << path_string << '\n';
    while (!stopping.load()) {
        pollfd ready{fd, POLLIN, 0};
        const int poll_result = poll(&ready, 1, 500);
        if (poll_result <= 0 || !(ready.revents & POLLIN)) continue;
        const int client = accept4(fd, nullptr, nullptr, SOCK_CLOEXEC);
        if (client < 0) {
            if (errno == EINTR) continue;
            if (stopping.load()) break;
            std::this_thread::sleep_for(std::chrono::milliseconds(50));
            continue;
        }
        std::string input;
        char buffer[4096];
        while (input.size() <= 64 * 1024) {
            const auto count = read(client, buffer, sizeof(buffer));
            if (count <= 0) break;
            input.append(buffer, static_cast<std::size_t>(count));
            if (input.find('\n') != std::string::npos) break;
        }
        Json response;
        try {
            const auto newline = input.find('\n');
            const Json request = Json::parse(input.substr(0, newline));
            response = handle_request(agent, request);
        } catch (const std::exception& exception) {
            response = Json{{"accepted", false}, {"error", exception.what()}};
        }
        const std::string output = response.dump() + "\n";
        (void)write(client, output.data(), output.size());
        close(client);
    }
    close(fd);
    unlink(path.c_str());
}

int model_command(int argc, char** argv) {
    const std::filesystem::path root = env("YUNSHAO_MODEL_ROOT", "/var/lib/yunshao/models");
    const auto config_dir = env("YUNSHAO_AGENT_CONFIG_DIR", "/etc/yunshao");
    const EdgeAppConfig config = EdgeAppConfig::load(config_dir);
    ModelCatalog catalog(root, config);
    if (argc >= 3 && std::string(argv[2]) == "status") {
        std::cout << catalog.status(argc >= 4 ? argv[3] : "").dump(2) << '\n';
        return 0;
    }
    if (argc == 4 && std::string(argv[2]) == "rollback") {
        catalog.rollback(argv[3]);
        std::cout << catalog.status(argv[3]).dump(2) << '\n';
        return 0;
    }
    if (argc == 7 && std::string(argv[2]) == "install" && std::string(argv[3]) == "--manifest" && std::string(argv[5]) == "--model") {
        catalog.install(argv[4], argv[6], config.npu_device_id);
        std::cout << catalog.status().dump(2) << '\n';
        return 0;
    }
    std::cerr << "Usage: yunshao-agent model install --manifest manifest.json --model model.om | status [model_id] | rollback <model_id>\n";
    return 2;
}

}  // namespace

int main(int argc, char** argv) {
    try {
        if (argc >= 2 && std::string(argv[1]) == "model") return model_command(argc, argv);
        const std::string device_id = env("YUNSHAO_DEVICE_ID");
        const std::filesystem::path db_path = env("YUNSHAO_AGENT_DB", "/var/lib/yunshao/agent.db");
        const std::filesystem::path model_root = env("YUNSHAO_MODEL_ROOT", "/var/lib/yunshao/models");
        const std::filesystem::path config_dir = env("YUNSHAO_AGENT_CONFIG_DIR", "/etc/yunshao");
        const std::filesystem::path socket_path = env("YUNSHAO_AGENT_SOCKET", "/run/yunshao/agent.sock");
        if (device_id.empty()) throw std::runtime_error("YUNSHAO_DEVICE_ID is required");
        Agent agent(device_id, db_path, model_root, EdgeAppConfig::load(config_dir));
        std::signal(SIGINT, stop_signal);
        std::signal(SIGTERM, stop_signal);
        std::thread socket_thread(serve_socket, std::ref(agent), socket_path);
        agent.run();
        if (socket_thread.joinable()) socket_thread.join();
        return 0;
    } catch (const std::exception& exception) {
        std::cerr << "agent stopped: " << exception.what() << '\n';
        return 1;
    }
}
