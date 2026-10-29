#include "adapters/file_runtime_rule_state_reader.hpp"

#include "common/cloud_config.hpp"

#include <ctime>
#include <cstdint>
#include <utility>

namespace yunshao::adapters {

FileRuntimeRuleStateReader::FileRuntimeRuleStateReader(std::filesystem::path config_directory)
    : config_directory_(std::move(config_directory)) {}

application::ports::RuntimeRuleState FileRuntimeRuleStateReader::read() {
    const auto config = EdgeAppConfig::load(config_directory_);
    const auto command = read_json_file(config_directory_ / "alarm_rules.json")
                             .value("cloud_command", Json::object());
    bool ready = command.empty();
    if (!ready) {
        try {
            const auto marker = read_json_file(config_directory_ / "cloud-ready.json");
            ready = marker.value("command_id", "") == command.value("command_id", "") &&
                    marker.value("updated_at_unix", std::int64_t{0}) < std::time(nullptr);
        } catch (...) {
            ready = false;
        }
    }
    return {config.alarm_rules, config.alarm_config_version, command, ready};
}

}  // namespace yunshao::adapters
