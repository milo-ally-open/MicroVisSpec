#pragma once

#include "application/ports/agent_service_lifecycle.hpp"

namespace yunshao::adapters {

class SystemdAgentServiceLifecycle final : public application::ports::AgentServiceLifecycle {
public:
    void enable_and_restart_agent() override;
};

}  // namespace yunshao::adapters
