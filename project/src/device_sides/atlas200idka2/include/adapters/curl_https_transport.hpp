#pragma once

#include "application/ports/https_transport.hpp"

namespace yunshao::adapters {

class CurlHttpsTransport final : public application::ports::HttpsTransport {
public:
    application::ports::HttpsResponse perform(
        const application::ports::HttpsRequest& request,
        application::SensitiveString& response_body) override;
};

}  // namespace yunshao::adapters
