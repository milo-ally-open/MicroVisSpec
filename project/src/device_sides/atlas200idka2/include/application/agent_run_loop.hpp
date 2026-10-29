#pragma once

#include "application/agent_cloud_cycle.hpp"
#include "application/agent_mqtt_session.hpp"

#include <chrono>
#include <functional>

namespace yunshao::application {

class AgentRunLoop {
public:
    using ActivationPoll = std::function<bool()>;
    using StopRequested = std::function<bool()>;
    using Sleeper = std::function<void(std::chrono::seconds)>;

    AgentRunLoop(AgentMqttSession& mqtt_session,
                 AgentCloudCycle& cloud_cycle,
                 ActivationPoll activation_poll,
                 StopRequested stop_requested,
                 Sleeper sleeper);

    bool run();

private:
    AgentMqttSession& mqtt_session_;
    AgentCloudCycle& cloud_cycle_;
    ActivationPoll activation_poll_;
    StopRequested stop_requested_;
    Sleeper sleeper_;
};

}  // namespace yunshao::application
