#pragma once

namespace yunshao::application::ports {

class AgentServiceLifecycle {
public:
    virtual ~AgentServiceLifecycle() = default;

    virtual void enable_and_restart_agent() = 0;
};

}  // namespace yunshao::application::ports
