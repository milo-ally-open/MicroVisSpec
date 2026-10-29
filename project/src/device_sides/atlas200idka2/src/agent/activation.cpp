#define _GNU_SOURCE

#include "adapters/curl_https_transport.hpp"
#include "application/agent_activation_workflow.hpp"
#include "application/sensitive_string.hpp"
#include "application/ports/agent_service_lifecycle.hpp"
#include "application/ports/https_transport.hpp"

#include <nlohmann/json.hpp>

#include <algorithm>
#include <cerrno>
#include <cctype>
#include <cstdio>
#include <cstdint>
#include <cstdlib>
#include <cstring>
#include <ctime>
#include <iostream>
#include <map>
#include <mutex>
#include <optional>
#include <stdexcept>
#include <string>
#include <termios.h>
#include <fcntl.h>
#include <utility>
#include <unistd.h>

namespace {

using Json = nlohmann::json;
using yunshao::application::SensitiveString;
using yunshao::application::ports::HttpsRequest;
using yunshao::application::ports::HttpsTransportFailure;
using yunshao::application::AgentActivationGrant;
using yunshao::application::AgentActivationWorkflow;

void wipe(std::string& value);

struct ActivationOptions {
    std::string server;
    std::string device_id;
    std::string api_ca;
    std::string mqtt_ca;
    bool ticket_from_stdin = false;
};

void wipe(std::string& value) {
    volatile char* data = value.empty() ? nullptr : value.data();
    for (std::size_t index = 0; index < value.size(); ++index) data[index] = '\0';
    value.clear();
}

class EnvironmentPasswordWiper {
public:
    explicit EnvironmentPasswordWiper(
        yunshao::application::ports::AgentConfigurationStore::Environment& environment)
        : environment_(environment) {}

    ~EnvironmentPasswordWiper() {
        const auto found = environment_.find("YUNSHAO_MQTT_PASSWORD");
        if (found != environment_.end()) wipe(found->second);
    }

private:
    yunshao::application::ports::AgentConfigurationStore::Environment& environment_;
};

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

yunshao::adapters::CurlHttpsTransport& activation_https_transport() {
    static yunshao::adapters::CurlHttpsTransport transport;
    return transport;
}

SensitiveString exchange_ticket(const ActivationOptions& options,
                                const std::string& ca_path,
                                const std::string& ticket,
                                yunshao::application::ports::HttpsTransport& transport) {
    SensitiveString response_body;
    SensitiveString request_body;
    Json payload = {{"device_id", options.device_id}, {"ticket", ticket}};
    request_body.value = payload.dump();
    wipe(payload["ticket"].get_ref<std::string&>());

    const std::string endpoint = options.server + "/edge/v1/activate";
    const HttpsRequest request{endpoint, ca_path, &request_body.value, nullptr, nullptr, 0, 25, false};
    const auto result = transport.perform(request, response_body);
    if (result.failure == HttpsTransportFailure::global_initialization ||
        result.failure == HttpsTransportFailure::client_initialization)
        throw std::runtime_error("无法初始化 HTTPS 客户端");
    if (result.failure != HttpsTransportFailure::none)
        throw std::runtime_error("HTTPS 激活请求失败，请检查 Atlas 可达地址、CA 和网络");
    if (result.status < 200 || result.status >= 300)
        throw std::runtime_error("云端拒绝激活请求，HTTP 状态码：" + std::to_string(result.status));
    return response_body;
}

std::optional<std::string> activation_expiry_text(const Json& response) {
    const auto expiry = response.value("activation_expires_at", Json(nullptr));
    if (expiry.is_null()) return std::nullopt;
    if (!expiry.is_string()) throw std::runtime_error("云端激活期限格式无效");
    return expiry.get<std::string>();
}

AgentActivationGrant activation_grant_from_json(Json& response,
                                                const std::string& device_id,
                                                const std::string& server_origin,
                                                const std::string& mqtt_ca_path) {
    auto& mqtt = response.at("mqtt");
    const std::string broker_url = mqtt.value("broker_url", std::string{});
    const std::string client_id = mqtt.value("client_id", std::string{});
    const std::string username = mqtt.value("username", std::string{});
    SensitiveString password(mqtt.value("password", std::string{}));
    const bool tls_required = mqtt.value("tls_required", false);
    const std::string protocol_version = mqtt.value("protocol_version", std::string{});

    if (mqtt.contains("password") && mqtt["password"].is_string())
        wipe(mqtt["password"].get_ref<std::string&>());
    if (mqtt.contains("username") && mqtt["username"].is_string())
        wipe(mqtt["username"].get_ref<std::string&>());
    if (mqtt.contains("client_id") && mqtt["client_id"].is_string())
        wipe(mqtt["client_id"].get_ref<std::string&>());

    return AgentActivationGrant{device_id, server_origin, broker_url, client_id, username,
                                std::move(password), mqtt_ca_path, tls_required, protocol_version,
                                activation_expiry_text(response)};
}
}  // namespace

int activation_command(int argc, char** argv,
                       yunshao::application::ports::AgentServiceLifecycle& service_lifecycle,
                       AgentActivationWorkflow& activation_workflow) {
    try {
        if (::geteuid() != 0) throw std::runtime_error("请使用 sudo 运行激活命令");
        const ActivationOptions options = parse_options(argc, argv);
        auto existing = activation_workflow.read_agent_environment();
        EnvironmentPasswordWiper existing_password_wiper(existing);
        const std::string api_ca = choose_ca(options.api_ca, existing, "YUNSHAO_EDGE_API_CA");
        const std::string mqtt_ca = choose_ca(options.mqtt_ca, existing, "YUNSHAO_MQTT_CA");
        if (::access(api_ca.c_str(), R_OK) != 0 || ::access(mqtt_ca.c_str(), R_OK) != 0) {
            throw std::runtime_error("找不到 TLS CA 文件；请先将受信任 CA 安装到 /etc/yunshao/mqtt-ca.pem，或通过参数指定");
        }

        SensitiveString ticket = options.ticket_from_stdin ? read_ticket_from_stdin() : read_hidden_ticket();
        SensitiveString body = exchange_ticket(options, api_ca, ticket.value,
                                               activation_https_transport());
        wipe(ticket.value);
        Json response = Json::parse(body.value);
        if (!response.is_object() || !response.contains("mqtt") || !response["mqtt"].is_object()) {
            throw std::runtime_error("云端激活响应格式无效");
        }
        const auto expiry = activation_expiry_text(response);
        auto grant = activation_grant_from_json(response, options.device_id,
                                                options.server, mqtt_ca);
        activation_workflow.apply_cli_grant(std::move(grant), api_ca);
        wipe(body.value);
        service_lifecycle.enable_and_restart_agent();
        if (expiry) std::cout << "设备激活完成，agent 已启动；授权到期时间：" << *expiry << '\n';
        else std::cout << "设备激活完成，agent 已启动；授权期限：无限期\n";
        return 0;
    } catch (const std::exception& exception) {
        std::cerr << "设备激活失败：" << exception.what() << '\n';
        return 1;
    }
}

bool poll_activation_once(const std::string& device_id, const std::string& server,
                          const std::string& username, const std::string& password,
                          const std::string& api_ca_path, const std::string& mqtt_ca_path,
                          AgentActivationWorkflow& activation_workflow) {
    try {
        if (!valid_device_id(device_id) || !valid_https_origin(server) || username.empty() ||
            password.empty() || ::access(api_ca_path.c_str(), R_OK) != 0 || ::access(mqtt_ca_path.c_str(), R_OK) != 0) return false;
        SensitiveString response_body;
        std::string origin = server;
        while (!origin.empty() && origin.back() == '/') origin.pop_back();
        const std::string endpoint = origin + "/edge/v1/devices/" + device_id + "/activation";
        const HttpsRequest request{endpoint, api_ca_path, nullptr, &username, &password, 3, 8, true};
        const auto result = activation_https_transport().perform(request, response_body);
        if (result.failure == HttpsTransportFailure::global_initialization)
            throw std::runtime_error("curl initialization failed");
        if (result.failure != HttpsTransportFailure::none || result.status == 204) return false;
        if (result.status != 200) {
            std::cerr << "agent activation poll returned HTTP " << result.status << '\n';
            return false;
        }
        Json response = Json::parse(response_body.value);
        if (!response.is_object()) return false;
        if (response.value("status", std::string{}) == "active" && !response.contains("mqtt")) {
            activation_workflow.refresh_lease(activation_expiry_text(response));
            wipe(response_body.value);
            return false;
        }
        if (!response.contains("mqtt") || !response["mqtt"].is_object()) return false;
        auto grant = activation_grant_from_json(response, device_id, origin, mqtt_ca_path);
        const auto activation = activation_workflow.apply_polled_grant(std::move(grant));
        if (!activation.lease_written) {
            std::cerr << "device activation lease update failed; agent will retry it over HTTPS\n";
        }
        wipe(response_body.value);
        return true;
    } catch (const std::exception&) {
        std::cerr << "agent activation poll failed; will retry\n";
        return false;
    }
}
