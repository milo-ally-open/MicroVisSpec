#include "manager/alarm_filter.hpp"

#include <algorithm>

namespace yunshao {
namespace {
std::string key(const std::string& model_id, const std::string& class_name) {
    return model_id + "\n" + class_name;
}
}

AlarmFilter::AlarmFilter(const std::vector<AlarmRule>& rules, int config_version)
    : config_version_(config_version) {
    if (config_version_ < 1) throw std::invalid_argument("alarm config version must be positive");
    replace_rules(rules);
}

void AlarmFilter::replace_rules(const std::vector<AlarmRule>& rules) {
    std::map<std::string, AlarmRule> replacement;
    for (const auto& rule : rules) replacement.emplace(key(rule.model_id, rule.class_name), rule);
    rules_ = std::move(replacement);
    for (auto state = states_.begin(); state != states_.end();) {
        if (rules_.find(state->first) == rules_.end()) state = states_.erase(state);
        else ++state;
    }
}

std::vector<AlarmTrigger> AlarmFilter::observe(const std::string& model_id,
                                               const std::vector<Detection>& detections,
                                               std::chrono::steady_clock::time_point now) {
    std::map<std::string, double> observed;
    for (const auto& detection : detections) {
        const auto found = rules_.find(key(model_id, detection.label));
        if (found == rules_.end() || !found->second.enabled || detection.confidence < found->second.confidence_threshold) continue;
        auto& confidence = observed[detection.label];
        confidence = std::max(confidence, static_cast<double>(detection.confidence));
    }

    std::vector<AlarmTrigger> triggers;
    for (const auto& item : rules_) {
        const auto& rule = item.second;
        if (rule.model_id != model_id || !rule.enabled) continue;
        auto& state = states_[item.first];
        const auto detection = observed.find(rule.class_name);
        if (detection == observed.end()) {
            state.active = false;
            state.emitted_in_window = false;
            continue;
        }
        if (!state.active) {
            state.active = true;
            state.first_seen = now;
            state.emitted_in_window = false;
        }
        const auto elapsed = std::chrono::duration_cast<std::chrono::milliseconds>(now - state.first_seen).count();
        const auto cooldown_elapsed = state.last_trigger == std::chrono::steady_clock::time_point{} ||
            std::chrono::duration_cast<std::chrono::milliseconds>(now - state.last_trigger).count() >= rule.cooldown_ms;
        if (!state.emitted_in_window && cooldown_elapsed && elapsed >= rule.min_duration_ms) {
            triggers.push_back(AlarmTrigger{rule.model_id, rule.class_name, rule.event_type,
                                            rule.class_attribute, rule.severity, config_version_,
                                            detection->second, static_cast<int>(elapsed)});
            state.last_trigger = now;
            state.emitted_in_window = true;
        }
    }
    return triggers;
}

}  // namespace yunshao
