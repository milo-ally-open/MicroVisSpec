#pragma once

#include <cstdint>
#include <optional>

namespace yunshao::application::ports {

class ActivationLeaseWriter {
public:
    virtual ~ActivationLeaseWriter() = default;

    virtual void write_expiry_unix(std::optional<std::int64_t> expires_at_unix) = 0;
};

}  // namespace yunshao::application::ports
