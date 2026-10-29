#include "application/agent_activation_workflow.hpp"

#include <algorithm>
#include <cctype>
#include <ctime>
#include <stdexcept>
#include <string>
#include <utility>

namespace yunshao::application {
namespace {

struct MqttEndpoint {
    std::string host;
    std::string port;
};

MqttEndpoint parse_broker_url(const std::string& value) {
    constexpr const char* prefix = "ssl://";
    if (value.compare(0, 6, prefix) != 0)
        throw std::runtime_error("激活响应中的 MQTT 地址不是 TLS 地址");
    const std::string authority = value.substr(6);
    if (authority.empty() || authority.find_first_of("/@?#") != std::string::npos)
        throw std::runtime_error("激活响应中的 MQTT 地址格式无效");

    std::string host;
    std::string port;
    if (authority.front() == '[') {
        const auto closing = authority.find(']');
        if (closing == std::string::npos || closing + 1 >= authority.size() || authority[closing + 1] != ':')
            throw std::runtime_error("激活响应中的 MQTT 地址格式无效");
        host = authority.substr(1, closing - 1);
        port = authority.substr(closing + 2);
    } else {
        const auto separator = authority.rfind(':');
        if (separator == std::string::npos)
            throw std::runtime_error("激活响应中的 MQTT 地址缺少端口");
        host = authority.substr(0, separator);
        port = authority.substr(separator + 1);
    }

    if (host.empty() || !std::all_of(host.begin(), host.end(), [](unsigned char ch) {
            return std::isalnum(ch) || ch == '.' || ch == '-' || ch == ':';
        }) || port.empty() || !std::all_of(port.begin(), port.end(), [](unsigned char ch) {
            return ch >= '0' && ch <= '9';
        })) {
        throw std::runtime_error("激活响应中的 MQTT 地址格式无效");
    }

    std::size_t parsed_port = 0;
    try {
        parsed_port = static_cast<std::size_t>(std::stoul(port));
    } catch (...) {
        throw std::runtime_error("激活响应中的 MQTT 端口无效");
    }
    if (parsed_port < 1 || parsed_port > 65535)
        throw std::runtime_error("激活响应中的 MQTT 端口无效");
    return {host, port};
}

std::int64_t parse_expiry_unix(const std::string& value) {
    std::tm utc{};
    char* end = ::strptime(value.c_str(), "%Y-%m-%dT%H:%M:%S", &utc);
    if (!end || (*end != 'Z' && *end != '.'))
        throw std::runtime_error("云端返回的授权到期时间格式无效");
    bool fractional = false;
    if (*end == '.') {
        ++end;
        if (*end < '0' || *end > '9')
            throw std::runtime_error("云端返回的授权到期时间格式无效");
        while (*end >= '0' && *end <= '9') {
            fractional = fractional || *end != '0';
            ++end;
        }
        if (*end != 'Z') throw std::runtime_error("云端返回的授权到期时间格式无效");
    }
    if (*(end + 1) != '\0') throw std::runtime_error("云端返回的授权到期时间格式无效");
    const time_t seconds = ::timegm(&utc);
    if (seconds < 0) throw std::runtime_error("云端返回的授权到期时间超出支持范围");
    return static_cast<std::int64_t>(seconds) + (fractional ? 1 : 0);
}

void wipe(std::string& value) {
    volatile char* data = value.empty() ? nullptr : value.data();
    for (std::size_t index = 0; index < value.size(); ++index) data[index] = '\0';
    value.clear();
}

class EnvironmentPasswordWiper {
public:
    explicit EnvironmentPasswordWiper(ports::AgentConfigurationStore::Environment& environment)
        : environment_(environment) {}
    ~EnvironmentPasswordWiper() {
        const auto found = environment_.find("YUNSHAO_MQTT_PASSWORD");
        if (found != environment_.end()) wipe(found->second);
    }

private:
    ports::AgentConfigurationStore::Environment& environment_;
};

}  // namespace

AgentActivationWorkflow::AgentActivationWorkflow(
    ports::AgentConfigurationStore& configuration_store,
    ports::ActivationLeaseWriter& lease_writer)
    : configuration_store_(configuration_store), lease_writer_(lease_writer) {}

ports::AgentConfigurationStore::Environment AgentActivationWorkflow::read_agent_environment() {
    return configuration_store_.read_agent_environment();
}

void AgentActivationWorkflow::apply_cli_grant(AgentActivationGrant grant, const std::string& api_ca_path) {
    validate_grant(grant, "云端激活响应缺少必要 MQTT TLS 凭据");
    const auto endpoint = parse_broker_url(grant.mqtt_broker_url);
    const auto expiry = parse_expiry(grant.activation_expires_at);

    ports::AgentConfigurationStore::Environment updates;
    EnvironmentPasswordWiper password_wiper(updates);
    updates.emplace("YUNSHAO_DEVICE_ID", grant.device_id);
    updates.emplace("YUNSHAO_EDGE_API_ORIGIN", grant.server_origin);
    updates.emplace("YUNSHAO_MQTT_HOST", endpoint.host);
    updates.emplace("YUNSHAO_MQTT_PORT", endpoint.port);
    updates.emplace("YUNSHAO_MQTT_CLIENT_ID", grant.mqtt_client_id);
    updates.emplace("YUNSHAO_MQTT_USERNAME", grant.mqtt_username);
    updates.emplace("YUNSHAO_MQTT_PASSWORD", grant.mqtt_password.value);
    updates.emplace("YUNSHAO_EDGE_API_CA", api_ca_path);
    updates.emplace("YUNSHAO_MQTT_CA", grant.mqtt_ca_path);
    updates.emplace("YUNSHAO_ACTIVATION_EXPIRES_AT",
                     grant.activation_expires_at.value_or(std::string{}));

    configuration_store_.write_agent_environment(std::move(updates));
    lease_writer_.write_expiry_unix(expiry);
    configuration_store_.remove_activation_overlay();
}

AgentActivationResult AgentActivationWorkflow::apply_polled_grant(AgentActivationGrant grant) {
    validate_grant(grant, "云端激活响应缺少有效 MQTT TLS 凭据");
    const auto endpoint = parse_broker_url(grant.mqtt_broker_url);
    const auto expiry = parse_expiry(grant.activation_expires_at);

    ports::AgentConfigurationStore::Environment overlay;
    EnvironmentPasswordWiper password_wiper(overlay);
    overlay.emplace("YUNSHAO_DEVICE_ID", grant.device_id);
    overlay.emplace("YUNSHAO_EDGE_API_ORIGIN", grant.server_origin);
    overlay.emplace("YUNSHAO_MQTT_HOST", endpoint.host);
    overlay.emplace("YUNSHAO_MQTT_PORT", endpoint.port);
    overlay.emplace("YUNSHAO_MQTT_CLIENT_ID", grant.mqtt_client_id);
    overlay.emplace("YUNSHAO_MQTT_USERNAME", grant.mqtt_username);
    overlay.emplace("YUNSHAO_MQTT_PASSWORD", grant.mqtt_password.value);
    overlay.emplace("YUNSHAO_MQTT_CA", grant.mqtt_ca_path);
    configuration_store_.write_activation_overlay(std::move(overlay));

    try {
        lease_writer_.write_expiry_unix(expiry);
    } catch (const std::exception&) {
        return {false};
    }
    return {true};
}

void AgentActivationWorkflow::refresh_lease(
    const std::optional<std::string>& activation_expires_at) {
    lease_writer_.write_expiry_unix(parse_expiry(activation_expires_at));
}

std::optional<std::int64_t> AgentActivationWorkflow::parse_expiry(
    const std::optional<std::string>& activation_expires_at) const {
    if (!activation_expires_at || activation_expires_at->empty()) return std::nullopt;
    return parse_expiry_unix(*activation_expires_at);
}

void AgentActivationWorkflow::validate_grant(const AgentActivationGrant& grant,
                                             const char* error_message) const {
    std::string compact_id = grant.device_id;
    compact_id.erase(std::remove(compact_id.begin(), compact_id.end(), '-'), compact_id.end());
    const bool valid_password = grant.mqtt_password.value.size() >= 32 &&
        grant.mqtt_password.value.size() <= 128 &&
        std::all_of(grant.mqtt_password.value.begin(), grant.mqtt_password.value.end(), [](unsigned char ch) {
            return std::isalnum(ch) || ch == '-' || ch == '_';
        });
    if (!grant.tls_required || grant.protocol_version != "3.1.1" ||
        grant.mqtt_client_id != "yunshao-edge-" + compact_id ||
        grant.mqtt_username != "yunshao-device-" + compact_id || !valid_password) {
        throw std::runtime_error(error_message);
    }
}

}  // namespace yunshao::application
