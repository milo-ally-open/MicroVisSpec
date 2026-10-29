#pragma once

#include "application/ports/activation_lease_status.hpp"

namespace yunshao::application {

class ActivationLeaseGate {
public:
    explicit ActivationLeaseGate(ports::ActivationLeaseStatus& lease_status);

    bool expired() const;
    void wait_until_valid() const;

private:
    ports::ActivationLeaseStatus& lease_status_;
};

}  // namespace yunshao::application
