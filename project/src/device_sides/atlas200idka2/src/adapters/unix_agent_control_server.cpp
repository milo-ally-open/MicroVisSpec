#include "adapters/unix_agent_control_server.hpp"

#include <sys/socket.h>
#include <sys/stat.h>
#include <sys/un.h>
#include <unistd.h>
#include <poll.h>

#include <cerrno>
#include <chrono>
#include <cstring>
#include <iostream>
#include <stdexcept>
#include <string>
#include <thread>
#include <utility>

namespace yunshao::adapters {
namespace {

using Json = nlohmann::json;

Json handle_request(application::ports::LocalAgentControl& control, const Json& request) {
    if (!request.is_object()) throw std::invalid_argument("request must be a JSON object");
    const std::string command = request.value("command", "");
    if (command == "status") return control.status();
    if (command == "enqueue_event") {
        if (!request.contains("event")) throw std::invalid_argument("event is required");
        const auto result = control.enqueue_event(request.at("event"));
        if (!result.accepted) return Json{{"accepted", false}, {"error", result.error}};
        return Json{{"accepted", true}, {"event_id", result.event_id}};
    }
    throw std::invalid_argument("unsupported command");
}

void write_all(int fd, const std::string& value) {
    std::size_t offset = 0;
    while (offset < value.size()) {
        const ssize_t count = write(fd, value.data() + offset, value.size() - offset);
        if (count < 0 && errno == EINTR) continue;
        if (count <= 0) throw std::runtime_error("cannot send local agent response");
        offset += static_cast<std::size_t>(count);
    }
}

}  // namespace

UnixAgentControlServer::UnixAgentControlServer(application::ports::LocalAgentControl& control,
                                               std::filesystem::path socket_path)
    : control_(control), socket_path_(std::move(socket_path)) {}

void UnixAgentControlServer::run(const std::atomic_bool& stopping) const {
    std::filesystem::create_directories(socket_path_.parent_path());
    struct stat existing{};
    if (lstat(socket_path_.c_str(), &existing) == 0) {
        if (!S_ISSOCK(existing.st_mode)) throw std::runtime_error("IPC path exists and is not a Unix socket");
        if (unlink(socket_path_.c_str()) != 0) throw std::runtime_error("cannot replace stale IPC socket");
    }

    const int fd = socket(AF_UNIX, SOCK_STREAM | SOCK_CLOEXEC, 0);
    if (fd < 0) throw std::runtime_error("cannot create local IPC socket");
    sockaddr_un address{};
    address.sun_family = AF_UNIX;
    const std::string path_string = socket_path_.string();
    if (path_string.size() >= sizeof(address.sun_path)) {
        close(fd);
        throw std::runtime_error("IPC socket path is too long");
    }
    std::strncpy(address.sun_path, path_string.c_str(), sizeof(address.sun_path) - 1);
    if (bind(fd, reinterpret_cast<sockaddr*>(&address), sizeof(address)) != 0 ||
        chmod(socket_path_.c_str(), 0660) != 0 || listen(fd, 32) != 0) {
        close(fd);
        unlink(socket_path_.c_str());
        throw std::runtime_error("cannot initialize local IPC socket");
    }
    std::cout << "agent IPC listening at " << path_string << '\n';
    while (!stopping.load()) {
        pollfd ready{fd, POLLIN, 0};
        const int poll_result = poll(&ready, 1, 500);
        if (poll_result <= 0 || !(ready.revents & POLLIN)) continue;
        const int client = accept4(fd, nullptr, nullptr, SOCK_CLOEXEC);
        if (client < 0) {
            if (errno == EINTR) continue;
            if (stopping.load()) break;
            std::this_thread::sleep_for(std::chrono::milliseconds(50));
            continue;
        }

        std::string input;
        char buffer[4096];
        while (input.size() <= 64 * 1024) {
            const auto count = read(client, buffer, sizeof(buffer));
            if (count <= 0) break;
            input.append(buffer, static_cast<std::size_t>(count));
            if (input.find('\n') != std::string::npos) break;
        }
        Json response;
        try {
            const auto newline = input.find('\n');
            response = handle_request(control_, Json::parse(input.substr(0, newline)));
        } catch (const std::exception& exception) {
            response = Json{{"accepted", false}, {"error", exception.what()}};
        }
        try {
            write_all(client, response.dump() + "\n");
        } catch (...) {
            // A disconnected local client does not stop the agent service loop.
        }
        close(client);
    }
    close(fd);
    unlink(socket_path_.c_str());
}

}  // namespace yunshao::adapters
