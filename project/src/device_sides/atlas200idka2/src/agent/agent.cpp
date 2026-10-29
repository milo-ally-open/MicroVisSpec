#include "adapters/unix_agent_control_server.hpp"
#include "adapters/mosquitto_mqtt_transport.hpp"
#include "adapters/sqlite_event_outbox.hpp"
#include "adapters/https_evidence_uploader.hpp"
#include "adapters/agent_cloud_rule_command_settings.hpp"
#include "adapters/agent_applied_cloud_rule_state.hpp"
#include "adapters/systemd_agent_service_lifecycle.hpp"
#include "adapters/file_activation_lease_writer.hpp"
#include "adapters/file_agent_configuration_store.hpp"
#include "adapters/system_event_metadata.hpp"
#include "application/agent_applied_rule_acknowledger.hpp"
#include "application/agent_activation_workflow.hpp"
#include "application/agent_run_loop.hpp"
#include "application/agent_cloud_command_processor.hpp"
#include "application/agent_cloud_cycle.hpp"
#include "application/agent_command_acknowledger.hpp"
#include "application/agent_mqtt_inbox.hpp"
#include "application/agent_mqtt_session.hpp"
#include "application/ports/durable_agent_event_outbox.hpp"
#include "application/ports/event_metadata.hpp"
#include "application/ports/evidence_uploader.hpp"
#include "application/ports/mqtt_transport.hpp"
#include "common/core.hpp"
#include "common/cloud_config.hpp"

#include <csignal>
#include <atomic>
#include <cctype>
#include <ctime>
#include <chrono>
#include <cstdlib>
#include <fstream>
#include <iostream>
#include <mutex>
#include <sstream>
#include <stdexcept>
#include <string>
#include <thread>

int activation_command(int argc, char** argv,
                       yunshao::application::ports::AgentServiceLifecycle& service_lifecycle,
                       yunshao::application::AgentActivationWorkflow& activation_workflow);
bool poll_activation_once(const std::string& device_id, const std::string& server,
                          const std::string& username, const std::string& password,
                          const std::string& api_ca_path, const std::string& mqtt_ca_path,
                          yunshao::application::AgentActivationWorkflow& activation_workflow);

namespace {

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

class Agent : public yunshao::application::ports::LocalAgentControl {
public:
    Agent(std::string device_id, yunshao::application::ports::DurableAgentEventOutbox& outbox,
          yunshao::application::ports::EvidenceUploader& evidence_uploader,
          yunshao::application::ports::MqttTransport& mqtt,
          yunshao::application::ports::MqttConnectionSettings mqtt_settings,
          const std::filesystem::path& model_root, EdgeAppConfig config,
          yunshao::application::ports::EventMetadata& event_metadata,
          yunshao::application::AgentActivationWorkflow& activation_workflow)
        : device_id_(std::move(device_id)), config_(std::move(config)), outbox_(outbox),
          mqtt_inbox_(outbox_, "devices/" + device_id_ + "/commands"),
          evidence_uploader_(evidence_uploader),
          mqtt_session_(mqtt, std::move(mqtt_settings), mqtt_inbox_, device_id_),
          models_(model_root, config_), event_metadata_(event_metadata),
          activation_workflow_(activation_workflow),
          cloud_rule_settings_(config_, models_, config_mutex_, config_directory(), device_id_),
          command_acknowledger_(device_id_, mqtt_session_, event_metadata_),
          cloud_command_processor_(mqtt_inbox_, cloud_rule_settings_, command_acknowledger_),
          applied_cloud_rule_state_(config_, outbox_, config_directory(), runtime_state_directory()),
          applied_rule_acknowledger_(applied_cloud_rule_state_, command_acknowledger_, config_mutex_),
          cloud_cycle_(device_id_, mqtt_session_, cloud_command_processor_, applied_rule_acknowledger_,
                       *this, outbox_, evidence_uploader_) {}

    Json status() override {
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
                    {"stream_state", runtime_running ? "running" : "unavailable"}, {"mqtt_state", mqtt_session_.ready() ? "connected" : "disconnected"},
                    {"desired_config_version", 0}, {"applied_config_version", 0}, {"queue_depth", counts["pending"]},
                    {"outbox", counts}, {"cameras", cameras}, {"algorithms", algorithms},
                    {"sent_at", event_metadata_.utc_timestamp()}};
    }

    yunshao::application::ports::EventEnqueueResult enqueue_event(const Json& event) override {
        std::string error;
        const bool accepted = outbox_.enqueue(event, &error);
        return {accepted, accepted ? event.value("event_id", "") : "", error};
    }

    bool run() {
        yunshao::application::AgentRunLoop run_loop(
            mqtt_session_, cloud_cycle_,
            [this] {
                return poll_activation_once(device_id_, env("YUNSHAO_EDGE_API_ORIGIN"),
                    env("YUNSHAO_MQTT_USERNAME"), env("YUNSHAO_MQTT_PASSWORD"),
                    env("YUNSHAO_EDGE_API_CA", env("YUNSHAO_MQTT_CA")),
                    env("YUNSHAO_MQTT_CA"), activation_workflow_);
            },
            [] { return stopping.load(); },
            [](std::chrono::seconds duration) { std::this_thread::sleep_for(duration); });
        return run_loop.run();
    }

private:
    std::string device_id_;
    EdgeAppConfig config_;
    yunshao::application::ports::DurableAgentEventOutbox& outbox_;
    yunshao::application::AgentMqttInbox mqtt_inbox_;
    yunshao::application::ports::EvidenceUploader& evidence_uploader_;
    yunshao::application::AgentMqttSession mqtt_session_;
    ModelCatalog models_;
    yunshao::application::ports::EventMetadata& event_metadata_;
    yunshao::application::AgentActivationWorkflow& activation_workflow_;
    std::mutex config_mutex_;
    yunshao::adapters::AgentCloudRuleCommandSettings cloud_rule_settings_;
    yunshao::application::AgentCommandAcknowledger command_acknowledger_;
    yunshao::application::AgentCloudCommandProcessor cloud_command_processor_;
    yunshao::adapters::AgentAppliedCloudRuleState applied_cloud_rule_state_;
    yunshao::application::AgentAppliedRuleAcknowledger applied_rule_acknowledger_;
    yunshao::application::AgentCloudCycle cloud_cycle_;

    std::filesystem::path config_directory() const { return env("YUNSHAO_AGENT_CONFIG_DIR", "/etc/yunshao"); }
    std::filesystem::path runtime_state_directory() const {
        return env("YUNSHAO_RUNTIME_STATE_DIR", "/run/yunshao/runtime");
    }
    Json runtime_state(const yunshao::StreamProfile& stream) const {
        return yunshao::fresh_runtime_state(
            runtime_state_directory() / (stream.stream_id + ".json"), stream.model_id);
    }
};

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
            yunshao::adapters::SystemdAgentServiceLifecycle service_lifecycle;
            yunshao::adapters::FileActivationLeaseWriter lease_writer(
                env("YUNSHAO_ACTIVATION_LEASE_FILE", "/var/lib/yunshao/activation-lease.json"));
            yunshao::adapters::FileAgentConfigurationStore configuration_store(
                "/etc/yunshao/agent.env", "/var/lib/yunshao/activation.env");
            yunshao::application::AgentActivationWorkflow activation_workflow(configuration_store, lease_writer);
            return activation_command(argc, argv, service_lifecycle, activation_workflow);
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
        yunshao::adapters::FileActivationLeaseWriter activation_lease_writer(
            env("YUNSHAO_ACTIVATION_LEASE_FILE", "/var/lib/yunshao/activation-lease.json"));
        yunshao::adapters::FileAgentConfigurationStore agent_configuration_store(
            "/etc/yunshao/agent.env", "/var/lib/yunshao/activation.env");
        yunshao::application::AgentActivationWorkflow activation_workflow(
            agent_configuration_store, activation_lease_writer);
        if (device_id.empty()) throw std::runtime_error("YUNSHAO_DEVICE_ID is required");
        yunshao::application::ports::MqttConnectionSettings mqtt_settings;
        mqtt_settings.host = env("YUNSHAO_MQTT_HOST");
        mqtt_settings.client_id = env("YUNSHAO_MQTT_CLIENT_ID");
        mqtt_settings.username = env("YUNSHAO_MQTT_USERNAME");
        mqtt_settings.password = env("YUNSHAO_MQTT_PASSWORD");
        mqtt_settings.ca_path = env("YUNSHAO_MQTT_CA");
        if (!mqtt_settings.host.empty()) mqtt_settings.port = std::stoi(env("YUNSHAO_MQTT_PORT", "8883"));
        const EdgeAppConfig config = EdgeAppConfig::load(config_dir);
        yunshao::adapters::MosquittoMqttTransport mqtt_transport;
        yunshao::adapters::SQLiteEventOutbox event_outbox(db_path, device_id, config);
        yunshao::adapters::HttpsEvidenceUploader evidence_uploader(event_outbox, {
            env("YUNSHAO_EDGE_API_ORIGIN"),
            std::filesystem::path(env("YUNSHAO_EVIDENCE_OUTBOX_DIR", "/var/lib/yunshao/evidence-outbox")),
            env("YUNSHAO_MQTT_USERNAME"),
            env("YUNSHAO_MQTT_PASSWORD"),
            std::filesystem::path(env("YUNSHAO_EDGE_API_CA", env("YUNSHAO_MQTT_CA")))});
        yunshao::adapters::SystemEventMetadata event_metadata;
        Agent agent(device_id, event_outbox, evidence_uploader, mqtt_transport,
                    std::move(mqtt_settings), model_root, config, event_metadata,
                    activation_workflow);
        yunshao::adapters::UnixAgentControlServer local_control(agent, socket_path);
        std::signal(SIGINT, stop_signal);
        std::signal(SIGTERM, stop_signal);
        std::thread socket_thread([&] { local_control.run(stopping); });
        const bool credentials_updated = agent.run();
        if (credentials_updated) stopping.store(true);
        if (socket_thread.joinable()) socket_thread.join();
        return credentials_updated ? 75 : 0;
    } catch (const std::exception& exception) {
        std::cerr << "agent stopped: " << exception.what() << '\n';
        return 1;
    }
}
