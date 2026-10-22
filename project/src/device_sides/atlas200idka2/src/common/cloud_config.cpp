#include "common/cloud_config.hpp"
#include <ctime>
#include <fstream>
#include <limits>
#include <stdexcept>

namespace yunshao {
Json read_json_file(const std::filesystem::path& path) {
    std::ifstream input(path);
    if (!input) throw std::runtime_error("cannot read JSON document: " + path.string());
    Json value; input >> value;
    if (!value.is_object()) throw std::invalid_argument("JSON document must be an object");
    return value;
}

void write_json_atomic(const std::filesystem::path& path, const Json& value) {
    std::filesystem::create_directories(path.parent_path());
    auto temporary = path; temporary += ".tmp";
    {
        std::ofstream output(temporary, std::ios::trunc);
        output << value.dump(2) << '\n';
        output.flush();
        if (!output) throw std::runtime_error("cannot persist JSON document");
    }
    std::filesystem::rename(temporary, path);
}

Json cloud_rule_parameters(const EdgeAppConfig& config, const std::string& model_id) {
    Json rules = Json::object();
    for (const auto& name : config.require_model(model_id).classes) {
        const auto* rule = config.find_rule(model_id, name);
        if (!rule) { rules[name] = Json{{"enabled", false}}; continue; }
        rules[name] = Json{{"enabled", rule->enabled}, {"confidence_threshold", rule->confidence_threshold},
                           {"minimum_duration_ms", rule->min_duration_ms}, {"cooldown_ms", rule->cooldown_ms},
                           {"severity", rule->severity}};
    }
    return Json{{"classroom_rules", rules}};
}

Json apply_cloud_rules(const std::filesystem::path& directory, const std::string& device_id,
                      const std::string& model_id, const std::string& model_version,
                      const Json& command) {
    const auto config = EdgeAppConfig::load(directory);
    const auto& model = config.require_model(model_id);
    if (command.value("schema_version", 0) != 1 || command.value("device_id", "") != device_id ||
        command.value("algorithm_key", "") != model.algorithm_key ||
        command.value("algorithm_version", "") != model_version ||
        command.value("command_id", "").size() != 36) throw std::invalid_argument("command identity mismatch");
    const auto version = command.at("expected_version").get<std::int64_t>();
    if (version < 1 || version > std::numeric_limits<int>::max()) throw std::invalid_argument("invalid rule version");
    const auto& parameters = command.at("parameters");
    if (!parameters.is_object() || parameters.size() != 1 || !parameters.contains("classroom_rules"))
        throw std::invalid_argument("unsupported rule parameters");
    const auto& rules = parameters.at("classroom_rules");
    if (!rules.is_object() || rules.size() != model.classes.size()) throw std::invalid_argument("all model classes are required");
    auto document = read_json_file(directory / "alarm_rules.json");
    const auto previous = document.value("cloud_command", Json::object());
    if (version < previous.value("expected_version", std::int64_t{0})) throw std::invalid_argument("stale rule version");
    if (version == previous.value("expected_version", std::int64_t{0})) {
        if (previous.value("command_id", "") != command.value("command_id", "") ||
            previous.at("parameters") != parameters) throw std::invalid_argument("conflicting rule version");
        return previous;
    }
    for (const auto& name : model.classes) {
        const auto& rule = rules.at(name);
        if (!rule.is_object() || !rule.at("enabled").is_boolean()) throw std::invalid_argument("enabled must be explicit");
        for (auto it = rule.begin(); it != rule.end(); ++it)
            if (it.key() != "enabled" && it.key() != "confidence_threshold" && it.key() != "minimum_duration_ms" &&
                it.key() != "cooldown_ms" && it.key() != "severity") throw std::invalid_argument("unsupported rule field");
        if (rule.at("enabled").get<bool>())
            for (const auto* key : {"confidence_threshold", "minimum_duration_ms", "cooldown_ms", "severity"})
                if (!rule.contains(key)) throw std::invalid_argument("enabled rule is incomplete");
        bool found = false;
        for (auto& target : document.at("alarm_rules")) {
            if (target.at("model_id") != model_id || target.at("class_name") != name) continue;
            found = true;
            target["enabled"] = rule.at("enabled");
            for (const auto* key : {"confidence_threshold", "cooldown_ms", "severity"})
                if (rule.contains(key)) target[key] = rule.at(key);
            if (rule.contains("minimum_duration_ms")) target["min_duration_ms"] = rule.at("minimum_duration_ms");
        }
        if (!found) throw std::invalid_argument("model class has no local alarm mapping");
    }
    document["config_version"] = version;
    document["cloud_command"] = command;
    // Validate the complete prospective snapshot before replacing any live file.
    EdgeAppConfig::parse(read_json_file(directory / "models.json"), read_json_file(directory / "streams_config.json"), document);
    write_json_atomic(directory / "alarm_rules.json", document);
    return command;
}

Json fresh_runtime_state(const std::filesystem::path& path, const std::string& model_id) {
    try {
        auto state = read_json_file(path);
        const auto age = std::time(nullptr) - state.at("updated_at_unix").get<std::int64_t>();
        if (age < 0 || age > 10 || state.value("model_id", "") != model_id) return Json::object();
        return state;
    } catch (...) { return Json::object(); }
}

Json cloud_event_payload(Json event) {
    // model_id is local routing metadata. The wire contract identifies the
    // registered algorithm and rejects extra classroom attributes.
    event.at("attributes").erase("model_id");
    return event;
}
}
