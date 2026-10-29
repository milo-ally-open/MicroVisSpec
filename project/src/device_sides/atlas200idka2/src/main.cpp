#include "adapters/agent_event_socket_outbox.hpp"
#include "adapters/file_activation_lease_status.hpp"
#include "adapters/file_alarm_evidence_outbox.hpp"
#include "adapters/file_runtime_rule_state_reader.hpp"
#include "adapters/atlas_evidence_frame_encoder.hpp"
#include "adapters/system_event_metadata.hpp"
#include "application/activation_lease_gate.hpp"
#include "application/alarm_event_publisher.hpp"
#include "common/core.hpp"
#include "manager/config_service.hpp"
#include "manager/inference_manager.hpp"
#include "manager/video_stream_manager.hpp"

#include <cstdlib>
#include <filesystem>
#include <iostream>
#include <stdexcept>
#include <string>
#include <vector>

namespace {

using yunshao::AlarmTrigger;
using yunshao::ConfigService;
using yunshao::EdgeAppConfig;
using yunshao::InferenceManager;
using yunshao::Json;
using yunshao::ModelCatalog;
using yunshao::RtspServerConfig;
using yunshao::VideoStreamManager;

std::string env(const char* name, const std::string& fallback = "") {
    const char* value = std::getenv(name);
    return value && *value ? value : fallback;
}

struct Options {
    std::filesystem::path config_dir = env("YUNSHAO_AGENT_CONFIG_DIR", "/etc/yunshao");
    std::filesystem::path model_root = env("YUNSHAO_MODEL_ROOT", "/var/lib/yunshao/models");
    std::filesystem::path plugin_dir = env("YUNSHAO_PLUGIN_DIR", "/usr/local/lib/yunshao/plugins");
    std::filesystem::path socket_path = env("YUNSHAO_AGENT_SOCKET", "/run/yunshao/agent.sock");
    std::string stream_id = env("YUNSHAO_STREAM_ID");
    std::filesystem::path output;
    bool check_config = false;
};

Options parse_options(int argc, char** argv) {
    Options options;
    for (int i = 1; i < argc; ++i) {
        const std::string argument = argv[i];
        auto next = [&](const char* name) -> std::string {
            if (i + 1 >= argc) throw std::invalid_argument(std::string("missing value for ") + name);
            return argv[++i];
        };
        if (argument == "--config-dir") options.config_dir = next("--config-dir");
        else if (argument == "--model-root") options.model_root = next("--model-root");
        else if (argument == "--plugin-dir") options.plugin_dir = next("--plugin-dir");
        else if (argument == "--stream-id") options.stream_id = next("--stream-id");
        else if (argument == "--output") options.output = next("--output");
        else if (argument == "--check-config") options.check_config = true;
        else if (argument == "--help" || argument == "-h") {
            std::cout << "Usage: yunshao-classroom-runtime [--config-dir DIR] [--stream-id ID] [--model-root DIR] [--plugin-dir DIR] [--output FILE] [--check-config]\n";
            std::exit(0);
        } else throw std::invalid_argument("unknown option: " + argument);
    }
    return options;
}

void print_config(const EdgeAppConfig& config) {
    Json models = Json::array();
    for (const auto& model : config.models) {
        models.push_back(Json{{"model_id", model.model_id}, {"backend", model.backend},
                              {"enabled", model.enabled}, {"classes", model.classes}});
    }
    Json streams = Json::array();
    for (const auto& stream : config.streams) {
        streams.push_back(Json{{"stream_id", stream.stream_id}, {"camera_id", stream.camera_id},
                               {"source_ref", stream.source_ref}, {"model_id", stream.model_id},
                               {"enabled", stream.enabled}, {"enable_ai", stream.enable_ai},
                               {"live_source", stream.live_source}, {"input_timeout_ms", stream.input_timeout_ms},
                               {"output_width", stream.output_width}, {"output_height", stream.output_height},
                               {"output_fps", stream.output_fps}, {"output_bitrate_kbps", stream.output_bitrate_kbps},
                               {"reconnect_initial_delay_ms", stream.reconnect_initial_delay_ms},
                               {"reconnect_max_delay_ms", stream.reconnect_max_delay_ms},
                               {"rtsp_transport", stream.rtsp_transport},
                               {"rtsp_output", Json{{"enabled", stream.rtsp_output.enabled},
                                                     {"listen_address", stream.rtsp_output.listen_address},
                                                     {"port", stream.rtsp_output.port},
                                                     {"mount_path", stream.rtsp_output.mount_path},
                                                     {"max_queue_ms", stream.rtsp_output.max_queue_ms}}}});
    }
    Json alarms = Json::array();
    for (const auto& rule : config.alarm_rules) {
        alarms.push_back(Json{{"model_id", rule.model_id}, {"class_name", rule.class_name},
                              {"enabled", rule.enabled}, {"severity", rule.severity}});
    }
    std::cout << Json{{"schema_version", config.schema_version}, {"npu_device_id", config.npu_device_id},
                      {"max_loaded_models", config.max_loaded_models}, {"models", models},
                      {"streams", streams}, {"alarm_rules", alarms}}.dump(2) << '\n';
}

}  // namespace

int main(int argc, char** argv) {
    try {
        Options options = parse_options(argc, argv);
        ConfigService config_service(options.config_dir);
        const EdgeAppConfig config = config_service.snapshot();
        if (options.check_config) {
            print_config(config);
            return 0;
        }
        if (options.stream_id.empty()) {
            for (const auto& stream : config.streams) {
                if (stream.enabled && stream.enable_ai) { options.stream_id = stream.stream_id; break; }
            }
        }
        if (options.stream_id.empty()) {
            std::cout << "No enabled AI stream is configured; classroom-runtime is idle.\n";
            return 0;
        }
        const auto& stream = config.require_stream(options.stream_id);
        if (!stream.enabled || !stream.enable_ai) {
            std::cout << "Stream " << stream.stream_id << " is disabled.\n";
            return 0;
        }
        const char* lease_file = std::getenv("YUNSHAO_ACTIVATION_LEASE_FILE");
        yunshao::adapters::FileActivationLeaseStatus lease_status(
            lease_file && *lease_file ? lease_file : "/var/lib/yunshao/activation-lease.json");
        yunshao::application::ActivationLeaseGate lease_gate(lease_status);
        lease_gate.wait_until_valid();
        const auto& model = config.require_model(stream.model_id);
        const std::string input_uri = env(stream.source_ref.c_str());
        if (input_uri.empty()) throw std::runtime_error("configured camera source is not injected: " + stream.source_ref);

        ModelCatalog catalog(options.model_root, config);
        const Json model_state = catalog.status(stream.model_id);
        if (model_state.value("model_state", "") != "installed") {
            throw std::runtime_error("active model is not ready: " + model_state.dump());
        }
        const std::string model_version = model_state.value("model_version", "unknown");
        yunshao::adapters::AgentEventSocketOutbox event_outbox(options.socket_path);
        yunshao::adapters::FileAlarmEvidenceOutbox evidence_outbox(
            std::filesystem::path(env("YUNSHAO_EVIDENCE_OUTBOX_DIR", "/var/lib/yunshao/evidence-outbox")));
        yunshao::adapters::SystemEventMetadata event_metadata;
        yunshao::application::AlarmEventPublisher alarm_event_publisher(
            event_outbox, evidence_outbox, event_metadata);
        if (options.output.empty()) {
            if (!stream.rtsp_output.enabled) {
                options.output = std::filesystem::path("/var/lib/yunshao/preview") / (stream.stream_id + ".h264");
            }
        }
        if (!options.output.parent_path().empty()) std::filesystem::create_directories(options.output.parent_path());
        RtspServerConfig rtsp_server_config;
        rtsp_server_config.enabled = stream.rtsp_output.enabled;
        if (rtsp_server_config.enabled) {
            rtsp_server_config.listen_address = stream.rtsp_output.listen_address;
            rtsp_server_config.port = stream.rtsp_output.port;
            rtsp_server_config.mount_path = stream.rtsp_output.mount_path;
            rtsp_server_config.fps = stream.output_fps;
            rtsp_server_config.bitrate_kbps = stream.output_bitrate_kbps;
            rtsp_server_config.max_queue_ms = stream.rtsp_output.max_queue_ms;
        }
        InferenceManager inference(config.max_loaded_models, config.npu_device_id,
                                   options.model_root, options.plugin_dir);
        yunshao::adapters::AtlasEvidenceFrameEncoder evidence_frame_encoder;
        yunshao::adapters::FileRuntimeRuleStateReader rule_state_reader(options.config_dir);
        VideoStreamManager stream_manager(config, inference,
            [&](const AlarmTrigger& trigger, const std::vector<unsigned char>& jpeg) {
                yunshao::application::AlarmEventData event;
                event.device_id = env("YUNSHAO_DEVICE_ID");
                event.camera_id = stream.camera_id;
                event.event_type = trigger.event_type;
                event.severity = trigger.severity;
                event.algorithm_key = model.algorithm_key;
                event.algorithm_version = model_version;
                event.model_id = trigger.model_id;
                event.class_attribute = trigger.class_attribute;
                event.class_name = trigger.class_name;
                event.confidence = trigger.confidence;
                event.rule_config_version = trigger.config_version;
                event.observed_duration_ms = trigger.observed_duration_ms;
                const auto result = alarm_event_publisher.publish(event, jpeg);
                std::cout << "persisted classroom alarm event " << result.value("event_id", "") << '\n';
            }, options.config_dir, evidence_frame_encoder, lease_status, rule_state_reader);
        for (;;) {
            stream_manager.run(stream, input_uri, options.output, rtsp_server_config);
            if (!lease_gate.expired()) break;
            lease_gate.wait_until_valid();
        }
        std::cout << "Processed stream " << stream.stream_id << " with model " << stream.model_id
                  << '@' << model_version;
        if (!options.output.empty()) std::cout << "; annotated H.264 saved to " << options.output.string();
        std::cout << '\n';
        return 0;
    } catch (const std::exception& error) {
        std::cerr << "classroom-runtime failed: " << error.what() << '\n';
        return 1;
    }
}
