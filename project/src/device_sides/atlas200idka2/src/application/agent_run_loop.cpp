#include "application/agent_run_loop.hpp"

#include <stdexcept>
#include <utility>

namespace yunshao::application {

AgentRunLoop::AgentRunLoop(AgentMqttSession& mqtt_session,
                           AgentCloudCycle& cloud_cycle,
                           ActivationPoll activation_poll,
                           StopRequested stop_requested,
                           Sleeper sleeper)
    : mqtt_session_(mqtt_session), cloud_cycle_(cloud_cycle),
      activation_poll_(std::move(activation_poll)),
      stop_requested_(std::move(stop_requested)), sleeper_(std::move(sleeper)) {
    if (!activation_poll_ || !stop_requested_ || !sleeper_) {
        throw std::invalid_argument("agent run loop requires poll, stop, and sleep callbacks");
    }
}

bool AgentRunLoop::run() {
    mqtt_session_.start();
    auto next_activation_poll = std::chrono::steady_clock::time_point{};
    while (!stop_requested_()) {
        const auto now = std::chrono::steady_clock::now();
        if (now >= next_activation_poll) {
            if (activation_poll_()) return true;
            next_activation_poll = now + std::chrono::seconds(15);
        }
        cloud_cycle_.run_if_connected();
        sleeper_(std::chrono::seconds(5));
    }
    return false;
}

}  // namespace yunshao::application
