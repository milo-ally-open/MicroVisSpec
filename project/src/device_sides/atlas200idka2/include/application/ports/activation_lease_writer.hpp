#pragma once

#include <cstdint>
#include <optional>

namespace microvis::application::ports {

class ActivationLeaseWriter {
public:
    virtual ~ActivationLeaseWriter() = default;

    virtual void write_expiry_unix(std::optional<std::int64_t> expires_at_unix) = 0;
};

}  // namespace microvis::application::ports
