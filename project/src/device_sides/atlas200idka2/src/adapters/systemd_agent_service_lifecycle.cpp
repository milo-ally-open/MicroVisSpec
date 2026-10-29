#include "adapters/systemd_agent_service_lifecycle.hpp"

#include <cerrno>
#include <stdexcept>
#include <string>
#include <sys/wait.h>
#include <unistd.h>
#include <vector>

namespace yunshao::adapters {
namespace {

void run_systemctl(const std::vector<std::string>& arguments) {
    const pid_t child = ::fork();
    if (child < 0) throw std::runtime_error("无法启动 systemd 命令");
    if (child == 0) {
        std::vector<char*> argv;
        argv.reserve(arguments.size() + 2);
        argv.push_back(const_cast<char*>("systemctl"));
        for (const auto& argument : arguments) argv.push_back(const_cast<char*>(argument.c_str()));
        argv.push_back(nullptr);
        ::execv("/usr/bin/systemctl", argv.data());
        ::execv("/bin/systemctl", argv.data());
        ::_exit(127);
    }

    int status = 0;
    while (::waitpid(child, &status, 0) < 0) {
        if (errno == EINTR) continue;
        throw std::runtime_error("无法等待 systemd 命令结束");
    }
    if (!WIFEXITED(status) || WEXITSTATUS(status) != 0)
        throw std::runtime_error("systemd 命令执行失败");
}

}  // namespace

void SystemdAgentServiceLifecycle::enable_and_restart_agent() {
    run_systemctl({"enable", "--now", "yunshao-agent.service"});
    run_systemctl({"restart", "yunshao-agent.service"});
}

}  // namespace yunshao::adapters
