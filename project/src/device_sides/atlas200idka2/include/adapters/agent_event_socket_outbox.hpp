#pragma once

#include "application/ports/alarm_event_sink.hpp"

#include <filesystem>

namespace yunshao::adapters {

class AgentEventSocketOutbox final : public application::ports::AlarmEventSink {
public:
    explicit AgentEventSocketOutbox(std::filesystem::path socket_path);

    nlohmann::json enqueue(const nlohmann::json& event) override;

private:
    std::filesystem::path socket_path_;
};

}  // namespace yunshao::adapters
