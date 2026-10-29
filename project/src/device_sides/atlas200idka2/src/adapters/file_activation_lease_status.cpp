#include "adapters/file_activation_lease_status.hpp"

#include "common/cloud_config.hpp"

#include <cstdint>
#include <ctime>
#include <utility>

namespace yunshao::adapters {

FileActivationLeaseStatus::FileActivationLeaseStatus(std::filesystem::path lease_path)
    : lease_path_(std::move(lease_path)) {}

bool FileActivationLeaseStatus::expired() const {
    if (!std::filesystem::exists(lease_path_)) return false;
    try {
        const auto lease = yunshao::read_json_file(lease_path_);
        if (!lease.contains("expires_at_unix") || lease["expires_at_unix"].is_null()) return false;
        return lease.at("expires_at_unix").get<std::int64_t>() <= std::time(nullptr);
    } catch (...) {
        return true;
    }
}

}  // namespace yunshao::adapters
