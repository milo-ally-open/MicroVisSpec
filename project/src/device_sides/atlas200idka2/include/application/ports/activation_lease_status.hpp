#pragma once

namespace microvis::application::ports {

class ActivationLeaseStatus {
public:
    virtual ~ActivationLeaseStatus() = default;
    virtual bool expired() const = 0;
};

}  // namespace microvis::application::ports
