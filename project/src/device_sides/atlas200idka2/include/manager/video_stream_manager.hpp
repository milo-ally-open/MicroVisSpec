#pragma once

#include "common/config.hpp"
#include "application/ports/activation_lease_status.hpp"
#include "application/ports/evidence_frame_encoder.hpp"
#include "application/ports/runtime_rule_state_reader.hpp"
#include "manager/alarm_filter.hpp"
#include "manager/inference_manager.hpp"
#include "media/rtsp_server.hpp"

#include <filesystem>
#include <functional>
#include <string>

namespace yunshao {

class VideoStreamManager {
public:
    using AlarmCallback = std::function<void(const AlarmTrigger&, const std::vector<unsigned char>&)>;

    VideoStreamManager(const EdgeAppConfig& config, InferenceManager& inference,
                       AlarmCallback on_alarm, std::filesystem::path config_directory,
                       application::ports::EvidenceFrameEncoder& evidence_frame_encoder,
                       application::ports::ActivationLeaseStatus& lease_status,
                       application::ports::RuntimeRuleStateReader& rule_state_reader);
    void run(const StreamProfile& stream, const std::string& input_uri,
             const std::filesystem::path& output_h264,
             const RtspServerConfig& rtsp_server_config);

private:
    const EdgeAppConfig& config_;
    InferenceManager& inference_;
    AlarmFilter alarms_;
    AlarmCallback on_alarm_;
    std::filesystem::path config_directory_;
    application::ports::EvidenceFrameEncoder& evidence_frame_encoder_;
    application::ports::ActivationLeaseStatus& lease_status_;
    application::ports::RuntimeRuleStateReader& rule_state_reader_;
};

}  // namespace yunshao
