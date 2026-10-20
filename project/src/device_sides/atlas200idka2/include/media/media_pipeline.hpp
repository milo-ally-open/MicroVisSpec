#pragma once

#include "media/rtsp_server.hpp"

#include <acl/dvpp/hi_dvpp_vdec.h>

#include <cstddef>
#include <memory>
#include <stdexcept>
#include <string>

class InputSourceError : public std::runtime_error {
public:
    using std::runtime_error::runtime_error;
};

class MediaPipeline {
public:
    MediaPipeline(const std::string& input, const std::string& output, int output_fps,
                  int infer_width, int infer_height, int output_width, int output_height,
                  int input_timeout_ms, int output_bitrate_kbps, const std::string& rtsp_transport,
                  yunshao::RtspServer* rtsp_server);
    MediaPipeline(const MediaPipeline&) = delete;
    MediaPipeline& operator=(const MediaPipeline&) = delete;
    ~MediaPipeline();

    int width() const;
    int height() const;
    int output_width() const;
    int output_height() const;
    int fps() const;
    int infer_width() const;
    int infer_height() const;
    int group() const;
    int output_channel() const;
    bool next_inference_frame(hi_video_frame_info& frame);
    bool get_output_frame(hi_video_frame_info& frame);
    void release_inference_frame(const hi_video_frame_info& frame);
    void release_output_frame(const hi_video_frame_info& frame);
    void encode_frame(const hi_video_frame_info& frame);
    std::size_t encoded_frames() const;

private:
    class Impl;
    std::unique_ptr<Impl> impl_;
};
