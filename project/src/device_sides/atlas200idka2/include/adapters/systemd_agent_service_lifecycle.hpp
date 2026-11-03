#pragma once

#include "application/ports/agent_service_lifecycle.hpp"

namespace microvis::adapters {

class SystemdAgentServiceLifecycle final : public application::ports::AgentServiceLifecycle {
public:
    void enable_and_restart_agent() override;
};

}  // namespace microvis::adapters
