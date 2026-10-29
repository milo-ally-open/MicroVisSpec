#pragma once

#include "common/config.hpp"

namespace yunshao::application::ports {

struct RuntimeRuleState {
    std::vector<AlarmRule> rules;
    int config_version = 1;
    Json cloud_command = Json::object();
    bool ready = true;
};

class RuntimeRuleStateReader {
public:
    virtual ~RuntimeRuleStateReader() = default;
    virtual RuntimeRuleState read() = 0;
};

}  // namespace yunshao::application::ports
