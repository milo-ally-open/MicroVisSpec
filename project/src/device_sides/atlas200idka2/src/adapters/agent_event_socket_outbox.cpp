#include "adapters/agent_event_socket_outbox.hpp"

#include <sys/socket.h>
#include <sys/un.h>
#include <unistd.h>

#include <cerrno>
#include <cstring>
#include <stdexcept>
#include <string>
#include <utility>

namespace yunshao::adapters {
namespace {

class ScopedFd {
public:
    explicit ScopedFd(int fd) noexcept : fd_(fd) {}
    ~ScopedFd() { close_now(); }

    ScopedFd(const ScopedFd&) = delete;
    ScopedFd& operator=(const ScopedFd&) = delete;

    int get() const noexcept { return fd_; }

    void close_now() noexcept {
        if (fd_ >= 0) {
            close(fd_);
            fd_ = -1;
        }
    }

private:
    int fd_;
};

void write_all(int fd, const std::string& value) {
    std::size_t offset = 0;
    while (offset < value.size()) {
        const ssize_t count = write(fd, value.data() + offset, value.size() - offset);
        if (count < 0 && errno == EINTR) continue;
        if (count <= 0) throw std::runtime_error("cannot send local agent request");
        offset += static_cast<std::size_t>(count);
    }
}

}  // namespace

AgentEventSocketOutbox::AgentEventSocketOutbox(std::filesystem::path socket_path)
    : socket_path_(std::move(socket_path)) {}

nlohmann::json AgentEventSocketOutbox::enqueue(const nlohmann::json& event) {
    ScopedFd fd(socket(AF_UNIX, SOCK_STREAM | SOCK_CLOEXEC, 0));
    if (fd.get() < 0) throw std::runtime_error("cannot create event IPC socket");

    sockaddr_un address{};
    address.sun_family = AF_UNIX;
    const std::string path = socket_path_.string();
    if (path.size() >= sizeof(address.sun_path)) {
        throw std::runtime_error("agent socket path is too long");
    }
    std::strncpy(address.sun_path, path.c_str(), sizeof(address.sun_path) - 1);
    if (connect(fd.get(), reinterpret_cast<sockaddr*>(&address), sizeof(address)) != 0) {
        throw std::runtime_error("agent event queue is unavailable");
    }

    write_all(fd.get(), nlohmann::json{{"command", "enqueue_event"}, {"event", event}}.dump() + "\n");
    std::string input;
    char buffer[4096];
    while (input.size() <= 64 * 1024) {
        const auto count = read(fd.get(), buffer, sizeof(buffer));
        if (count < 0 && errno == EINTR) continue;
        if (count <= 0) break;
        input.append(buffer, static_cast<std::size_t>(count));
        if (input.find('\n') != std::string::npos) break;
    }
    fd.close_now();
    const auto response = nlohmann::json::parse(input);
    if (!response.value("accepted", false)) {
        throw std::runtime_error("agent rejected event: " + response.value("error", "unknown error"));
    }
    return response;
}

}  // namespace yunshao::adapters
