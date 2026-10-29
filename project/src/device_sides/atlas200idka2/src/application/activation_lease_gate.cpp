#include "application/activation_lease_gate.hpp"

#include <chrono>
#include <iostream>
#include <thread>

namespace yunshao::application {

ActivationLeaseGate::ActivationLeaseGate(ports::ActivationLeaseStatus& lease_status)
    : lease_status_(lease_status) {}

bool ActivationLeaseGate::expired() const { return lease_status_.expired(); }

void ActivationLeaseGate::wait_until_valid() const {
    while (lease_status_.expired()) {
        std::cout << "设备授权已到期，runtime 暂停；网页续期后将自动恢复" << std::endl;
        std::this_thread::sleep_for(std::chrono::seconds(5));
    }
}

}  // namespace yunshao::application
