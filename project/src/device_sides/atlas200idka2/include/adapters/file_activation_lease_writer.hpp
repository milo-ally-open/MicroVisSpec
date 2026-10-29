#pragma once

#include "application/ports/activation_lease_writer.hpp"

#include <filesystem>

namespace yunshao::adapters {

class FileActivationLeaseWriter final : public application::ports::ActivationLeaseWriter {
public:
    explicit FileActivationLeaseWriter(std::filesystem::path lease_path);

    void write_expiry_unix(std::optional<std::int64_t> expires_at_unix) override;

private:
    std::filesystem::path lease_path_;
};

}  // namespace yunshao::adapters
