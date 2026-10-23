#include "manager/video_stream_manager.hpp"

#include "media/media_pipeline.hpp"
#include "media/region_osd.hpp"
#include "common/cloud_config.hpp"

#include <acl/media/hi_mpi_vpss.h>
#include <acl/acl.h>
#include <opencv2/imgproc.hpp>
#include <opencv2/imgcodecs.hpp>

#include <algorithm>
#include <chrono>
#include <cmath>
#include <cstdlib>
#include <ctime>
#include <cstring>
#include <iostream>
#include <memory>
#include <stdexcept>
#include <thread>
#include <utility>
#include <vector>

namespace yunshao {
namespace {

struct LetterboxInfo { float scale; int left; int top; };

bool activation_lease_expired() {
    const char* configured = std::getenv("YUNSHAO_ACTIVATION_LEASE_FILE");
    const std::filesystem::path path = configured && *configured ? configured : "/var/lib/yunshao/activation-lease.json";
    if (!std::filesystem::exists(path)) return false;  // Legacy deployments without a lease remain unchanged.
    try {
        const auto lease = yunshao::read_json_file(path);
        if (!lease.contains("expires_at_unix") || lease["expires_at_unix"].is_null()) return false;
        return lease.at("expires_at_unix").get<std::int64_t>() <= std::time(nullptr);
    } catch (...) {
        return true;  // A corrupt lease fails closed and pauses local video processing.
    }
}

LetterboxInfo letterbox_for(int width, int height, int target_width, int target_height) {
    const float scale = std::min(static_cast<float>(target_width) / width,
                                 static_cast<float>(target_height) / height);
    const int resized_width = static_cast<int>(std::round(width * scale));
    const int resized_height = static_cast<int>(std::round(height * scale));
    return {scale,
            static_cast<int>(std::round((target_width - resized_width) / 2.0F - 0.1F)),
            static_cast<int>(std::round((target_height - resized_height) / 2.0F - 0.1F))};
}

std::vector<unsigned char> capture_evidence(const hi_video_frame& frame,
                                          const std::vector<Detection>& detections) {
    const int width = static_cast<int>(frame.width);
    const int height = static_cast<int>(frame.height);
    const auto pitch = frame.width_stride[0];
    const auto chroma_pitch = frame.width_stride[1] ? frame.width_stride[1] : pitch;
    const auto aligned_height = frame.height_stride[0] ? frame.height_stride[0] : frame.height;
    std::vector<unsigned char> luma(static_cast<std::size_t>(pitch) * height);
    std::vector<unsigned char> chroma(static_cast<std::size_t>(chroma_pitch) * height / 2);
    const auto* uv = frame.virt_addr[1] ? static_cast<unsigned char*>(frame.virt_addr[1]) :
        static_cast<unsigned char*>(frame.virt_addr[0]) + static_cast<std::size_t>(pitch) * aligned_height;
    if (aclrtMemcpy(luma.data(), luma.size(), frame.virt_addr[0], luma.size(), ACL_MEMCPY_DEVICE_TO_HOST) != ACL_SUCCESS ||
        aclrtMemcpy(chroma.data(), chroma.size(), uv, chroma.size(), ACL_MEMCPY_DEVICE_TO_HOST) != ACL_SUCCESS)
        throw std::runtime_error("cannot copy evidence frame from VPSS");
    cv::Mat nv21(height * 3 / 2, width, CV_8UC1);
    for (int y = 0; y < height; ++y) std::memcpy(nv21.ptr(y), luma.data() + y * pitch, width);
    for (int y = 0; y < height / 2; ++y) std::memcpy(nv21.ptr(height + y), chroma.data() + y * chroma_pitch, width);
    cv::Mat image;
    cv::cvtColor(nv21, image, cv::COLOR_YUV2BGR_NV21);
    for (const auto& detection : detections) {
        const cv::Rect box = cv::Rect(static_cast<int>(detection.x), static_cast<int>(detection.y),
            static_cast<int>(detection.width), static_cast<int>(detection.height)) & cv::Rect(0, 0, width, height);
        if (box.empty()) continue;
        cv::rectangle(image, box, cv::Scalar(32, 210, 64), 2);
        cv::putText(image, detection.label, cv::Point(box.x, std::max(18, box.y - 4)),
                    cv::FONT_HERSHEY_SIMPLEX, 0.5, cv::Scalar(32, 210, 64), 1);
    }
    std::vector<unsigned char> jpeg;
    if (!cv::imencode(".jpg", image, jpeg, {cv::IMWRITE_JPEG_QUALITY, 85}))
        throw std::runtime_error("cannot encode evidence JPEG");
    return jpeg;
}

}  // namespace

VideoStreamManager::VideoStreamManager(const EdgeAppConfig& config, InferenceManager& inference,
                                       AlarmCallback on_alarm, std::filesystem::path config_directory)
    : config_(config), inference_(inference), alarms_(config.alarm_rules, config.alarm_config_version),
      on_alarm_(std::move(on_alarm)), config_directory_(std::move(config_directory)) {}

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
        if (activation_lease_expired()) {
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
                if (activation_lease_expired()) {
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
                            const auto snapshot = EdgeAppConfig::load(config_directory_);
                            const auto desired = yunshao::read_json_file(config_directory_ / "alarm_rules.json").value("cloud_command", Json::object());
                            if (desired != command || snapshot.alarm_config_version != current_config.alarm_config_version) {
                                alarms_ = AlarmFilter(snapshot.alarm_rules, snapshot.alarm_config_version);
                                current_config = snapshot;
                                command = desired;
                            }
                            rules_ready = command.empty();
                            if (!command.empty()) {
                                try {
                                    const auto ready = yunshao::read_json_file(config_directory_ / "cloud-ready.json");
                                    rules_ready = ready.value("command_id", "") == command.value("command_id", "") &&
                                        ready.value("updated_at_unix", std::int64_t{0}) < std::time(nullptr);
                                } catch (...) { rules_ready = false; }
                            }
                        } catch (const std::exception&) { std::cerr << "alarm_configuration_reload_failed\n"; }
                        next_refresh = now + std::chrono::seconds(1);
                    }
                    const auto alarms = rules_ready ? alarms_.observe(stream.model_id, detections, std::chrono::steady_clock::now()) : std::vector<AlarmTrigger>{};
                    if (on_alarm_ && !alarms.empty()) {
                        std::vector<unsigned char> jpeg;
                        try { jpeg = capture_evidence(output_frame.v_frame, detections); }
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
