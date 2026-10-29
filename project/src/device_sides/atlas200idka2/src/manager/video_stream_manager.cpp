#include "manager/video_stream_manager.hpp"

#include "media/media_pipeline.hpp"
#include "media/region_osd.hpp"

#include <acl/media/hi_mpi_vpss.h>
#include <algorithm>
#include <chrono>
#include <cmath>
#include <cstdlib>
#include <iostream>
#include <memory>
#include <stdexcept>
#include <thread>
#include <utility>
#include <vector>

namespace yunshao {
namespace {

struct LetterboxInfo { float scale; int left; int top; };

LetterboxInfo letterbox_for(int width, int height, int target_width, int target_height) {
    const float scale = std::min(static_cast<float>(target_width) / width,
                                 static_cast<float>(target_height) / height);
    const int resized_width = static_cast<int>(std::round(width * scale));
    const int resized_height = static_cast<int>(std::round(height * scale));
    return {scale,
            static_cast<int>(std::round((target_width - resized_width) / 2.0F - 0.1F)),
            static_cast<int>(std::round((target_height - resized_height) / 2.0F - 0.1F))};
}

}  // namespace

VideoStreamManager::VideoStreamManager(const EdgeAppConfig& config, InferenceManager& inference,
                                       AlarmCallback on_alarm, std::filesystem::path config_directory,
                                       application::ports::EvidenceFrameEncoder& evidence_frame_encoder,
                                       application::ports::ActivationLeaseStatus& lease_status,
                                       application::ports::RuntimeRuleStateReader& rule_state_reader)
    : config_(config), inference_(inference), alarms_(config.alarm_rules, config.alarm_config_version),
      on_alarm_(std::move(on_alarm)), config_directory_(std::move(config_directory)),
      evidence_frame_encoder_(evidence_frame_encoder),
      lease_status_(lease_status), rule_state_reader_(rule_state_reader) {}

void VideoStreamManager::run(const StreamProfile& stream, const std::string& input_uri,
                             const std::filesystem::path& output_h264,
                             const RtspServerConfig& rtsp_server_config) {
    if (!stream.enabled || !stream.enable_ai) throw std::invalid_argument("stream is disabled");
    const auto& model_profile = config_.require_model(stream.model_id);
    EdgeAppConfig current_config = config_;
    Json command = Json::object();
    auto next_refresh = std::chrono::steady_clock::time_point{};
    bool rules_ready = true;
    const char* state_env = std::getenv("YUNSHAO_RUNTIME_STATE_DIR");
    const auto state_path = std::filesystem::path(state_env && *state_env ? state_env : "/run/yunshao/runtime") / (stream.stream_id + ".json");
    inference_.load(model_profile);
    const double confidence_threshold = stream.confidence_threshold.value_or(model_profile.confidence_threshold);
    const double nms_threshold = stream.nms_threshold.value_or(model_profile.nms_threshold);
    if (stream.rtsp_output.enabled != rtsp_server_config.enabled) {
        throw std::invalid_argument("RTSP server runtime options do not match the stream profile");
    }
    std::unique_ptr<RtspServer> rtsp_server;
    if (rtsp_server_config.enabled) {
        rtsp_server = std::make_unique<RtspServer>(rtsp_server_config);
        std::cout << "rtsp_output_ready stream_id=" << stream.stream_id
                  << " port=" << stream.rtsp_output.port
                  << " mount_path=" << stream.rtsp_output.mount_path << std::endl;
    }

    int reconnect_delay_ms = stream.reconnect_initial_delay_ms;
    while (true) {
        if (lease_status_.expired()) {
            std::cout << "device activation expired; local video runtime is paused" << std::endl;
            return;
        }
        try {
            MediaPipeline pipeline(input_uri, output_h264.string(), stream.output_fps,
                                   model_profile.input_width, model_profile.input_height,
                                   stream.output_width, stream.output_height, stream.input_timeout_ms,
                                   stream.output_bitrate_kbps, stream.rtsp_transport, rtsp_server.get());
            RegionOsd region(pipeline.group(), pipeline.output_channel(), pipeline.output_width(), pipeline.output_height());
            std::vector<Detection> detections;
            std::vector<RegionOsdDetection> overlay;
            std::size_t frames = 0;
            hi_video_frame_info inference_frame{};
            while (true) {
                if (lease_status_.expired()) {
                    std::cout << "device activation expired; local video runtime is paused" << std::endl;
                    return;
                }
                if (!pipeline.next_inference_frame(inference_frame)) break;
                hi_video_frame_info output_frame{};
                if (!pipeline.get_output_frame(output_frame)) {
                    pipeline.release_inference_frame(inference_frame);
                    continue;
                }
                const hi_video_frame& frame = inference_frame.v_frame;
                const hi_u32 height_stride = frame.height_stride[0] == 0 ? frame.height : frame.height_stride[0];
                const size_t bytes = static_cast<size_t>(frame.width_stride[0]) * height_stride * 3 / 2;
                const auto letterbox = letterbox_for(pipeline.width(), pipeline.height(),
                                                     model_profile.input_width, model_profile.input_height);
                InferenceFrame view;
                view.nv21_device = frame.virt_addr[0];
                view.bytes = bytes;
                view.width = static_cast<int>(frame.width);
                view.height = static_cast<int>(frame.height);
                view.stride_width = static_cast<int>(frame.width_stride[0]);
                view.stride_height = static_cast<int>(height_stride);
                view.source_width = pipeline.width();
                view.source_height = pipeline.height();
                view.target_width = pipeline.output_width();
                view.target_height = pipeline.output_height();
                view.letterbox_scale = letterbox.scale;
                view.letterbox_left = letterbox.left;
                view.letterbox_top = letterbox.top;

                try {
                    inference_.infer(stream.model_id, view, confidence_threshold, nms_threshold, detections);
                } catch (...) {
                    pipeline.release_output_frame(output_frame);
                    pipeline.release_inference_frame(inference_frame);
                    throw;
                }
                try {
                    const auto now = std::chrono::steady_clock::now();
                    if (!config_directory_.empty() && now >= next_refresh) {
                        try {
                            const auto snapshot = rule_state_reader_.read();
                            if (snapshot.cloud_command != command || snapshot.config_version != current_config.alarm_config_version) {
                                alarms_ = AlarmFilter(snapshot.rules, snapshot.config_version);
                                current_config.alarm_rules = snapshot.rules;
                                current_config.alarm_config_version = snapshot.config_version;
                                command = snapshot.cloud_command;
                            }
                            rules_ready = snapshot.ready;
                        } catch (const std::exception&) { std::cerr << "alarm_configuration_reload_failed\n"; }
                        next_refresh = now + std::chrono::seconds(1);
                    }
                    const auto alarms = rules_ready ? alarms_.observe(stream.model_id, detections, std::chrono::steady_clock::now()) : std::vector<AlarmTrigger>{};
                    if (on_alarm_ && !alarms.empty()) {
                        std::vector<unsigned char> jpeg;
                        const hi_video_frame& evidence_frame = output_frame.v_frame;
                        const auto width = static_cast<int>(evidence_frame.width);
                        const auto height = static_cast<int>(evidence_frame.height);
                        const auto luma_pitch = static_cast<std::size_t>(evidence_frame.width_stride[0]);
                        const auto chroma_pitch = static_cast<std::size_t>(
                            evidence_frame.width_stride[1] ? evidence_frame.width_stride[1] : evidence_frame.width_stride[0]);
                        const auto aligned_height = static_cast<std::size_t>(
                            evidence_frame.height_stride[0] ? evidence_frame.height_stride[0] : evidence_frame.height);
                        try {
                            jpeg = evidence_frame_encoder_.encode_jpeg(
                                application::ports::EvidenceFrameView{
                                    evidence_frame.virt_addr[0], evidence_frame.virt_addr[1], width, height,
                                    luma_pitch, chroma_pitch, aligned_height}, detections);
                        }
                        catch (const std::exception&) { std::cerr << "alarm_evidence_capture_failed\n"; }
                        for (const auto& alarm : alarms) on_alarm_(alarm, jpeg);
                    }
                } catch (...) {
                    pipeline.release_output_frame(output_frame);
                    pipeline.release_inference_frame(inference_frame);
                    throw;
                }
                overlay.clear();
                overlay.reserve(detections.size());
                for (const auto& detection : detections) {
                    overlay.push_back(RegionOsdDetection{detection.class_id, detection.confidence,
                                                          detection.x, detection.y, detection.width, detection.height});
                }
                region.update(overlay, model_profile.classes);
                try {
                    pipeline.encode_frame(output_frame);
                } catch (...) {
                    pipeline.release_output_frame(output_frame);
                    pipeline.release_inference_frame(inference_frame);
                    throw;
                }
                pipeline.release_output_frame(output_frame);
                pipeline.release_inference_frame(inference_frame);
                ++frames;
                if (frames % static_cast<std::size_t>(stream.output_fps) == 0) {
                    yunshao::write_json_atomic(state_path, Json{{"model_id", stream.model_id}, {"frames", frames},
                        {"updated_at_unix", std::time(nullptr)}, {"runtime_state", "running"},
                        {"cloud_command_id", command.value("command_id", "")},
                        {"rule_config_version", current_config.alarm_config_version}});
                }
                reconnect_delay_ms = stream.reconnect_initial_delay_ms;
            }
            if (stream.live_source) throw InputSourceError("live source reached end-of-stream");
            if (frames == 0 || pipeline.encoded_frames() != frames) {
                throw std::runtime_error("stream ended without complete decoded, inferred, and encoded frames");
            }
            return;
        } catch (const InputSourceError& error) {
            if (!stream.live_source) throw;
            std::cerr << "camera_source_unavailable stream_id=" << stream.stream_id
                      << " retry_in_ms=" << reconnect_delay_ms << " cause=" << error.what() << '\n';
            std::this_thread::sleep_for(std::chrono::milliseconds(reconnect_delay_ms));
            reconnect_delay_ms = std::min(stream.reconnect_max_delay_ms, reconnect_delay_ms * 2);
        }
    }
}

}  // namespace yunshao
