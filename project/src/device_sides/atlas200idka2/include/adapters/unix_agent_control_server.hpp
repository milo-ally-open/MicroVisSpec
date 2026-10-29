#pragma once

#include "application/ports/local_agent_control.hpp"

#include <atomic>
#include <filesystem>

namespace yunshao::adapters {

class UnixAgentControlServer {
public:
    UnixAgentControlServer(application::ports::LocalAgentControl& control,
                           std::filesystem::path socket_path);

    void run(const std::atomic_bool& stopping) const;

private:
    application::ports::LocalAgentControl& control_;
    std::filesystem::path socket_path_;
};

}  // namespace yunshao::adapters
