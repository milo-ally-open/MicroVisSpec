#pragma once

#include "application/ports/activation_lease_status.hpp"

#include <filesystem>

namespace yunshao::adapters {

class FileActivationLeaseStatus final : public application::ports::ActivationLeaseStatus {
public:
    explicit FileActivationLeaseStatus(std::filesystem::path lease_path);

    bool expired() const override;

private:
    std::filesystem::path lease_path_;
};

}  // namespace yunshao::adapters
