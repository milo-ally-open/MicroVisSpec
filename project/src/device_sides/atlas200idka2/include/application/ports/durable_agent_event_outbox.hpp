#pragma once

#include <nlohmann/json.hpp>

#include <cstddef>
#include <string>
#include <vector>

namespace yunshao {
struct EdgeAppConfig;
}

namespace yunshao::application::ports {

// Application contract for durable local event storage and acknowledgement state.
class DurableAgentEventOutbox {
public:
    virtual ~DurableAgentEventOutbox() = default;

    virtual bool enqueue(const nlohmann::json& event, std::string* error) = 0;
    virtual std::vector<nlohmann::json> due_events(std::size_t limit = 32) = 0;
    virtual void note_publish_attempt(const std::string& event_id,
                                      const std::string& error = "") = 0;
    virtual bool apply_ack(const nlohmann::json& ack, std::string* error) = 0;
    virtual nlohmann::json counts() = 0;
    virtual void replace_config(EdgeAppConfig config) = 0;
    virtual std::string event_state(const std::string& event_id) = 0;
};

}  // namespace yunshao::application::ports
