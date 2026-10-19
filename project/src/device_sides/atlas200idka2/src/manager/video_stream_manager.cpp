#include "manager/video_stream_manager.hpp"

#include "media/media_pipeline.hpp"
#include "media/region_osd.hpp"

#include <acl/media/hi_mpi_vpss.h>

#include <algorithm>
#include <chrono>
#include <cmath>
#include <iostream>
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
                                       AlarmCallback on_alarm)
    : config_(config), inference_(inference), alarms_(config.alarm_rules, config.alarm_config_version), on_alarm_(std::move(on_alarm)) {}

void VideoStreamManager::run(const StreamProfile& stream, const std::string& input_uri,
                             const std::filesystem::path& output_h264) {
    if (!stream.enabled || !stream.enable_ai) throw std::invalid_argument("stream is disabled");
    const auto& model_profile = config_.require_model(stream.model_id);
    inference_.load(model_profile);
    const double confidence_threshold = stream.confidence_threshold.value_or(model_profile.confidence_threshold);
    const double nms_threshold = stream.nms_threshold.value_or(model_profile.nms_threshold);

    int reconnect_delay_ms = stream.reconnect_initial_delay_ms;
    while (true) {
        try {
            MediaPipeline pipeline(input_uri, output_h264.string(), stream.output_fps,
                                   model_profile.input_width, model_profile.input_height,
                                   stream.output_width, stream.output_height, stream.input_timeout_ms,
                                   stream.output_bitrate_kbps, stream.rtsp_transport);
            RegionOsd region(pipeline.group(), pipeline.output_channel(), pipeline.output_width(), pipeline.output_height());
            std::vector<Detection> detections;
            std::vector<RegionOsdDetection> overlay;
            std::size_t frames = 0;
            hi_video_frame_info inference_frame{};
            while (pipeline.next_inference_frame(inference_frame)) {
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
                    pipeline.release_inference_frame(inference_frame);
                    throw;
                }
                try {
                    const auto alarms = alarms_.observe(stream.model_id, detections, std::chrono::steady_clock::now());
                    if (on_alarm_) for (const auto& alarm : alarms) on_alarm_(alarm);
                } catch (...) {
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
                hi_video_frame_info output_frame{};
                pipeline.get_output_frame(output_frame);
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
