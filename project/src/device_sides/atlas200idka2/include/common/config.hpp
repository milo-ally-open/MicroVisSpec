#pragma once

#include <nlohmann/json.hpp>

#include <filesystem>
#include <optional>
#include <string>
#include <utility>
#include <vector>

namespace yunshao {

using Json = nlohmann::json;

struct ModelProfile {
    std::string model_id;
    std::string algorithm_key;
    std::string format;
    std::string model_file;
    std::string soc_version;
    std::string model_family;
    std::string backend;
    std::string decoder;
    std::string plugin_library;
    bool enabled = false;
    int input_width = 0;
    int input_height = 0;
    std::string pixel_format;
    std::string preprocessing;
    std::vector<std::string> classes;
    double confidence_threshold = 0.25;
    double nms_threshold = 0.45;
    int max_detections = 300;
};

struct StreamProfile {
    std::string stream_id;
    std::string camera_id;
    std::string source_ref;
    std::string model_id;
    bool enabled = false;
    bool enable_ai = false;
    bool live_source = false;
    int output_width = 0;
    int output_height = 0;
    int output_fps = 0;
    int ai_fps = 0;
    int output_bitrate_kbps = 1500;
    int input_timeout_ms = 5000;
    int reconnect_initial_delay_ms = 1000;
    int reconnect_max_delay_ms = 30000;
    std::string rtsp_transport = "tcp";
    std::optional<double> confidence_threshold;
    std::optional<double> nms_threshold;
};

struct AlarmRule {
    std::string model_id;
    std::string class_name;
    std::string event_type;
    std::string class_attribute;
    bool enabled = false;
    std::string severity;
    double confidence_threshold = 0.0;
    int min_duration_ms = 0;
    int cooldown_ms = 0;
};

class EdgeAppConfig {
public:
    int schema_version = 1;
    int alarm_config_version = 1;
    int npu_device_id = 0;
    std::size_t max_loaded_models = 1;
    std::size_t outbox_max_bytes = 0;
    std::size_t max_event_payload_bytes = 0;
    int event_retry_base_seconds = 0;
    int event_retry_max_seconds = 0;
    std::vector<std::string> supported_formats;
    std::vector<std::string> supported_backends;
    std::vector<std::string> supported_model_families;
    std::vector<std::string> supported_decoders;
    std::vector<std::string> supported_pixel_formats;
    std::vector<std::string> supported_preprocessing;
    std::vector<std::string> supported_soc_versions;
    std::vector<std::pair<int, int>> supported_input_shapes;
    std::vector<std::string> allowed_severities;
    std::vector<ModelProfile> models;
    std::vector<StreamProfile> streams;
    std::vector<AlarmRule> alarm_rules;

    static EdgeAppConfig load(const std::filesystem::path& directory);
    static EdgeAppConfig parse(const Json& models_json,
                               const Json& streams_json,
                               const Json& alarm_rules_json);

    const ModelProfile& require_model(const std::string& model_id, bool require_enabled = true) const;
    const StreamProfile& require_stream(const std::string& stream_id) const;
    const AlarmRule* find_rule(const std::string& model_id, const std::string& class_name) const;
};

}  // namespace yunshao
