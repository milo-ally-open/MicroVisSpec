#pragma once

#include "ai_interface.hpp"

#include <chrono>
#include <map>
#include <string>
#include <vector>

namespace yunshao {

struct AlarmTrigger {
    std::string model_id;
    std::string class_name;
    std::string event_type;
    std::string class_attribute;
    std::string severity;
    int config_version = 1;
    double confidence = 0.0;
    int observed_duration_ms = 0;
};

class AlarmFilter {
public:
    AlarmFilter(const std::vector<AlarmRule>& rules, int config_version);
    std::vector<AlarmTrigger> observe(const std::string& model_id,
                                      const std::vector<Detection>& detections,
                                      std::chrono::steady_clock::time_point now);
    void replace_rules(const std::vector<AlarmRule>& rules);

private:
    struct State {
        std::chrono::steady_clock::time_point first_seen{};
        std::chrono::steady_clock::time_point last_trigger{};
        bool active = false;
        bool emitted_in_window = false;
    };
    std::map<std::string, AlarmRule> rules_;
    std::map<std::string, State> states_;
    int config_version_ = 1;
};

}  // namespace yunshao
