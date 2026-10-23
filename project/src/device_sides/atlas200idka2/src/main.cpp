#include "common/core.hpp"
#include "common/cloud_config.hpp"
#include "manager/config_service.hpp"
#include "manager/inference_manager.hpp"
#include "manager/video_stream_manager.hpp"

#include <sys/socket.h>
#include <sys/un.h>
#include <unistd.h>

#include <array>
#include <cerrno>
#include <chrono>
#include <cstdlib>
#include <cstring>
#include <ctime>
#include <filesystem>
#include <iomanip>
#include <fstream>
#include <iostream>
#include <random>
#include <sstream>
#include <stdexcept>
#include <string>
#include <thread>
#include <vector>

namespace {

using yunshao::AlarmTrigger;
using yunshao::ConfigService;
using yunshao::EdgeAppConfig;
using yunshao::EventOutbox;
using yunshao::InferenceManager;
using yunshao::Json;
using yunshao::ModelCatalog;
using yunshao::RtspServerConfig;
using yunshao::StreamProfile;
using yunshao::VideoStreamManager;

std::string env(const char* name, const std::string& fallback = "") {
    const char* value = std::getenv(name);
    return value && *value ? value : fallback;
}

std::string utc_now() {
    const auto now = std::chrono::system_clock::now();
    const auto time = std::chrono::system_clock::to_time_t(now);
    std::tm utc{};
    gmtime_r(&time, &utc);
    std::ostringstream result;
    result << std::put_time(&utc, "%Y-%m-%dT%H:%M:%SZ");
    return result.str();
}

bool activation_lease_expired() {
    const char* configured = std::getenv("YUNSHAO_ACTIVATION_LEASE_FILE");
    const std::filesystem::path path = configured && *configured ? configured : "/var/lib/yunshao/activation-lease.json";
    if (!std::filesystem::exists(path)) return false;
    try {
        const auto lease = yunshao::read_json_file(path);
        if (!lease.contains("expires_at_unix") || lease["expires_at_unix"].is_null()) return false;
        return lease.at("expires_at_unix").get<std::int64_t>() <= std::time(nullptr);
    } catch (...) {
        return true;
    }
}

void wait_for_activation_lease() {
    while (activation_lease_expired()) {
        std::cout << "设备授权已到期，runtime 暂停；网页续期后将自动恢复" << std::endl;
        std::this_thread::sleep_for(std::chrono::seconds(5));
    }
}

std::string make_event_id() {
    std::random_device random;
    std::array<unsigned char, 16> bytes{};
    for (auto& value : bytes) value = static_cast<unsigned char>(random());
    bytes[6] = static_cast<unsigned char>((bytes[6] & 0x0fU) | 0x40U);
    bytes[8] = static_cast<unsigned char>((bytes[8] & 0x3fU) | 0x80U);
    std::ostringstream result;
    result << std::hex << std::setfill('0');
    for (std::size_t i = 0; i < bytes.size(); ++i) {
        result << std::setw(2) << static_cast<unsigned int>(bytes[i]);
        if (i == 3 || i == 5 || i == 7 || i == 9) result << '-';
    }
    return result.str();
}

void write_all(int fd, const std::string& value) {
    std::size_t offset = 0;
    while (offset < value.size()) {
        const ssize_t count = write(fd, value.data() + offset, value.size() - offset);
        if (count < 0 && errno == EINTR) continue;
        if (count <= 0) throw std::runtime_error("cannot send local agent request");
        offset += static_cast<std::size_t>(count);
    }
}

Json enqueue_event(const std::filesystem::path& socket_path, const Json& event) {
    const int fd = socket(AF_UNIX, SOCK_STREAM | SOCK_CLOEXEC, 0);
    if (fd < 0) throw std::runtime_error("cannot create event IPC socket");
    sockaddr_un address{};
    address.sun_family = AF_UNIX;
    const std::string path = socket_path.string();
    if (path.size() >= sizeof(address.sun_path)) {
        close(fd);
        throw std::runtime_error("agent socket path is too long");
    }
    std::strncpy(address.sun_path, path.c_str(), sizeof(address.sun_path) - 1);
    if (connect(fd, reinterpret_cast<sockaddr*>(&address), sizeof(address)) != 0) {
        close(fd);
        throw std::runtime_error("agent event queue is unavailable");
    }
    try {
        write_all(fd, Json{{"command", "enqueue_event"}, {"event", event}}.dump() + "\n");
        std::string input;
        char buffer[4096];
        while (input.size() <= 64 * 1024) {
            const auto count = read(fd, buffer, sizeof(buffer));
            if (count < 0 && errno == EINTR) continue;
            if (count <= 0) break;
            input.append(buffer, static_cast<std::size_t>(count));
            if (input.find('\n') != std::string::npos) break;
        }
        close(fd);
        const auto response = Json::parse(input);
        if (!response.value("accepted", false)) {
            throw std::runtime_error("agent rejected event: " + response.value("error", "unknown error"));
        }
        return response;
    } catch (...) {
        close(fd);
        throw;
    }
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

void publish_alarm(const AlarmTrigger& trigger, const StreamProfile& stream,
                   const yunshao::ModelProfile& model, const std::string& model_version,
                   const std::filesystem::path& socket_path, const std::vector<unsigned char>& jpeg) {
    const std::string device_id = env("YUNSHAO_DEVICE_ID");
    if (device_id.empty()) throw std::runtime_error("YUNSHAO_DEVICE_ID is required before enabling any alarm rule");
    const auto event_id = make_event_id();
    const auto evidence_directory = std::filesystem::path(env("YUNSHAO_EVIDENCE_OUTBOX_DIR", "/var/lib/yunshao/evidence-outbox"));
    const auto evidence = evidence_directory / (event_id + ".jpg");
    if (!jpeg.empty() && jpeg.size() <= 4 * 1024 * 1024) {
        try {
            std::filesystem::create_directories(evidence_directory);
            std::uintmax_t used = 0;
            for (const auto& item : std::filesystem::directory_iterator(evidence_directory))
                if (item.is_regular_file()) used += item.file_size();
            if (used + jpeg.size() > 100 * 1024 * 1024) throw std::runtime_error("evidence outbox is full");
            auto temporary = evidence; temporary += ".tmp";
            { std::ofstream output(temporary, std::ios::binary);
              output.write(reinterpret_cast<const char*>(jpeg.data()), static_cast<std::streamsize>(jpeg.size()));
              output.flush();
              if (!output) throw std::runtime_error("cannot persist evidence"); }
            std::filesystem::rename(temporary, evidence);
        } catch (const std::exception&) { std::cerr << "alarm_evidence_persist_failed\n"; }
    }
    Json event{{"schema_version", 1}, {"device_id", device_id}, {"event_id", event_id},
               {"camera_id", stream.camera_id}, {"event_type", trigger.event_type},
               {"severity", trigger.severity}, {"occurred_at", utc_now()}, {"sent_at", utc_now()},
               {"algorithm_key", model.algorithm_key}, {"algorithm_version", model_version},
               {"attributes", Json{{"model_id", trigger.model_id}, {trigger.class_attribute, trigger.class_name},
                                    {"confidence", trigger.confidence},
                                    {"rule_config_version", trigger.config_version},
                                    {"observed_duration_ms", trigger.observed_duration_ms}}}};
    const auto result = enqueue_event(socket_path, event);
    std::cout << "persisted classroom alarm event " << result.value("event_id", "") << '\n';
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
        wait_for_activation_lease();
        const auto& model = config.require_model(stream.model_id);
        const std::string input_uri = env(stream.source_ref.c_str());
        if (input_uri.empty()) throw std::runtime_error("configured camera source is not injected: " + stream.source_ref);

        ModelCatalog catalog(options.model_root, config);
        const Json model_state = catalog.status(stream.model_id);
        if (model_state.value("model_state", "") != "installed") {
            throw std::runtime_error("active model is not ready: " + model_state.dump());
        }
        const std::string model_version = model_state.value("model_version", "unknown");
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
        VideoStreamManager stream_manager(config, inference,
            [&](const AlarmTrigger& trigger, const std::vector<unsigned char>& jpeg) {
                publish_alarm(trigger, stream, model, model_version, options.socket_path, jpeg);
            }, options.config_dir);
        for (;;) {
            stream_manager.run(stream, input_uri, options.output, rtsp_server_config);
            if (!activation_lease_expired()) break;
            wait_for_activation_lease();
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
