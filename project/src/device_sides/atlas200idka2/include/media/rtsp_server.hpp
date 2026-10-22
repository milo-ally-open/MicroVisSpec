#pragma once
#include "media/h264_parameter_sets.hpp"

#include <gst/app/gstappsrc.h>
#include <gst/rtsp-server/rtsp-server.h>

#include <cstddef>
#include <cstdint>
#include <atomic>
#include <mutex>
#include <string>
#include <thread>

namespace yunshao {

struct RtspServerConfig {
    bool enabled = false;
    std::string listen_address;
    int port = 0;
    std::string mount_path;
    int fps = 0;
    int bitrate_kbps = 0;
    int max_queue_ms = 0;
};

class RtspServer {
public:
    explicit RtspServer(RtspServerConfig config);
    RtspServer(const RtspServer&) = delete;
    RtspServer& operator=(const RtspServer&) = delete;
    ~RtspServer();

    bool push_access_unit(const std::uint8_t* data, std::size_t size,
                          std::uint64_t duration);
    std::uint64_t dropped_frames() const;
    bool failed() const { return media_config_failed_.load(); }

private:
    static void configure_media(GstRTSPMediaFactory* factory, GstRTSPMedia* media, gpointer user_data);
    static void media_unprepared(GstRTSPMedia* media, gpointer user_data);
    void on_configure_media(GstRTSPMedia* media);
    void on_media_unprepared(GstRTSPMedia* media);
    void run_loop();

    int fps_ = 0;
    std::uint64_t max_queue_bytes_ = 0;
    GMainContext* context_ = nullptr;
    GMainLoop* loop_ = nullptr;
    GstRTSPServer* server_ = nullptr;
    GSource* source_ = nullptr;
    guint source_id_ = 0;
    GstRTSPMedia* active_media_ = nullptr;
    GstAppSrc* active_appsrc_ = nullptr;
    mutable std::mutex appsrc_mutex_;
    std::thread loop_thread_;
    std::uint64_t dropped_frames_ = 0;
    std::atomic<bool> media_config_failed_{false};
    H264ParameterSets parameter_sets_;
};

}  // namespace yunshao
