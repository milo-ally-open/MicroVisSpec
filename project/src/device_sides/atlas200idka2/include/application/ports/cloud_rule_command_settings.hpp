#pragma once

#include <nlohmann/json.hpp>

namespace yunshao::application::ports {

// Applies one already-decoded cloud rule command to the active local
// configuration. Implementations must make the update atomic with respect to
// readers of the live configuration.
class CloudRuleCommandSettings {
public:
    virtual ~CloudRuleCommandSettings() = default;
    virtual void apply(const nlohmann::json& command) = 0;
};

}  // namespace yunshao::application::ports
