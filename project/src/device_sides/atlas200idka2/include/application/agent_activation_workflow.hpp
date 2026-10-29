#pragma once

#include "application/ports/activation_lease_writer.hpp"
#include "application/ports/agent_configuration_store.hpp"
#include "application/sensitive_string.hpp"

#include <cstdint>
#include <optional>
#include <string>

namespace yunshao::application {

struct AgentActivationGrant {
    std::string device_id;
    std::string server_origin;
    std::string mqtt_broker_url;
    std::string mqtt_client_id;
    std::string mqtt_username;
    SensitiveString mqtt_password;
    std::string mqtt_ca_path;
    bool tls_required = false;
    std::string protocol_version;
    std::optional<std::string> activation_expires_at;
};

struct AgentActivationResult {
    bool lease_written = true;
};

class AgentActivationWorkflow {
public:
    AgentActivationWorkflow(ports::AgentConfigurationStore& configuration_store,
                            ports::ActivationLeaseWriter& lease_writer);

    ports::AgentConfigurationStore::Environment read_agent_environment();
    void apply_cli_grant(AgentActivationGrant grant, const std::string& api_ca_path);
    AgentActivationResult apply_polled_grant(AgentActivationGrant grant);
    void refresh_lease(const std::optional<std::string>& activation_expires_at);

private:
    std::optional<std::int64_t> parse_expiry(
        const std::optional<std::string>& activation_expires_at) const;
    void validate_grant(const AgentActivationGrant& grant, const char* error_message) const;

    ports::AgentConfigurationStore& configuration_store_;
    ports::ActivationLeaseWriter& lease_writer_;
};

}  // namespace yunshao::application
