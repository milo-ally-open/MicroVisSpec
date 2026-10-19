#pragma once

#include "common/config.hpp"
#include "manager/alarm_filter.hpp"
#include "manager/inference_manager.hpp"

#include <filesystem>
#include <functional>
#include <string>

namespace yunshao {

class VideoStreamManager {
public:
    using AlarmCallback = std::function<void(const AlarmTrigger&)>;

    VideoStreamManager(const EdgeAppConfig& config, InferenceManager& inference,
                       AlarmCallback on_alarm = {});
    void run(const StreamProfile& stream, const std::string& input_uri,
             const std::filesystem::path& output_h264);

private:
    const EdgeAppConfig& config_;
    InferenceManager& inference_;
    AlarmFilter alarms_;
    AlarmCallback on_alarm_;
};

}  // namespace yunshao
