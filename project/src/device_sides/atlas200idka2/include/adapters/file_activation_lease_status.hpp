#pragma once

#include "application/ports/activation_lease_status.hpp"

#include <filesystem>

namespace microvis::adapters {

class FileActivationLeaseStatus final : public application::ports::ActivationLeaseStatus {
public:
    explicit FileActivationLeaseStatus(std::filesystem::path lease_path);

    bool expired() const override;

private:
    std::filesystem::path lease_path_;
};

}  // namespace microvis::adapters
