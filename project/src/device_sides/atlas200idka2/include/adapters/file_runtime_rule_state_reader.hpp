#pragma once

#include "application/ports/runtime_rule_state_reader.hpp"

#include <filesystem>

namespace yunshao::adapters {

class FileRuntimeRuleStateReader final : public application::ports::RuntimeRuleStateReader {
public:
    explicit FileRuntimeRuleStateReader(std::filesystem::path config_directory);
    application::ports::RuntimeRuleState read() override;

private:
    std::filesystem::path config_directory_;
};

}  // namespace yunshao::adapters
