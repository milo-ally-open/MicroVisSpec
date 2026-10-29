#pragma once

#include "application/ports/agent_configuration_store.hpp"

#include <filesystem>

namespace yunshao::adapters {

class FileAgentConfigurationStore final : public application::ports::AgentConfigurationStore {
public:
    FileAgentConfigurationStore(std::filesystem::path agent_environment_path,
                                std::filesystem::path activation_overlay_path);

    Environment read_agent_environment() override;
    void write_agent_environment(Environment updates) override;
    void write_activation_overlay(Environment values) override;
    void remove_activation_overlay() override;

private:
    std::filesystem::path agent_environment_path_;
    std::filesystem::path activation_overlay_path_;
};

}  // namespace yunshao::adapters
