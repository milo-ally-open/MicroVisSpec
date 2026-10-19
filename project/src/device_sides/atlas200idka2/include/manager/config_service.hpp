#pragma once

#include "common/config.hpp"

#include <filesystem>
#include <mutex>

namespace yunshao {

class ConfigService {
public:
    explicit ConfigService(std::filesystem::path directory);
    EdgeAppConfig snapshot() const;
    void reload();

private:
    std::filesystem::path directory_;
    mutable std::mutex mutex_;
    EdgeAppConfig config_;
};

}  // namespace yunshao
