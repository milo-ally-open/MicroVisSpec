#pragma once

#include <nlohmann/json.hpp>

namespace microvis::application::ports {

// Outbound port for handing a runtime alarm event to the local agent.
class AlarmEventSink {
public:
    virtual ~AlarmEventSink() = default;
    virtual nlohmann::json enqueue(const nlohmann::json& event) = 0;
};

}  // namespace microvis::application::ports
