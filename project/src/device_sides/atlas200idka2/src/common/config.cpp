#include "common/config.hpp"

#include <algorithm>
#include <cmath>
#include <cstdlib>
#include <fstream>
#include <regex>
#include <set>
#include <stdexcept>
#include <utility>

namespace yunshao {
namespace {

Json load_json(const std::filesystem::path& path) {
    std::ifstream input(path);
    if (!input) throw std::runtime_error("cannot open edge configuration: " + path.string());
    Json value;
    try {
        input >> value;
    } catch (const std::exception& error) {
        throw std::runtime_error("invalid JSON in " + path.string() + ": " + error.what());
    }
    if (!value.is_object()) throw std::runtime_error("configuration root must be an object: " + path.string());
    return value;
}

void require_object(const Json& value, const char* name) {
    if (!value.is_object()) throw std::invalid_argument(std::string(name) + " must be an object");
}

void require_keys(const Json& value, std::initializer_list<const char*> keys, const char* name) {
    require_object(value, name);
    for (const auto* key : keys) {
        if (!value.contains(key)) throw std::invalid_argument(std::string(name) + " is missing required key: " + key);
    }
}

bool valid_component(const std::string& value) {
    static const std::regex pattern("^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$");
    return value != "." && value != ".." && std::regex_match(value, pattern);
}

bool valid_model_id(const std::string& value) {
    static const std::regex pattern("^model_[a-z0-9]+(?:_[a-z0-9]+)*_(detection|segmentation|classification)$");
    return std::regex_match(value, pattern);
}

void validate_threshold(double value, const char* name) {
    if (!std::isfinite(value) || value < 0.0 || value > 1.0) {
        throw std::invalid_argument(std::string(name) + " must be finite and within [0,1]");
    }
}

bool contains_class(const ModelProfile& profile, const std::string& value) {
    for (const auto& item : profile.classes) if (item == value) return true;
    return false;
}

std::vector<std::string> string_values(const Json& parent, const char* key, const char* name) {
    if (!parent.contains(key) || !parent.at(key).is_array() || parent.at(key).empty()) {
        throw std::invalid_argument(std::string(name) + " must be a non-empty array");
    }
    std::vector<std::string> result;
    std::set<std::string> unique;
    for (const auto& value : parent.at(key)) {
        const auto item = value.get<std::string>();
        if (item.empty() || !unique.insert(item).second) {
            throw std::invalid_argument(std::string(name) + " contains an empty or duplicate value");
        }
        result.push_back(item);
    }
    return result;
}

bool includes(const std::vector<std::string>& values, const std::string& candidate) {
    return std::find(values.begin(), values.end(), candidate) != values.end();
}

}  // namespace

EdgeAppConfig EdgeAppConfig::load(const std::filesystem::path& directory) {
    return parse(load_json(directory / "models.json"),
                 load_json(directory / "streams_config.json"),
                 load_json(directory / "alarm_rules.json"));
}

EdgeAppConfig EdgeAppConfig::parse(const Json& models_json,
                                   const Json& streams_json,
                                   const Json& alarm_rules_json) {
    require_keys(models_json, {"schema_version", "runtime", "models"}, "models.json");
    require_keys(streams_json, {"schema_version", "global_settings", "streams"}, "streams_config.json");
    require_keys(alarm_rules_json, {"schema_version", "config_version", "allowed_severities", "alarm_rules"}, "alarm_rules.json");
    if (models_json.at("schema_version") != 1 || streams_json.at("schema_version") != 1 ||
        alarm_rules_json.at("schema_version") != 1) {
        throw std::invalid_argument("unsupported edge configuration schema_version");
    }

    EdgeAppConfig result;
    const Json& runtime = models_json.at("runtime");
    require_keys(runtime, {"npu_device_id", "max_loaded_models", "outbox_max_bytes",
                           "max_event_payload_bytes", "event_retry_base_seconds", "event_retry_max_seconds",
                           "capabilities"}, "models.json.runtime");
    result.npu_device_id = runtime.at("npu_device_id").get<int>();
    const auto max_loaded = runtime.at("max_loaded_models").get<int>();
    if (result.npu_device_id < 0 || max_loaded < 1 || max_loaded > 16) {
        throw std::invalid_argument("NPU device ID or max_loaded_models is out of range");
    }
    result.max_loaded_models = static_cast<std::size_t>(max_loaded);
    const auto outbox_max_bytes = runtime.at("outbox_max_bytes").get<std::int64_t>();
    const auto max_event_payload_bytes = runtime.at("max_event_payload_bytes").get<std::int64_t>();
    result.event_retry_base_seconds = runtime.at("event_retry_base_seconds").get<int>();
    result.event_retry_max_seconds = runtime.at("event_retry_max_seconds").get<int>();
    if (outbox_max_bytes < 1 || max_event_payload_bytes < 1 ||
        result.event_retry_base_seconds < 1 || result.event_retry_max_seconds < result.event_retry_base_seconds) {
        throw std::invalid_argument("event outbox size or retry limits are invalid");
    }
    result.outbox_max_bytes = static_cast<std::size_t>(outbox_max_bytes);
    result.max_event_payload_bytes = static_cast<std::size_t>(max_event_payload_bytes);

    const Json& capabilities = runtime.at("capabilities");
    require_keys(capabilities, {"formats", "backends", "model_families", "decoders", "pixel_formats",
                                "preprocessing", "soc_versions", "input_shapes"}, "models.json.runtime.capabilities");
    result.supported_formats = string_values(capabilities, "formats", "capabilities.formats");
    result.supported_backends = string_values(capabilities, "backends", "capabilities.backends");
    result.supported_model_families = string_values(capabilities, "model_families", "capabilities.model_families");
    result.supported_decoders = string_values(capabilities, "decoders", "capabilities.decoders");
    result.supported_pixel_formats = string_values(capabilities, "pixel_formats", "capabilities.pixel_formats");
    result.supported_preprocessing = string_values(capabilities, "preprocessing", "capabilities.preprocessing");
    result.supported_soc_versions = string_values(capabilities, "soc_versions", "capabilities.soc_versions");
    const auto& input_shapes = capabilities.at("input_shapes");
    if (!input_shapes.is_array() || input_shapes.empty()) {
        throw std::invalid_argument("capabilities.input_shapes must be a non-empty array");
    }
    std::set<std::pair<int, int>> unique_shapes;
    for (const auto& shape : input_shapes) {
        require_keys(shape, {"width", "height"}, "capabilities.input_shape");
        const std::pair<int, int> dimensions{shape.at("width").get<int>(), shape.at("height").get<int>()};
        if (dimensions.first < 1 || dimensions.second < 1 || !unique_shapes.insert(dimensions).second) {
            throw std::invalid_argument("capabilities.input_shapes contains invalid or duplicate dimensions");
        }
        result.supported_input_shapes.push_back(dimensions);
    }

    const Json& models = models_json.at("models");
    if (!models.is_array() || models.empty() || models.size() > 64) {
        throw std::invalid_argument("models.json.models must contain 1..64 profiles");
    }
    std::set<std::string> model_ids;
    for (const auto& value : models) {
        require_keys(value, {"model_id", "algorithm_key", "format", "model_file", "soc_version",
                             "model_family", "backend", "decoder", "plugin_library", "enabled",
                             "input", "classes", "confidence_threshold", "nms_threshold", "max_detections"},
                     "model profile");
        ModelProfile profile;
        profile.model_id = value.at("model_id").get<std::string>();
        profile.algorithm_key = value.at("algorithm_key").get<std::string>();
        profile.format = value.at("format").get<std::string>();
        profile.model_file = value.at("model_file").get<std::string>();
        profile.soc_version = value.at("soc_version").get<std::string>();
        profile.model_family = value.at("model_family").get<std::string>();
        profile.backend = value.at("backend").get<std::string>();
        profile.decoder = value.at("decoder").get<std::string>();
        profile.plugin_library = value.at("plugin_library").get<std::string>();
        profile.enabled = value.at("enabled").get<bool>();
        if (!valid_model_id(profile.model_id) || !model_ids.insert(profile.model_id).second ||
            !valid_component(profile.algorithm_key)) {
            throw std::invalid_argument("model_id must follow model_<domain>_<detection|segmentation|classification> and be unique");
        }
        if (!includes(result.supported_formats, profile.format) ||
            !includes(result.supported_backends, profile.backend) ||
            !includes(result.supported_model_families, profile.model_family) ||
            !includes(result.supported_decoders, profile.decoder) ||
            !includes(result.supported_soc_versions, profile.soc_version)) {
            throw std::invalid_argument("model profile selects a capability not declared by this runtime");
        }
        const std::filesystem::path model_file(profile.model_file);
        if (model_file.empty() || model_file.has_parent_path() || model_file.filename() != model_file) {
            throw std::invalid_argument("model_file must be a filename without a path");
        }
        const std::filesystem::path plugin(profile.plugin_library);
        const std::string expected_plugin = "lib" + profile.model_id + ".so";
        if (plugin.empty() || plugin.has_parent_path() || plugin.filename() != plugin ||
            plugin.extension() != ".so" || profile.plugin_library != expected_plugin) {
            throw std::invalid_argument("plugin_library must be lib<model_id>.so without a path");
        }
        const Json& input = value.at("input");
        require_keys(input, {"width", "height", "pixel_format", "preprocessing"}, "model profile.input");
        profile.input_width = input.at("width").get<int>();
        profile.input_height = input.at("height").get<int>();
        profile.pixel_format = input.at("pixel_format").get<std::string>();
        profile.preprocessing = input.at("preprocessing").get<std::string>();
        const std::pair<int, int> profile_dimensions{profile.input_width, profile.input_height};
        if (!includes(result.supported_pixel_formats, profile.pixel_format) ||
            !includes(result.supported_preprocessing, profile.preprocessing) ||
            std::find(result.supported_input_shapes.begin(), result.supported_input_shapes.end(), profile_dimensions) == result.supported_input_shapes.end()) {
            throw std::invalid_argument("model profile input is not declared by this runtime's capabilities");
        }
        if (!value.at("classes").is_array() || value.at("classes").empty() || value.at("classes").size() > 256) {
            throw std::invalid_argument("model profile classes must contain 1..256 entries");
        }
        std::set<std::string> classes;
        for (const auto& class_value : value.at("classes")) {
            const auto class_name = class_value.get<std::string>();
            if (class_name.empty() || class_name.size() > 96 || class_name.find_first_of("\r\n\t") != std::string::npos ||
                !classes.insert(class_name).second) {
                throw std::invalid_argument("model profile has an invalid or duplicate class name");
            }
            profile.classes.push_back(class_name);
        }
        profile.confidence_threshold = value.at("confidence_threshold").get<double>();
        profile.nms_threshold = value.at("nms_threshold").get<double>();
        profile.max_detections = value.at("max_detections").get<int>();
        validate_threshold(profile.confidence_threshold, "confidence_threshold");
        validate_threshold(profile.nms_threshold, "nms_threshold");
        if (profile.max_detections < 1 || profile.max_detections > 1000) {
            throw std::invalid_argument("max_detections must be within [1,1000]");
        }
        result.models.push_back(std::move(profile));
    }

    const Json& global = streams_json.at("global_settings");
    require_keys(global, {"output_fps"}, "streams_config.json.global_settings");
    const int default_fps = global.at("output_fps").get<int>();
    if (default_fps < 1 || default_fps > 60) throw std::invalid_argument("global output_fps must be within [1,60]");
    const Json& streams = streams_json.at("streams");
    if (!streams.is_array() || streams.empty() || streams.size() > 64) {
        throw std::invalid_argument("streams_config.json.streams must contain 1..64 entries");
    }
    std::set<std::string> stream_ids;
    for (const auto& value : streams) {
        require_keys(value, {"stream_id", "camera_id", "source_ref", "model_id", "enabled", "enable_ai",
                             "output_width", "output_height", "ai_fps"}, "stream profile");
        StreamProfile stream;
        stream.stream_id = value.at("stream_id").get<std::string>();
        stream.camera_id = value.at("camera_id").get<std::string>();
        stream.source_ref = value.at("source_ref").get<std::string>();
        stream.model_id = value.at("model_id").get<std::string>();
        stream.enabled = value.at("enabled").get<bool>();
        stream.enable_ai = value.at("enable_ai").get<bool>();
        stream.output_width = value.at("output_width").get<int>();
        stream.output_height = value.at("output_height").get<int>();
        stream.output_fps = value.value("output_fps", default_fps);
        stream.ai_fps = value.at("ai_fps").get<int>();
        if (!valid_component(stream.stream_id) || !stream_ids.insert(stream.stream_id).second ||
            stream.camera_id.empty() || stream.camera_id.size() > 128) {
            throw std::invalid_argument("stream ID is invalid/duplicated or camera_id is invalid");
        }
        static const std::regex env_name("^[A-Z][A-Z0-9_]{2,127}$");
        if (!std::regex_match(stream.source_ref, env_name)) {
            throw std::invalid_argument("stream source_ref must name an injected environment variable");
        }
        if (stream.output_width < 64 || stream.output_width > 7680 || stream.output_width % 2 != 0 ||
            stream.output_height < 64 || stream.output_height > 4320 || stream.output_height % 2 != 0 ||
            stream.output_fps < 1 || stream.output_fps > 60 || stream.ai_fps < 1 || stream.ai_fps > 60) {
            throw std::invalid_argument("stream output dimensions or frame rates are out of range");
        }
        if (value.contains("confidence_threshold")) {
            stream.confidence_threshold = value.at("confidence_threshold").get<double>();
            validate_threshold(*stream.confidence_threshold, "stream confidence_threshold");
        }
        if (value.contains("nms_threshold")) {
            stream.nms_threshold = value.at("nms_threshold").get<double>();
            validate_threshold(*stream.nms_threshold, "stream nms_threshold");
        }
        const auto model = std::find_if(result.models.begin(), result.models.end(), [&](const ModelProfile& candidate) {
            return candidate.model_id == stream.model_id;
        });
        if (model == result.models.end()) throw std::invalid_argument("stream references an unknown model_id");
        if (stream.enabled && stream.enable_ai && !model->enabled) {
            throw std::invalid_argument("enabled AI stream references a disabled model profile");
        }
        result.streams.push_back(std::move(stream));
    }

    result.alarm_config_version = alarm_rules_json.at("config_version").get<int>();
    result.allowed_severities = string_values(alarm_rules_json, "allowed_severities", "alarm_rules.json.allowed_severities");
    if (result.alarm_config_version < 1) throw std::invalid_argument("alarm config_version must be positive");
    const Json& rules = alarm_rules_json.at("alarm_rules");
    if (!rules.is_array() || rules.size() > 1024) throw std::invalid_argument("alarm_rules must be an array of at most 1024 rules");
    std::set<std::string> rule_keys;
    for (const auto& value : rules) {
        require_keys(value, {"model_id", "class_name", "event_type", "class_attribute", "enabled", "severity", "confidence_threshold",
                             "min_duration_ms", "cooldown_ms"}, "alarm rule");
        AlarmRule rule;
        rule.model_id = value.at("model_id").get<std::string>();
        rule.class_name = value.at("class_name").get<std::string>();
        rule.event_type = value.at("event_type").get<std::string>();
        rule.class_attribute = value.at("class_attribute").get<std::string>();
        rule.enabled = value.at("enabled").get<bool>();
        rule.severity = value.at("severity").get<std::string>();
        rule.confidence_threshold = value.at("confidence_threshold").get<double>();
        rule.min_duration_ms = value.at("min_duration_ms").get<int>();
        rule.cooldown_ms = value.at("cooldown_ms").get<int>();
        const auto model = std::find_if(result.models.begin(), result.models.end(), [&](const ModelProfile& candidate) {
            return candidate.model_id == rule.model_id;
        });
        if (model == result.models.end() || !contains_class(*model, rule.class_name)) {
            throw std::invalid_argument("alarm rule references an unknown model or class");
        }
        if (rule.class_name.empty() || !includes(result.allowed_severities, rule.severity) ||
            !std::regex_match(rule.event_type, std::regex("^[a-z][a-z0-9._-]{1,127}$")) ||
            !std::regex_match(rule.class_attribute, std::regex("^[A-Za-z][A-Za-z0-9_]{0,63}$"))) {
            throw std::invalid_argument("alarm rule class or severity is invalid");
        }
        validate_threshold(rule.confidence_threshold, "alarm confidence_threshold");
        if (rule.min_duration_ms < 0 || rule.min_duration_ms > 3600000 || rule.cooldown_ms < 0 || rule.cooldown_ms > 86400000) {
            throw std::invalid_argument("alarm rule duration is out of range");
        }
        if (rule.enabled && rule.min_duration_ms < 1) {
            throw std::invalid_argument("enabled alarm rules require a positive min_duration_ms");
        }
        if (!rule_keys.insert(rule.model_id + "\n" + rule.class_name).second) {
            throw std::invalid_argument("alarm rule is duplicated for a model/class pair");
        }
        result.alarm_rules.push_back(std::move(rule));
    }
    return result;
}

const ModelProfile& EdgeAppConfig::require_model(const std::string& model_id, bool require_enabled) const {
    for (const auto& model : models) {
        if (model.model_id == model_id) {
            if (require_enabled && !model.enabled) throw std::invalid_argument("model profile is disabled: " + model_id);
            return model;
        }
    }
    throw std::invalid_argument("unknown model_id: " + model_id);
}

const StreamProfile& EdgeAppConfig::require_stream(const std::string& stream_id) const {
    for (const auto& stream : streams) if (stream.stream_id == stream_id) return stream;
    throw std::invalid_argument("unknown stream_id: " + stream_id);
}

const AlarmRule* EdgeAppConfig::find_rule(const std::string& model_id, const std::string& class_name) const {
    for (const auto& rule : alarm_rules) {
        if (rule.model_id == model_id && rule.class_name == class_name) return &rule;
    }
    return nullptr;
}

}  // namespace yunshao
