#include "adapters/file_agent_configuration_store.hpp"

#include "application/sensitive_string.hpp"

#include <algorithm>
#include <cerrno>
#include <cstring>
#include <fcntl.h>
#include <fstream>
#include <grp.h>
#include <set>
#include <stdexcept>
#include <string>
#include <string_view>
#include <sys/stat.h>
#include <sys/types.h>
#include <unistd.h>
#include <utility>
#include <vector>

namespace yunshao::adapters {
namespace {

using Environment = application::ports::AgentConfigurationStore::Environment;

void wipe(std::string& value) {
    volatile char* data = value.empty() ? nullptr : value.data();
    for (std::size_t index = 0; index < value.size(); ++index) data[index] = '\0';
    value.clear();
}

std::string_view environment_key(const std::string& line) {
    const auto separator = line.find('=');
    if (separator == std::string::npos || separator == 0) return {};
    auto first = line.find_first_not_of(" \t\r\n");
    if (first == std::string::npos || first >= separator) return {};
    auto last = line.find_last_not_of(" \t\r\n", separator - 1);
    if (last == std::string::npos || last < first) return {};
    return std::string_view(line.data() + first, last - first + 1);
}

bool is_mqtt_password(const std::string& line) {
    return environment_key(line) == "YUNSHAO_MQTT_PASSWORD";
}

void wipe_mqtt_password(Environment& environment) {
    const auto found = environment.find("YUNSHAO_MQTT_PASSWORD");
    if (found != environment.end()) wipe(found->second);
}

class EnvironmentPasswordGuard {
public:
    explicit EnvironmentPasswordGuard(Environment& environment) : environment_(environment) {}
    ~EnvironmentPasswordGuard() {
        if (active_) wipe_mqtt_password(environment_);
    }
    void release() { active_ = false; }

private:
    Environment& environment_;
    bool active_ = true;
};

class PasswordLineGuard {
public:
    explicit PasswordLineGuard(std::vector<std::string>& lines) : lines_(lines) {}
    ~PasswordLineGuard() {
        for (auto& line : lines_) {
            if (is_mqtt_password(line)) wipe(line);
        }
    }

private:
    std::vector<std::string>& lines_;
};

std::string trim_range(const std::string& value, std::size_t begin, std::size_t end) {
    if (begin >= end || begin >= value.size()) return {};
    end = std::min(end, value.size());
    const auto first = value.find_first_not_of(" \t\r\n", begin);
    if (first == std::string::npos || first >= end) return {};
    const auto last = value.find_last_not_of(" \t\r\n", end - 1);
    if (last == std::string::npos || last < first) return {};
    return value.substr(first, last - first + 1);
}

void write_fully(int descriptor, const std::string& contents, const char* error_message) {
    std::size_t offset = 0;
    while (offset < contents.size()) {
        const auto written = ::write(descriptor, contents.data() + offset, contents.size() - offset);
        if (written < 0 && errno == EINTR) continue;
        if (written <= 0) throw std::runtime_error(error_message);
        offset += static_cast<std::size_t>(written);
    }
}

}  // namespace

FileAgentConfigurationStore::FileAgentConfigurationStore(
    std::filesystem::path agent_environment_path,
    std::filesystem::path activation_overlay_path)
    : agent_environment_path_(std::move(agent_environment_path)),
      activation_overlay_path_(std::move(activation_overlay_path)) {}

application::ports::AgentConfigurationStore::Environment
FileAgentConfigurationStore::read_agent_environment() {
    Environment values;
    EnvironmentPasswordGuard password_guard(values);
    std::ifstream input(agent_environment_path_);
    if (!input && std::filesystem::exists(agent_environment_path_))
        throw std::runtime_error("无法读取现有 agent.env");

    while (true) {
        application::SensitiveString line;
        if (!std::getline(input, line.value)) break;
        const auto first = line.value.find_first_not_of(" \t\r\n");
        if (first == std::string::npos || line.value[first] == '#') continue;
        const auto separator = line.value.find('=');
        if (separator == std::string::npos) continue;
        auto key = trim_range(line.value, 0, separator);
        if (key == "YUNSHAO_MQTT_PASSWORD") {
            application::SensitiveString password(
                trim_range(line.value, separator + 1, line.value.size()));
            values[std::move(key)] = std::move(password.value);
        } else {
            values[std::move(key)] = trim_range(line.value, separator + 1, line.value.size());
        }
    }
    if (!input.eof() && input.fail()) throw std::runtime_error("无法读取现有 agent.env");

    password_guard.release();
    return values;
}

void FileAgentConfigurationStore::write_agent_environment(Environment updates) {
    EnvironmentPasswordGuard updates_password_guard(updates);
    std::vector<std::string> lines;
    PasswordLineGuard lines_password_guard(lines);
    std::set<std::string> replaced;
    std::ifstream input(agent_environment_path_);
    if (!input && std::filesystem::exists(agent_environment_path_))
        throw std::runtime_error("无法读取现有 agent.env");

    while (true) {
        application::SensitiveString line;
        if (!std::getline(input, line.value)) break;
        const auto separator = line.value.find('=');
        if (separator == std::string::npos) {
            lines.push_back(line.value);
        } else {
            const auto key = trim_range(line.value, 0, separator);
            const auto found = updates.find(key);
            if (found == updates.end()) {
                lines.push_back(line.value);
            } else {
                lines.push_back(found->first + "=" + found->second);
                replaced.insert(found->first);
            }
        }
    }
    if (!input.eof() && input.fail()) throw std::runtime_error("无法读取现有 agent.env");
    for (const auto& item : updates) {
        if (!replaced.count(item.first)) lines.push_back(item.first + "=" + item.second);
    }

    const auto* group = ::getgrnam("yunshao");
    if (!group) throw std::runtime_error("找不到 yunshao 服务组，不能安全写入 agent.env");
    const auto temporary = agent_environment_path_.string() + ".activation." + std::to_string(::getpid());
    const int descriptor = ::open(temporary.c_str(), O_WRONLY | O_CREAT | O_EXCL | O_CLOEXEC | O_NOFOLLOW, 0600);
    if (descriptor < 0) throw std::runtime_error("无法创建 agent.env 临时文件");
    bool keep_temporary = false;
    try {
        if (::fchown(descriptor, 0, group->gr_gid) != 0 || ::fchmod(descriptor, 0640) != 0)
            throw std::runtime_error("无法设置 agent.env 安全权限");
        for (const auto& item : lines) {
            application::SensitiveString output(item + "\n");
            write_fully(descriptor, output.value, "无法写入 agent.env");
        }
        if (::fsync(descriptor) != 0) throw std::runtime_error("无法同步 agent.env");
        if (::close(descriptor) != 0) throw std::runtime_error("无法关闭 agent.env 临时文件");
        if (::rename(temporary.c_str(), agent_environment_path_.c_str()) != 0)
            throw std::runtime_error("无法原子更新 agent.env");
        const int directory = ::open(agent_environment_path_.parent_path().c_str(), O_RDONLY | O_DIRECTORY | O_CLOEXEC);
        if (directory >= 0) {
            (void)::fsync(directory);
            ::close(directory);
        }
        keep_temporary = true;
    } catch (...) {
        (void)::close(descriptor);
        if (!keep_temporary) (void)::unlink(temporary.c_str());
        throw;
    }
}

void FileAgentConfigurationStore::write_activation_overlay(Environment values) {
    EnvironmentPasswordGuard values_password_guard(values);
    constexpr const char* keys[] = {
        "YUNSHAO_DEVICE_ID", "YUNSHAO_EDGE_API_ORIGIN", "YUNSHAO_MQTT_HOST",
        "YUNSHAO_MQTT_PORT", "YUNSHAO_MQTT_CLIENT_ID", "YUNSHAO_MQTT_USERNAME",
        "YUNSHAO_MQTT_PASSWORD", "YUNSHAO_MQTT_CA"};
    application::SensitiveString contents;
    for (const auto* key : keys) {
        contents.value.append(key);
        contents.value.push_back('=');
        contents.value.append(values.at(key));
        contents.value.push_back('\n');
    }

    const auto temporary = activation_overlay_path_.string() + ".tmp." + std::to_string(::getpid());
    const int descriptor = ::open(temporary.c_str(), O_WRONLY | O_CREAT | O_TRUNC | O_CLOEXEC | O_NOFOLLOW, 0600);
    if (descriptor < 0) throw std::runtime_error("无法创建设备凭据覆盖文件");
    try {
        write_fully(descriptor, contents.value, "无法写入设备凭据覆盖文件");
        const int sync_result = ::fsync(descriptor);
        const int close_result = ::close(descriptor);
        if (sync_result != 0 || close_result != 0 ||
            ::rename(temporary.c_str(), activation_overlay_path_.c_str()) != 0)
            throw std::runtime_error("无法提交设备凭据覆盖文件");
    } catch (...) {
        (void)::close(descriptor);
        (void)::unlink(temporary.c_str());
        throw;
    }
}

void FileAgentConfigurationStore::remove_activation_overlay() {
    if (::unlink(activation_overlay_path_.c_str()) != 0 && errno != ENOENT)
        throw std::runtime_error("无法移除旧的激活凭据覆盖文件");
}

}  // namespace yunshao::adapters
