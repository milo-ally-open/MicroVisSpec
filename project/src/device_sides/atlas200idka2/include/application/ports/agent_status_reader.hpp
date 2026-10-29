#pragma once

#include <nlohmann/json.hpp>

namespace yunshao::application::ports {

// Narrow read-only facet used by local diagnostics and cloud heartbeats.
class AgentStatusReader {
public:
    virtual ~AgentStatusReader() = default;
    virtual nlohmann::json status() = 0;
};

}  // namespace yunshao::application::ports
