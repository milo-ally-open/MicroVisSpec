#pragma once

#include "application/sensitive_string.hpp"

#include <string>

namespace yunshao::application::ports {

struct HttpsRequest {
    const std::string& url;
    const std::string& ca_path;
    const std::string* body = nullptr;
    const std::string* username = nullptr;
    const std::string* password = nullptr;
    long connect_timeout_seconds = 0;
    long timeout_seconds = 25;
    bool no_signal = false;
};

enum class HttpsTransportFailure {
    none,
    global_initialization,
    client_initialization,
    transfer,
};

struct HttpsResponse {
    HttpsTransportFailure failure = HttpsTransportFailure::none;
    long status = 0;
};

class HttpsTransport {
public:
    virtual ~HttpsTransport() = default;
    virtual HttpsResponse perform(const HttpsRequest& request,
                                  SensitiveString& response_body) = 0;
};

}  // namespace yunshao::application::ports
