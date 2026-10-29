#pragma once

#include <map>
#include <string>

namespace yunshao::application::ports {

class AgentConfigurationStore {
public:
    using Environment = std::map<std::string, std::string>;

    virtual ~AgentConfigurationStore() = default;

    virtual Environment read_agent_environment() = 0;
    virtual void write_agent_environment(Environment updates) = 0;
    virtual void write_activation_overlay(Environment values) = 0;
    virtual void remove_activation_overlay() = 0;
};

}  // namespace yunshao::application::ports
