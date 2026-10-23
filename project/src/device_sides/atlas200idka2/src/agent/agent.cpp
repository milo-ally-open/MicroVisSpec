#include "common/core.hpp"
#include "common/cloud_config.hpp"

#include <mosquitto.h>
#include <curl/curl.h>
#include <cstdio>

#include <sys/socket.h>
#include <sys/stat.h>
#include <sys/un.h>
#include <unistd.h>
#include <poll.h>

#include <csignal>
#include <atomic>
#include <cctype>
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

int activation_command(int argc, char** argv);
bool poll_activation_once(const std::string& device_id, const std::string& server,
                          const std::string& username, const std::string& password,
                          const std::string& api_ca_path, const std::string& mqtt_ca_path);

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

std::string trim(std::string value) {
    const auto first = value.find_first_not_of(" \t\r\n");
    if (first == std::string::npos) return {};
    const auto last = value.find_last_not_of(" \t\r\n");
    return value.substr(first, last - first + 1);
}

void load_cann_environment() {
    const std::string path = env("YUNSHAO_CANN_ENV_FILE", "/etc/yunshao/cann.env");
    std::ifstream input(path);
    if (!input) throw std::runtime_error("cannot open CANN environment file: " + path);

    bool has_library_path = false;
    bool has_toolkit_path = false;
    bool has_opp_path = false;
    std::string line;
    while (std::getline(input, line)) {
        line = trim(std::move(line));
        if (line.empty() || line.front() == '#') continue;
        const auto separator = line.find('=');
        if (separator == std::string::npos) {
            throw std::runtime_error("invalid CANN environment assignment in " + path);
        }
        const std::string name = trim(line.substr(0, separator));
        std::string value = trim(line.substr(separator + 1));
        if (name.empty() || !((name.front() >= 'A' && name.front() <= 'Z') ||
                              (name.front() >= 'a' && name.front() <= 'z') || name.front() == '_')) {
            throw std::runtime_error("invalid CANN environment variable name in " + path);
        }
        for (const unsigned char character : name) {
            if (!std::isalnum(character) && character != '_') {
                throw std::runtime_error("invalid CANN environment variable name in " + path);
            }
        }
        if (value.size() >= 2 && ((value.front() == '"' && value.back() == '"') ||
                                  (value.front() == '\'' && value.back() == '\''))) {
            value = value.substr(1, value.size() - 2);
        }
        if (::setenv(name.c_str(), value.c_str(), 1) != 0) {
            throw std::runtime_error("cannot load CANN environment variable: " + name);
        }
        if (name == "LD_LIBRARY_PATH") has_library_path = !value.empty();
        if (name == "ASCEND_HOME_PATH" || name == "ASCEND_TOOLKIT_HOME") has_toolkit_path = !value.empty();
        if (name == "ASCEND_OPP_PATH") has_opp_path = !value.empty();
    }
    if (!input.eof()) throw std::runtime_error("cannot read CANN environment file: " + path);
    if (!has_library_path || !has_toolkit_path || !has_opp_path) {
        throw std::runtime_error("CANN environment file must define LD_LIBRARY_PATH, ASCEND_HOME_PATH, and ASCEND_OPP_PATH");
    }
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
        std::lock_guard<std::mutex> config_lock(config_mutex_);
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
        Json algorithms = Json::array();
        Json cameras = Json::array();
        bool runtime_running = false;
        Json cloud = Json::object();
        try { cloud = yunshao::read_json_file(config_directory() / "alarm_rules.json").value("cloud_command", Json::object()); }
        catch (...) {}
        for (const auto& profile : config_.models) {
            const auto installed = models_.status(profile.model_id);
            if (!profile.enabled || installed.value("model_state", "") != "installed") continue;
            Json runtime = Json::object();
            for (const auto& stream : config_.streams) {
                if (stream.model_id != profile.model_id) continue;
                const auto state = runtime_state(stream);
                cameras.push_back(Json{{"camera_key", stream.camera_key},
                    {"camera_state", state.empty() ? "unknown" : "streaming"},
                    {"stream_state", state.empty() ? "unavailable" : "running"}});
                if (!state.empty()) runtime = state;
            }
            runtime_running = runtime_running || !runtime.empty();
            const bool applied = !runtime.empty() && !cloud.empty() &&
                runtime.value("cloud_command_id", "") == cloud.value("command_id", "");
            algorithms.push_back(Json{{"algorithm_key", profile.algorithm_key},
                {"algorithm_version", installed.value("model_version", "")},
                {"runtime_state", runtime.empty() ? "installed_not_loaded" : "running"},
                {"capabilities", Json{{"classes", profile.classes}, {"parameters", Json::object()}}},
                {"reported_parameters", applied ? cloud.at("parameters") : yunshao::cloud_rule_parameters(config_, profile.model_id)},
                {"reported_config_version", applied ? cloud.at("expected_version").get<int>() : 0}});
        }
        return Json{{"schema_version", 1}, {"device_id", device_id_}, {"device_online", true},
                    {"camera_state", runtime_running ? "streaming" : "unknown"}, {"npu_state", runtime_running ? "ready" : "not_ready"},
                    {"model_catalog", model}, {"model_state", runtime_running ? "running" : model_state},
                    {"stream_state", runtime_running ? "running" : "unavailable"}, {"mqtt_state", connected_.load() ? "connected" : "disconnected"},
                    {"desired_config_version", 0}, {"applied_config_version", 0}, {"queue_depth", counts["pending"]},
                    {"outbox", counts}, {"cameras", cameras}, {"algorithms", algorithms}, {"sent_at", utc_now()}};
    }

    bool enqueue(const Json& event, std::string* error) { return outbox_.enqueue(event, error); }

    bool run() {
        start_mqtt();
        auto next_activation_poll = std::chrono::steady_clock::time_point{};
        while (!stopping.load()) {
            const auto now = std::chrono::steady_clock::now();
            if (now >= next_activation_poll) {
                if (poll_activation_once(device_id_, env("YUNSHAO_EDGE_API_ORIGIN"),
                                         env("YUNSHAO_MQTT_USERNAME"), env("YUNSHAO_MQTT_PASSWORD"),
                                         env("YUNSHAO_EDGE_API_CA", env("YUNSHAO_MQTT_CA")),
                                         env("YUNSHAO_MQTT_CA"))) return true;
                next_activation_poll = now + std::chrono::seconds(15);
            }
            if (connected_.load()) {
                process_commands();
                acknowledge_applied_rules();
                publish_heartbeat();
                publish_due_events();
                upload_evidence();
            }
            std::this_thread::sleep_for(std::chrono::seconds(5));
        }
        return false;
    }

private:
    std::string device_id_;
    EdgeAppConfig config_;
    EventOutbox outbox_;
    ModelCatalog models_;
    mosquitto* client_ = nullptr;
    std::atomic_bool connected_{false};
    std::mutex config_mutex_;
    std::mutex command_mutex_;
    std::vector<Json> commands_;
    std::string acknowledged_command_;

    static std::size_t discard_response(char*, std::size_t size, std::size_t count, void*) { return size * count; }
    void upload_evidence() {
        const auto origin = env("YUNSHAO_EDGE_API_ORIGIN");
        if (origin.empty()) return;
        if (origin.rfind("https://", 0) != 0) throw std::runtime_error("edge API must use HTTPS");
        const auto directory = std::filesystem::path(env("YUNSHAO_EVIDENCE_OUTBOX_DIR", "/var/lib/yunshao/evidence-outbox"));
        if (!std::filesystem::exists(directory)) return;
        static std::once_flag curl_initialized;
        std::call_once(curl_initialized, [] { if (curl_global_init(CURL_GLOBAL_DEFAULT) != CURLE_OK) throw std::runtime_error("cannot initialize HTTPS uploader"); });
        for (const auto& entry : std::filesystem::directory_iterator(directory)) {
            if (!entry.is_regular_file() || entry.path().extension() != ".jpg") continue;
            const auto event_id = entry.path().stem().string();
            if (event_id.size() != 36) continue;
            const auto state = outbox_.event_state(event_id);
            if (state == "quarantined") { std::filesystem::remove(entry.path()); continue; }
            if (state != "acked") continue;
            const auto bytes = entry.file_size();
            if (bytes == 0 || bytes > 4 * 1024 * 1024) continue;
            FILE* file = std::fopen(entry.path().c_str(), "rb");
            if (!file) return;
            CURL* curl = curl_easy_init();
            if (!curl) { std::fclose(file); return; }
            const auto url = origin + "/edge/v1/devices/" + device_id_ + "/events/" + event_id + "/evidence";
            const auto username = env("YUNSHAO_MQTT_USERNAME");
            const auto password = env("YUNSHAO_MQTT_PASSWORD");
            const auto ca = env("YUNSHAO_EDGE_API_CA", env("YUNSHAO_MQTT_CA"));
            auto* headers = curl_slist_append(nullptr, "Content-Type: image/jpeg");
            curl_easy_setopt(curl, CURLOPT_URL, url.c_str());
            curl_easy_setopt(curl, CURLOPT_PROXY, "");
            curl_easy_setopt(curl, CURLOPT_UPLOAD, 1L);
            curl_easy_setopt(curl, CURLOPT_READDATA, file);
            curl_easy_setopt(curl, CURLOPT_INFILESIZE_LARGE, static_cast<curl_off_t>(bytes));
            curl_easy_setopt(curl, CURLOPT_HTTPHEADER, headers);
            curl_easy_setopt(curl, CURLOPT_HTTPAUTH, CURLAUTH_BASIC);
            curl_easy_setopt(curl, CURLOPT_USERNAME, username.c_str());
            curl_easy_setopt(curl, CURLOPT_PASSWORD, password.c_str());
            curl_easy_setopt(curl, CURLOPT_CAINFO, ca.c_str());
            curl_easy_setopt(curl, CURLOPT_SSL_VERIFYPEER, 1L);
            curl_easy_setopt(curl, CURLOPT_SSL_VERIFYHOST, 2L);
            curl_easy_setopt(curl, CURLOPT_PROTOCOLS, CURLPROTO_HTTPS);
            curl_easy_setopt(curl, CURLOPT_FOLLOWLOCATION, 0L);
            curl_easy_setopt(curl, CURLOPT_CONNECTTIMEOUT, 3L);
            curl_easy_setopt(curl, CURLOPT_TIMEOUT, 8L);
            curl_easy_setopt(curl, CURLOPT_NOSIGNAL, 1L);
            curl_easy_setopt(curl, CURLOPT_WRITEFUNCTION, &Agent::discard_response);
            const auto result = curl_easy_perform(curl);
            long status = 0;
            curl_easy_getinfo(curl, CURLINFO_RESPONSE_CODE, &status);
            curl_slist_free_all(headers); curl_easy_cleanup(curl); std::fclose(file);
            if (result == CURLE_OK && status >= 200 && status < 300) {
                std::filesystem::remove(entry.path());
                std::cout << "evidence_uploaded event_id=" << event_id << '\n';
            }
            return; // Bound work per heartbeat; a failure is retried on a later pass.
        }
    }

    std::filesystem::path config_directory() const { return env("YUNSHAO_AGENT_CONFIG_DIR", "/etc/yunshao"); }
    Json runtime_state(const yunshao::StreamProfile& stream) const {
        const auto directory = std::filesystem::path(env("YUNSHAO_RUNTIME_STATE_DIR", "/run/yunshao/runtime"));
        return yunshao::fresh_runtime_state(directory / (stream.stream_id + ".json"), stream.model_id);
    }
    void command_ack(const Json& command, const std::string& status, const std::string& reason = "") {
        const auto payload = Json{{"schema_version", 1}, {"device_id", device_id_},
            {"command_id", command.value("command_id", "")}, {"sent_at", utc_now()},
            {"status", status}, {"reported_config_version", command.value("expected_version", 0)},
            {"failure_reason", reason}}.dump();
        const auto destination = topic("command-acks");
        if (mosquitto_publish(client_, nullptr, destination.c_str(), static_cast<int>(payload.size()), payload.data(), 1, false) != MOSQ_ERR_SUCCESS)
            throw std::runtime_error("cannot acknowledge cloud configuration");
    }
    void process_commands() {
        std::vector<Json> queued;
        { std::lock_guard<std::mutex> lock(command_mutex_); queued.swap(commands_); }
        for (const auto& command : queued) {
            try {
                std::lock_guard<std::mutex> lock(config_mutex_);
                bool supported = false;
                for (const auto& profile : config_.models) {
                    if (!profile.enabled || profile.algorithm_key != command.value("algorithm_key", "")) continue;
                    const auto model = models_.status(profile.model_id);
                    if (model.value("model_state", "") != "installed") throw std::invalid_argument("model is not installed");
                    yunshao::apply_cloud_rules(config_directory(), device_id_, profile.model_id,
                        model.value("model_version", ""), command);
                    supported = true;
                    break;
                }
                if (!supported) throw std::invalid_argument("algorithm is not installed");
                config_ = EdgeAppConfig::load(config_directory());
            } catch (const std::exception& error) {
                command_ack(command, "rejected", error.what());
            }
        }
    }
    void acknowledge_applied_rules() {
        std::lock_guard<std::mutex> lock(config_mutex_);
        const auto command = yunshao::read_json_file(config_directory() / "alarm_rules.json").value("cloud_command", Json::object());
        if (command.empty() || command.value("command_id", "") == acknowledged_command_) return;
        bool found = false;
        for (const auto& stream : config_.streams) {
            if (!stream.enabled || !stream.enable_ai || config_.require_model(stream.model_id).algorithm_key != command.value("algorithm_key", "")) continue;
            found = true;
            const auto state = runtime_state(stream);
            if (state.value("cloud_command_id", "") != command.value("command_id", "")) return;
        }
        if (!found) return;
        outbox_.replace_config(config_);
        command_ack(command, "applied");
        yunshao::write_json_atomic(config_directory() / "cloud-ready.json",
            Json{{"command_id", command.at("command_id")}, {"updated_at_unix", std::time(nullptr)}});
        acknowledged_command_ = command.at("command_id").get<std::string>();
    }

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
            const auto commands_topic = self->topic("commands");
            if (mosquitto_subscribe(client, nullptr, commands_topic.c_str(), 1) != MOSQ_ERR_SUCCESS) {
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
            if (std::string(message->topic) == self->topic("commands")) {
                if (payload.size() > 65536) return;
                const auto command = Json::parse(payload);
                std::lock_guard<std::mutex> lock(self->command_mutex_);
                if (self->commands_.size() < 16) self->commands_.push_back(command);
                return;
            }
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
            const std::string payload = yunshao::cloud_event_payload(event).dump();
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
        if (argc >= 2 && std::string(argv[1]) == "activate") {
            return activation_command(argc, argv);
        }
        if (argc >= 2 && std::string(argv[1]) == "model") {
            load_cann_environment();
            return model_command(argc, argv);
        }
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
        const bool credentials_updated = agent.run();
        if (credentials_updated) stopping.store(true);
        if (socket_thread.joinable()) socket_thread.join();
        return credentials_updated ? 75 : 0;
    } catch (const std::exception& exception) {
        std::cerr << "agent stopped: " << exception.what() << '\n';
        return 1;
    }
}
