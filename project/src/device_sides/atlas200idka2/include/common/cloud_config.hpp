#pragma once
#include "common/config.hpp"

namespace yunshao {
Json read_json_file(const std::filesystem::path& path);
void write_json_atomic(const std::filesystem::path& path, const Json& value);
Json cloud_rule_parameters(const EdgeAppConfig& config, const std::string& model_id);
Json apply_cloud_rules(const std::filesystem::path& directory, const std::string& device_id,
                      const std::string& model_id, const std::string& model_version,
                      const Json& command);
Json fresh_runtime_state(const std::filesystem::path& path, const std::string& model_id);
Json cloud_event_payload(Json event);
}
