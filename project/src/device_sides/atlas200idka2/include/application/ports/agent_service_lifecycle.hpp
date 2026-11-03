#pragma once

namespace microvis::application::ports {

class AgentServiceLifecycle {
public:
    virtual ~AgentServiceLifecycle() = default;

    virtual void enable_and_restart_agent() = 0;
};

}  // namespace microvis::application::ports
