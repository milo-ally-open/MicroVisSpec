#pragma once

#include "application/ports/agent_status_reader.hpp"

#include <nlohmann/json.hpp>

#include <string>

namespace yunshao::application::ports {

struct EventEnqueueResult {
    bool accepted = false;
    std::string event_id;
    std::string error;
};

// Inbound application contract used by local control transports.
class LocalAgentControl : public AgentStatusReader {
public:
    virtual ~LocalAgentControl() = default;
    virtual EventEnqueueResult enqueue_event(const nlohmann::json& event) = 0;
};

}  // namespace yunshao::application::ports
