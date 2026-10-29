#pragma once

#include <nlohmann/json.hpp>

#include <optional>
#include <string>

namespace yunshao::application::ports {

// Persistence and runtime-state boundary used while acknowledging an applied
// cloud rule command. Calls are serialized by the Agent configuration lock.
class AppliedCloudRuleState {
public:
    virtual ~AppliedCloudRuleState() = default;

    virtual std::optional<nlohmann::json> prepare_applied_ack(
        const std::string& previously_acknowledged_command_id) = 0;
    virtual void mark_ready(const nlohmann::json& command) = 0;
};

}  // namespace yunshao::application::ports
