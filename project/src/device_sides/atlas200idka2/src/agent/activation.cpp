#define _GNU_SOURCE

#include <curl/curl.h>
#include <nlohmann/json.hpp>

#include <algorithm>
#include <cerrno>
#include <cctype>
#include <cstdio>
#include <cstdint>
#include <cstdlib>
#include <cstring>
#include <ctime>
#include <filesystem>
#include <fstream>
#include <grp.h>
#include <iostream>
#include <map>
#include <mutex>
#include <pwd.h>
#include <set>
#include <stdexcept>
#include <string>
#include <sys/stat.h>
#include <sys/types.h>
#include <sys/wait.h>
#include <termios.h>
#include <fcntl.h>
#include <utility>
#include <unistd.h>
#include <vector>

namespace {

using Json = nlohmann::json;

void wipe(std::string& value);

struct SensitiveString {
    std::string value;
    SensitiveString() = default;
    explicit SensitiveString(std::string input) : value(std::move(input)) {}
    SensitiveString(const SensitiveString&) = delete;
    SensitiveString& operator=(const SensitiveString&) = delete;
    SensitiveString(SensitiveString&& other) noexcept : value(std::move(other.value)) { wipe(other.value); }
    SensitiveString& operator=(SensitiveString&& other) noexcept {
        if (this != &other) {
            wipe(value);
            value = std::move(other.value);
            wipe(other.value);
        }
        return *this;
    }
    ~SensitiveString() {
        volatile char* data = value.empty() ? nullptr : value.data();
        for (std::size_t index = 0; index < value.size(); ++index) data[index] = '\0';
    }
};

struct ActivationOptions {
    std::string server;
    std::string device_id;
    std::string api_ca;
    std::string mqtt_ca;
    bool ticket_from_stdin = false;
};

std::string trim(std::string value) {
    const auto first = value.find_first_not_of(" \t\r\n");
    if (first == std::string::npos) return {};
    const auto last = value.find_last_not_of(" \t\r\n");
    return value.substr(first, last - first + 1);
}

void wipe(std::string& value) {
    volatile char* data = value.empty() ? nullptr : value.data();
    for (std::size_t index = 0; index < value.size(); ++index) data[index] = '\0';
    value.clear();
}

bool valid_device_id(const std::string& value) {
    if (value.size() != 36) return false;
    for (std::size_t index = 0; index < value.size(); ++index) {
        if (index == 8 || index == 13 || index == 18 || index == 23) {
            if (value[index] != '-') return false;
        } else if (!((value[index] >= '0' && value[index] <= '9') ||
                     (value[index] >= 'a' && value[index] <= 'f') ||
                     (value[index] >= 'A' && value[index] <= 'F'))) {
            return false;
        }
    }
    return true;
}

bool valid_https_origin(const std::string& value) {
    constexpr const char* scheme = "https://";
    if (value.compare(0, std::strlen(scheme), scheme) != 0 ||
        value.find_first_of("?#") != std::string::npos || value.find('@') != std::string::npos) {
        return false;
    }
    const auto authority_start = std::strlen(scheme);
    const auto suffix = value.find('/', authority_start);
    const std::string authority = value.substr(authority_start, suffix == std::string::npos ? std::string::npos : suffix - authority_start);
    if (authority.empty() || value.find_first_of("\r\n\t ") != std::string::npos) return false;
    if (!std::all_of(authority.begin(), authority.end(), [](unsigned char ch) {
            return std::isalnum(ch) || ch == '.' || ch == '-' || ch == ':' || ch == '[' || ch == ']';
        })) return false;
    if (suffix != std::string::npos && value.find_first_not_of('/', suffix) != std::string::npos) return false;
    return true;
}

ActivationOptions parse_options(int argc, char** argv) {
    ActivationOptions options;
    for (int index = 2; index < argc; ++index) {
        const std::string key = argv[index];
        if (key == "--help" || key == "-h") {
            std::cout << "用法：sudo yunshao-agent activate --server https://<Atlas可达的云端地址> --device-id <UUID> [--api-ca <CA文件>] [--mqtt-ca <CA文件>] [--ticket-stdin]\n"
                      << "默认从终端隐藏读取票据；--ticket-stdin 从标准输入读取，供受控的远程部署流程使用。\n";
            std::exit(0);
        }
        if (key == "--ticket-stdin") {
            options.ticket_from_stdin = true;
            continue;
        }
        if (index + 1 >= argc) throw std::runtime_error("activate 命令缺少参数值");
        const std::string value = argv[++index];
        if (key == "--server") options.server = value;
        else if (key == "--device-id") options.device_id = value;
        else if (key == "--api-ca") options.api_ca = value;
        else if (key == "--mqtt-ca") options.mqtt_ca = value;
        else throw std::runtime_error("activate 命令包含未知参数");
    }
    if (!valid_https_origin(options.server) || !valid_device_id(options.device_id)) {
        throw std::runtime_error("需要有效的 HTTPS server origin 和 device UUID");
    }
    while (!options.server.empty() && options.server.back() == '/') options.server.pop_back();
    return options;
}

SensitiveString read_ticket_from_stdin() {
    std::string ticket;
    if (!std::getline(std::cin, ticket)) throw std::runtime_error("无法从标准输入读取激活票据");
    if (!ticket.empty() && ticket.back() == '\r') ticket.pop_back();
    if (ticket.size() < 32 || ticket.size() > 128) {
        wipe(ticket);
        throw std::runtime_error("激活票据长度无效");
    }
    SensitiveString secret(std::move(ticket));
    wipe(ticket);
    return secret;
}

SensitiveString read_hidden_ticket() {
    const int tty = ::open("/dev/tty", O_RDWR | O_CLOEXEC);
    if (tty < 0) throw std::runtime_error("无法打开终端读取激活票据");
    termios previous{};
    if (::tcgetattr(tty, &previous) != 0) {
        ::close(tty);
        throw std::runtime_error("无法设置隐藏输入");
    }
    termios hidden = previous;
    hidden.c_lflag &= static_cast<tcflag_t>(~ECHO);
    if (::tcsetattr(tty, TCSAFLUSH, &hidden) != 0) {
        ::close(tty);
        throw std::runtime_error("无法设置隐藏输入");
    }
    constexpr char prompt[] = "请输入网页显示的一次性激活票据：";
    (void)::write(tty, prompt, sizeof(prompt) - 1);
    std::string ticket;
    char character = 0;
    while (true) {
        const auto count = ::read(tty, &character, 1);
        if (count < 0 && errno == EINTR) continue;
        if (count <= 0 || character == '\n' || character == '\r') break;
        if (ticket.size() < 128) ticket.push_back(character);
    }
    (void)::tcsetattr(tty, TCSAFLUSH, &previous);
    constexpr char newline[] = "\n";
    (void)::write(tty, newline, sizeof(newline) - 1);
    ::close(tty);
    if (ticket.size() < 32 || ticket.size() > 128) {
        wipe(ticket);
        throw std::runtime_error("激活票据长度无效");
    }
    SensitiveString secret(std::move(ticket));
    wipe(ticket);
    return secret;
}

std::map<std::string, std::string> read_agent_environment(const std::filesystem::path& path) {
    std::map<std::string, std::string> values;
    std::ifstream input(path);
    if (!input && std::filesystem::exists(path)) throw std::runtime_error("无法读取现有 agent.env");
    std::string line;
    while (std::getline(input, line)) {
        line = trim(std::move(line));
        if (line.empty() || line.front() == '#') continue;
        const auto separator = line.find('=');
        if (separator == std::string::npos) continue;
        values[trim(line.substr(0, separator))] = trim(line.substr(separator + 1));
    }
    if (!input.eof() && input.fail()) throw std::runtime_error("无法读取现有 agent.env");
    return values;
}

std::string choose_ca(const std::string& explicit_path, const std::map<std::string, std::string>& existing,
                      const std::string& variable) {
    if (!explicit_path.empty()) return explicit_path;
    const auto found = existing.find(variable);
    if (found != existing.end() && !found->second.empty()) return found->second;
    if (variable == "YUNSHAO_EDGE_API_CA") {
        const auto mqtt = existing.find("YUNSHAO_MQTT_CA");
        if (mqtt != existing.end() && !mqtt->second.empty()) return mqtt->second;
    }
    return "/etc/yunshao/mqtt-ca.pem";
}

size_t append_response(char* data, size_t size, size_t count, void* context) {
    const auto length = size * count;
    auto* output = static_cast<SensitiveString*>(context);
    if (length > 65536 || output->value.size() > 65536 - length) return 0;
    output->value.append(data, length);
    return length;
}

SensitiveString exchange_ticket(const ActivationOptions& options, const std::string& ca_path, const std::string& ticket) {
    SensitiveString response_body;
    SensitiveString request_body;
    Json payload = {{"device_id", options.device_id}, {"ticket", ticket}};
    request_body.value = payload.dump();
    wipe(payload["ticket"].get_ref<std::string&>());

    CURL* curl = ::curl_easy_init();
    if (!curl) throw std::runtime_error("无法初始化 HTTPS 客户端");
    const std::string endpoint = options.server + "/edge/v1/activate";
    struct curl_slist* headers = nullptr;
    headers = ::curl_slist_append(headers, "Content-Type: application/json");
    ::curl_easy_setopt(curl, CURLOPT_URL, endpoint.c_str());
    ::curl_easy_setopt(curl, CURLOPT_PROTOCOLS, CURLPROTO_HTTPS);
    ::curl_easy_setopt(curl, CURLOPT_POST, 1L);
    ::curl_easy_setopt(curl, CURLOPT_POSTFIELDS, request_body.value.data());
    ::curl_easy_setopt(curl, CURLOPT_POSTFIELDSIZE, static_cast<long>(request_body.value.size()));
    ::curl_easy_setopt(curl, CURLOPT_HTTPHEADER, headers);
    ::curl_easy_setopt(curl, CURLOPT_CAINFO, ca_path.c_str());
    ::curl_easy_setopt(curl, CURLOPT_SSL_VERIFYPEER, 1L);
    ::curl_easy_setopt(curl, CURLOPT_SSL_VERIFYHOST, 2L);
    ::curl_easy_setopt(curl, CURLOPT_FOLLOWLOCATION, 0L);
    ::curl_easy_setopt(curl, CURLOPT_NOPROXY, "*");
    ::curl_easy_setopt(curl, CURLOPT_TIMEOUT, 25L);
    ::curl_easy_setopt(curl, CURLOPT_WRITEFUNCTION, append_response);
    ::curl_easy_setopt(curl, CURLOPT_WRITEDATA, &response_body);
    const CURLcode result = ::curl_easy_perform(curl);
    long status = 0;
    (void)::curl_easy_getinfo(curl, CURLINFO_RESPONSE_CODE, &status);
    ::curl_slist_free_all(headers);
    ::curl_easy_cleanup(curl);
    if (result != CURLE_OK) throw std::runtime_error("HTTPS 激活请求失败，请检查 Atlas 可达地址、CA 和网络");
    if (status < 200 || status >= 300) throw std::runtime_error("云端拒绝激活请求，HTTP 状态码：" + std::to_string(status));
    return response_body;
}

struct MqttEndpoint {
    std::string host;
    std::string port;
};

MqttEndpoint parse_broker_url(const std::string& value) {
    constexpr const char* prefix = "ssl://";
    if (value.compare(0, std::strlen(prefix), prefix) != 0) throw std::runtime_error("激活响应中的 MQTT 地址不是 TLS 地址");
    const std::string authority = value.substr(std::strlen(prefix));
    if (authority.empty() || authority.find_first_of("/@?#") != std::string::npos) throw std::runtime_error("激活响应中的 MQTT 地址格式无效");
    std::string host;
    std::string port;
    if (authority.front() == '[') {
        const auto closing = authority.find(']');
        if (closing == std::string::npos || closing + 1 >= authority.size() || authority[closing + 1] != ':') throw std::runtime_error("激活响应中的 MQTT 地址格式无效");
        host = authority.substr(1, closing - 1);
        port = authority.substr(closing + 2);
    } else {
        const auto separator = authority.rfind(':');
        if (separator == std::string::npos) throw std::runtime_error("激活响应中的 MQTT 地址缺少端口");
        host = authority.substr(0, separator);
        port = authority.substr(separator + 1);
    }
    if (host.empty() || !std::all_of(host.begin(), host.end(), [](unsigned char ch) {
            return std::isalnum(ch) || ch == '.' || ch == '-' || ch == ':';
        }) || port.empty() || !std::all_of(port.begin(), port.end(), [](unsigned char ch) { return ch >= '0' && ch <= '9'; })) {
        throw std::runtime_error("激活响应中的 MQTT 地址格式无效");
    }
    std::size_t parsed_port = 0;
    try {
        parsed_port = static_cast<std::size_t>(std::stoul(port));
    } catch (...) {
        throw std::runtime_error("激活响应中的 MQTT 端口无效");
    }
    if (parsed_port < 1 || parsed_port > 65535) throw std::runtime_error("激活响应中的 MQTT 端口无效");
    return {host, port};
}

std::int64_t activation_expiry_unix(const std::string& value) {
    std::tm utc{};
    char* end = ::strptime(value.c_str(), "%Y-%m-%dT%H:%M:%S", &utc);
    if (!end || (*end != 'Z' && *end != '.')) throw std::runtime_error("云端返回的授权到期时间格式无效");
    bool fractional = false;
    if (*end == '.') {
        ++end;
        if (*end < '0' || *end > '9') throw std::runtime_error("云端返回的授权到期时间格式无效");
        while (*end >= '0' && *end <= '9') {
            fractional = fractional || *end != '0';
            ++end;
        }
        if (*end != 'Z') throw std::runtime_error("云端返回的授权到期时间格式无效");
    }
    if (*(end + 1) != '\0') throw std::runtime_error("云端返回的授权到期时间格式无效");
    time_t seconds = ::timegm(&utc);
    if (seconds < 0) throw std::runtime_error("云端返回的授权到期时间超出支持范围");
    if (fractional) ++seconds;  // Store whole seconds and avoid expiring locally before the server.
    return static_cast<std::int64_t>(seconds);
}

int systemctl(const std::vector<std::string>& arguments, bool allow_failure = false) {
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
    if (WIFEXITED(status)) {
        const int exit_code = WEXITSTATUS(status);
        if (exit_code == 0 || allow_failure) return exit_code;
    }
    if (allow_failure) return 1;
    throw std::runtime_error("systemd 命令执行失败");
}

void write_runtime_lease(const std::string& expiry) {
    const std::filesystem::path path = "/var/lib/yunshao/activation-lease.json";
    const auto temporary = path.string() + ".tmp." + std::to_string(::getpid());
    const int descriptor = ::open(temporary.c_str(), O_WRONLY | O_CREAT | O_TRUNC | O_CLOEXEC | O_NOFOLLOW, 0600);
    if (descriptor < 0) throw std::runtime_error("无法保存设备授权期限");
    if (::geteuid() == 0) {
        const auto* user = ::getpwnam("yunshao");
        const auto* group = ::getgrnam("yunshao");
        if (!user || !group || ::fchown(descriptor, user->pw_uid, group->gr_gid) != 0 || ::fchmod(descriptor, 0640) != 0) {
            (void)::close(descriptor);
            (void)::unlink(temporary.c_str());
            throw std::runtime_error("无法设置设备授权期限文件权限");
        }
    } else if (::fchmod(descriptor, 0600) != 0) {
        (void)::close(descriptor);
        (void)::unlink(temporary.c_str());
        throw std::runtime_error("无法设置设备授权期限文件权限");
    }
    const Json lease = expiry.empty() ? Json{{"expires_at_unix", nullptr}} : Json{{"expires_at_unix", activation_expiry_unix(expiry)}};
    const std::string contents = lease.dump() + "\n";
    std::size_t offset = 0;
    while (offset < contents.size()) {
        const auto written = ::write(descriptor, contents.data() + offset, contents.size() - offset);
        if (written < 0 && errno == EINTR) continue;
        if (written <= 0) {
            (void)::close(descriptor);
            (void)::unlink(temporary.c_str());
            throw std::runtime_error("无法写入设备授权期限");
        }
        offset += static_cast<std::size_t>(written);
    }
    const int sync_result = ::fsync(descriptor);
    const int close_result = ::close(descriptor);
    if (sync_result != 0 || close_result != 0 || ::rename(temporary.c_str(), path.c_str()) != 0) {
        (void)::unlink(temporary.c_str());
        throw std::runtime_error("无法提交设备授权期限");
    }
}

void write_activation_overlay(const Json& response, const std::string& device_id,
                              const std::string& server, const std::string& mqtt_ca) {
    const auto& mqtt = response.at("mqtt");
    const auto endpoint = parse_broker_url(mqtt.value("broker_url", std::string{}));
    const auto client_id = mqtt.value("client_id", std::string{});
    const auto username = mqtt.value("username", std::string{});
    SensitiveString password(mqtt.value("password", std::string{}));
    std::string compact_id = device_id;
    compact_id.erase(std::remove(compact_id.begin(), compact_id.end(), '-'), compact_id.end());
    const bool valid_password = password.value.size() >= 32 && password.value.size() <= 128 &&
        std::all_of(password.value.begin(), password.value.end(), [](unsigned char ch) {
            return std::isalnum(ch) || ch == '-' || ch == '_';
        });
    if (!mqtt.value("tls_required", false) || mqtt.value("protocol_version", std::string{}) != "3.1.1" ||
        client_id != "yunshao-edge-" + compact_id || username != "yunshao-device-" + compact_id || !valid_password) {
        throw std::runtime_error("云端激活响应缺少有效 MQTT TLS 凭据");
    }
    const auto expiry = response.value("activation_expires_at", Json(nullptr));
    const std::string expiry_text = expiry.is_string() ? expiry.get<std::string>() : std::string{};
    if (!expiry.is_null() && !expiry.is_string()) throw std::runtime_error("云端激活期限格式无效");
    if (!expiry_text.empty()) (void)activation_expiry_unix(expiry_text);
    SensitiveString contents;
    auto append = [&](const std::string& name, const std::string& value) {
        contents.value += name + "=" + value + "\n";
    };
    append("YUNSHAO_DEVICE_ID", device_id);
    append("YUNSHAO_EDGE_API_ORIGIN", server);
    append("YUNSHAO_MQTT_HOST", endpoint.host);
    append("YUNSHAO_MQTT_PORT", endpoint.port);
    append("YUNSHAO_MQTT_CLIENT_ID", client_id);
    append("YUNSHAO_MQTT_USERNAME", username);
    append("YUNSHAO_MQTT_PASSWORD", password.value);
    append("YUNSHAO_MQTT_CA", mqtt_ca);
    const std::filesystem::path path = "/var/lib/yunshao/activation.env";
    const auto temporary = path.string() + ".tmp." + std::to_string(::getpid());
    const int descriptor = ::open(temporary.c_str(), O_WRONLY | O_CREAT | O_TRUNC | O_CLOEXEC | O_NOFOLLOW, 0600);
    if (descriptor < 0) throw std::runtime_error("无法创建设备凭据覆盖文件");
    std::size_t offset = 0;
    while (offset < contents.value.size()) {
        const auto written = ::write(descriptor, contents.value.data() + offset, contents.value.size() - offset);
        if (written < 0 && errno == EINTR) continue;
        if (written <= 0) {
            (void)::close(descriptor);
            (void)::unlink(temporary.c_str());
            throw std::runtime_error("无法写入设备凭据覆盖文件");
        }
        offset += static_cast<std::size_t>(written);
    }
    const int sync_result = ::fsync(descriptor);
    const int close_result = ::close(descriptor);
    if (sync_result != 0 || close_result != 0 || ::rename(temporary.c_str(), path.c_str()) != 0) {
        (void)::unlink(temporary.c_str());
        throw std::runtime_error("无法提交设备凭据覆盖文件");
    }
    wipe(contents.value);
    wipe(password.value);
    try {
        write_runtime_lease(expiry_text);
    } catch (const std::exception&) {
        std::cerr << "device activation lease update failed; agent will retry it over HTTPS\n";
    }
}

void write_agent_environment(const std::filesystem::path& path, const std::map<std::string, std::string>& updates) {
    std::vector<std::string> lines;
    std::set<std::string> replaced;
    std::ifstream input(path);
    if (!input && std::filesystem::exists(path)) throw std::runtime_error("无法读取现有 agent.env");
    std::string line;
    while (std::getline(input, line)) {
        const auto separator = line.find('=');
        if (separator == std::string::npos) {
            lines.push_back(line);
            continue;
        }
        const auto found = updates.find(trim(line.substr(0, separator)));
        if (found == updates.end()) {
            lines.push_back(line);
            continue;
        }
        lines.push_back(found->first + "=" + found->second);
        replaced.insert(found->first);
    }
    for (const auto& item : updates) {
        if (!replaced.count(item.first)) lines.push_back(item.first + "=" + item.second);
    }

    const auto group = ::getgrnam("yunshao");
    if (!group) throw std::runtime_error("找不到 yunshao 服务组，不能安全写入 agent.env");
    const auto temporary = path.string() + ".activation." + std::to_string(::getpid());
    const int descriptor = ::open(temporary.c_str(), O_WRONLY | O_CREAT | O_EXCL | O_CLOEXEC | O_NOFOLLOW, 0600);
    if (descriptor < 0) throw std::runtime_error("无法创建 agent.env 临时文件");
    bool keep_temporary = false;
    try {
        if (::fchown(descriptor, 0, group->gr_gid) != 0 || ::fchmod(descriptor, 0640) != 0) throw std::runtime_error("无法设置 agent.env 安全权限");
        for (const auto& item : lines) {
            std::string output = item + "\n";
            std::size_t offset = 0;
            while (offset < output.size()) {
                const auto written = ::write(descriptor, output.data() + offset, output.size() - offset);
                if (written < 0 && errno == EINTR) continue;
                if (written <= 0) throw std::runtime_error("无法写入 agent.env");
                offset += static_cast<std::size_t>(written);
            }
            if (item.compare(0, std::strlen("YUNSHAO_MQTT_PASSWORD="), "YUNSHAO_MQTT_PASSWORD=") == 0) wipe(output);
        }
        if (::fsync(descriptor) != 0) throw std::runtime_error("无法同步 agent.env");
        if (::close(descriptor) != 0) throw std::runtime_error("无法关闭 agent.env 临时文件");
        if (::rename(temporary.c_str(), path.c_str()) != 0) throw std::runtime_error("无法原子更新 agent.env");
        for (auto& item : lines) {
            if (item.compare(0, std::strlen("YUNSHAO_MQTT_PASSWORD="), "YUNSHAO_MQTT_PASSWORD=") == 0) wipe(item);
        }
        const int directory = ::open(path.parent_path().c_str(), O_RDONLY | O_DIRECTORY | O_CLOEXEC);
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

void enable_and_start_agent() {
    systemctl({"enable", "--now", "yunshao-agent.service"});
    systemctl({"restart", "yunshao-agent.service"});
}

}  // namespace

int activation_command(int argc, char** argv) {
    try {
        if (::geteuid() != 0) throw std::runtime_error("请使用 sudo 运行激活命令");
        const ActivationOptions options = parse_options(argc, argv);
        const std::filesystem::path agent_env = "/etc/yunshao/agent.env";
        auto existing = read_agent_environment(agent_env);
        const std::string api_ca = choose_ca(options.api_ca, existing, "YUNSHAO_EDGE_API_CA");
        const std::string mqtt_ca = choose_ca(options.mqtt_ca, existing, "YUNSHAO_MQTT_CA");
        auto old_password = existing.find("YUNSHAO_MQTT_PASSWORD");
        if (old_password != existing.end()) wipe(old_password->second);
        if (::access(api_ca.c_str(), R_OK) != 0 || ::access(mqtt_ca.c_str(), R_OK) != 0) {
            throw std::runtime_error("找不到 TLS CA 文件；请先将受信任 CA 安装到 /etc/yunshao/mqtt-ca.pem，或通过参数指定");
        }

        SensitiveString ticket = options.ticket_from_stdin ? read_ticket_from_stdin() : read_hidden_ticket();
        if (::curl_global_init(CURL_GLOBAL_DEFAULT) != CURLE_OK) throw std::runtime_error("无法初始化 HTTPS 客户端");
        SensitiveString body = exchange_ticket(options, api_ca, ticket.value);
        wipe(ticket.value);
        Json response = Json::parse(body.value);
        if (!response.is_object() || !response.contains("mqtt") || !response["mqtt"].is_object()) {
            throw std::runtime_error("云端激活响应格式无效");
        }
        auto& mqtt = response["mqtt"];
        const auto broker_url = mqtt.value("broker_url", std::string{});
        const auto client_id = mqtt.value("client_id", std::string{});
        const auto username = mqtt.value("username", std::string{});
        SensitiveString mqtt_password(mqtt.value("password", std::string{}));
        std::string compact_id = options.device_id;
        compact_id.erase(std::remove(compact_id.begin(), compact_id.end(), '-'), compact_id.end());
        const std::string expected_username = "yunshao-device-" + compact_id;
        const std::string expected_client_id = "yunshao-edge-" + compact_id;
        const bool valid_password = mqtt_password.value.size() >= 32 && mqtt_password.value.size() <= 128 &&
            std::all_of(mqtt_password.value.begin(), mqtt_password.value.end(), [](unsigned char ch) {
                return std::isalnum(ch) || ch == '-' || ch == '_';
            });
        if (!mqtt.value("tls_required", false) || mqtt.value("protocol_version", std::string{}) != "3.1.1" ||
            client_id != expected_client_id || username != expected_username || !valid_password) {
            throw std::runtime_error("云端激活响应缺少必要 MQTT TLS 凭据");
        }
        if (mqtt.contains("password") && mqtt["password"].is_string()) wipe(mqtt["password"].get_ref<std::string&>());
        wipe(response["mqtt"]["username"].get_ref<std::string&>());
        const auto endpoint = parse_broker_url(broker_url);
        std::string activation_expiry;
        const auto expiry = response.value("activation_expires_at", Json(nullptr));
        if (!expiry.is_null()) {
            if (!expiry.is_string()) throw std::runtime_error("云端激活期限格式无效");
            activation_expiry = expiry.get<std::string>();
            (void)activation_expiry_unix(activation_expiry);
        }
        std::map<std::string, std::string> updates{
            {"YUNSHAO_DEVICE_ID", options.device_id},
            {"YUNSHAO_EDGE_API_ORIGIN", options.server},
            {"YUNSHAO_MQTT_HOST", endpoint.host},
            {"YUNSHAO_MQTT_PORT", endpoint.port},
            {"YUNSHAO_MQTT_CLIENT_ID", client_id},
            {"YUNSHAO_MQTT_USERNAME", username},
            {"YUNSHAO_MQTT_PASSWORD", mqtt_password.value},
            {"YUNSHAO_EDGE_API_CA", api_ca},
            {"YUNSHAO_MQTT_CA", mqtt_ca},
            {"YUNSHAO_ACTIVATION_EXPIRES_AT", activation_expiry},
        };
        write_agent_environment(agent_env, updates);
        write_runtime_lease(activation_expiry);
        if (::unlink("/var/lib/yunshao/activation.env") != 0 && errno != ENOENT)
            throw std::runtime_error("无法移除旧的激活凭据覆盖文件");
        wipe(mqtt_password.value);
        wipe(updates["YUNSHAO_MQTT_PASSWORD"]);
        wipe(body.value);
        wipe(response["mqtt"]["client_id"].get_ref<std::string&>());
        enable_and_start_agent();
        if (expiry.is_string()) std::cout << "设备激活完成，agent 已启动；授权到期时间：" << expiry.get<std::string>() << '\n';
        else std::cout << "设备激活完成，agent 已启动；授权期限：无限期\n";
        return 0;
    } catch (const std::exception& exception) {
        std::cerr << "设备激活失败：" << exception.what() << '\n';
        return 1;
    }
}

bool poll_activation_once(const std::string& device_id, const std::string& server,
                          const std::string& username, const std::string& password,
                          const std::string& api_ca_path, const std::string& mqtt_ca_path) {
    try {
        if (!valid_device_id(device_id) || !valid_https_origin(server) || username.empty() ||
            password.empty() || ::access(api_ca_path.c_str(), R_OK) != 0 || ::access(mqtt_ca_path.c_str(), R_OK) != 0) return false;
        static std::once_flag curl_initialized;
        std::call_once(curl_initialized, [] {
            if (::curl_global_init(CURL_GLOBAL_DEFAULT) != CURLE_OK) throw std::runtime_error("curl initialization failed");
        });
        SensitiveString response_body;
        CURL* curl = ::curl_easy_init();
        if (!curl) return false;
        std::string origin = server;
        while (!origin.empty() && origin.back() == '/') origin.pop_back();
        const std::string endpoint = origin + "/edge/v1/devices/" + device_id + "/activation";
        ::curl_easy_setopt(curl, CURLOPT_URL, endpoint.c_str());
        ::curl_easy_setopt(curl, CURLOPT_PROTOCOLS, CURLPROTO_HTTPS);
        ::curl_easy_setopt(curl, CURLOPT_HTTPAUTH, CURLAUTH_BASIC);
        ::curl_easy_setopt(curl, CURLOPT_USERNAME, username.c_str());
        ::curl_easy_setopt(curl, CURLOPT_PASSWORD, password.c_str());
        ::curl_easy_setopt(curl, CURLOPT_CAINFO, api_ca_path.c_str());
        ::curl_easy_setopt(curl, CURLOPT_SSL_VERIFYPEER, 1L);
        ::curl_easy_setopt(curl, CURLOPT_SSL_VERIFYHOST, 2L);
        ::curl_easy_setopt(curl, CURLOPT_FOLLOWLOCATION, 0L);
        ::curl_easy_setopt(curl, CURLOPT_NOPROXY, "*");
        ::curl_easy_setopt(curl, CURLOPT_CONNECTTIMEOUT, 3L);
        ::curl_easy_setopt(curl, CURLOPT_TIMEOUT, 8L);
        ::curl_easy_setopt(curl, CURLOPT_NOSIGNAL, 1L);
        ::curl_easy_setopt(curl, CURLOPT_WRITEFUNCTION, append_response);
        ::curl_easy_setopt(curl, CURLOPT_WRITEDATA, &response_body);
        const CURLcode result = ::curl_easy_perform(curl);
        long status = 0;
        (void)::curl_easy_getinfo(curl, CURLINFO_RESPONSE_CODE, &status);
        ::curl_easy_cleanup(curl);
        if (result != CURLE_OK || status == 204) return false;
        if (status != 200) {
            std::cerr << "agent activation poll returned HTTP " << status << '\n';
            return false;
        }
        Json response = Json::parse(response_body.value);
        if (!response.is_object()) return false;
        if (response.value("status", std::string{}) == "active" && !response.contains("mqtt")) {
            const auto expiry = response.value("activation_expires_at", Json(nullptr));
            if (!expiry.is_null() && !expiry.is_string()) return false;
            write_runtime_lease(expiry.is_string() ? expiry.get<std::string>() : std::string{});
            wipe(response_body.value);
            return false;
        }
        if (!response.contains("mqtt") || !response["mqtt"].is_object()) return false;
        write_activation_overlay(response, device_id, origin, mqtt_ca_path);
        if (response["mqtt"].contains("password") && response["mqtt"]["password"].is_string())
            wipe(response["mqtt"]["password"].get_ref<std::string&>());
        for (const char* key : {"username", "client_id"}) {
            if (response["mqtt"].contains(key) && response["mqtt"][key].is_string())
                wipe(response["mqtt"][key].get_ref<std::string&>());
        }
        wipe(response_body.value);
        return true;
    } catch (const std::exception&) {
        std::cerr << "agent activation poll failed; will retry\n";
        return false;
    }
}
