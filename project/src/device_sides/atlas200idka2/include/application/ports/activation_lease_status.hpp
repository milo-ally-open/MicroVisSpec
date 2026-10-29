#pragma once

namespace yunshao::application::ports {

class ActivationLeaseStatus {
public:
    virtual ~ActivationLeaseStatus() = default;
    virtual bool expired() const = 0;
};

}  // namespace yunshao::application::ports
