#include "media/rtsp_server.hpp"

#include <algorithm>
#include <cmath>
#include <exception>
#include <iostream>
#include <mutex>
#include <stdexcept>
#include <string>

namespace yunshao {
namespace {

void require_element_factory(const char* name) {
    GstElementFactory* factory = gst_element_factory_find(name);
    if (factory == nullptr) throw std::runtime_error(std::string("required GStreamer element is unavailable: ") + name);
    gst_object_unref(factory);
}

bool gst_buffer_is_keyframe(const std::uint8_t* data, std::size_t size) {
    for (std::size_t i = 0; i + 3 < size;) {
        std::size_t prefix = 0;
        if (data[i] == 0 && data[i + 1] == 0 && data[i + 2] == 1) prefix = 3;
        else if (i + 4 < size && data[i] == 0 && data[i + 1] == 0 &&
                 data[i + 2] == 0 && data[i + 3] == 1) prefix = 4;
        if (prefix == 0) {
            ++i;
            continue;
        }
        if (i + prefix < size && (data[i + prefix] & 0x1fU) == 5U) return true;
        i += prefix;
    }
    return false;
}

}  // namespace

RtspServer::RtspServer(RtspServerConfig config) : fps_(config.fps) {
    if (!config.enabled) throw std::invalid_argument("RTSP server configuration is disabled");
    if (config.listen_address.empty() || config.port < 1024 || config.port > 65535 ||
        config.mount_path.empty() || config.mount_path.front() != '/' || fps_ < 1 ||
        config.bitrate_kbps < 1 || config.max_queue_ms < 1) {
        throw std::invalid_argument("RTSP server configuration is incomplete");
    }

    const long double queue_bytes = static_cast<long double>(config.bitrate_kbps) * 1000.0L *
                                    static_cast<long double>(config.max_queue_ms) / 8000.0L;
    max_queue_bytes_ = static_cast<std::uint64_t>(std::max<long double>(65536.0L, std::ceil(queue_bytes)));

    static std::once_flag gst_initialized;
    std::call_once(gst_initialized, [] { gst_init(nullptr, nullptr); });
    require_element_factory("appsrc");
    require_element_factory("rtph264pay");

    server_ = gst_rtsp_server_new();
    if (server_ == nullptr) throw std::runtime_error("cannot create GStreamer RTSP server");
    context_ = g_main_context_new();
    loop_ = context_ == nullptr ? nullptr : g_main_loop_new(context_, FALSE);
    if (context_ == nullptr || loop_ == nullptr) {
        if (loop_ != nullptr) g_main_loop_unref(loop_);
        if (context_ != nullptr) g_main_context_unref(context_);
        g_object_unref(server_);
        loop_ = nullptr;
        context_ = nullptr;
        server_ = nullptr;
        throw std::runtime_error("cannot create GStreamer RTSP event loop");
    }

    try {
        const std::string service = std::to_string(config.port);
        gst_rtsp_server_set_address(server_, config.listen_address.c_str());
        gst_rtsp_server_set_service(server_, service.c_str());

        GstRTSPMediaFactory* factory = gst_rtsp_media_factory_new();
        if (factory == nullptr) {
            throw std::runtime_error("cannot create GStreamer RTSP media factory");
        }
        const std::string launch = "( appsrc name=edge_src is-live=true format=time block=false ! "
                                   "rtph264pay name=pay0 pt=96 config-interval=1 )";
        gst_rtsp_media_factory_set_launch(factory, launch.c_str());
        gst_rtsp_media_factory_set_shared(factory, TRUE);
        gst_rtsp_media_factory_set_protocols(factory, GST_RTSP_LOWER_TRANS_TCP);
        g_signal_connect(factory, "media-configure", G_CALLBACK(&RtspServer::configure_media), this);

        GstRTSPMountPoints* mounts = gst_rtsp_server_get_mount_points(server_);
        if (mounts == nullptr) {
            g_object_unref(factory);
            throw std::runtime_error("cannot get GStreamer RTSP mount points");
        }
        gst_rtsp_mount_points_add_factory(mounts, config.mount_path.c_str(), factory);
        g_object_unref(mounts);

        GError* error = nullptr;
        source_ = gst_rtsp_server_create_source(server_, nullptr, &error);
        if (source_ == nullptr) {
            const std::string message = error == nullptr ? "cannot bind RTSP listener" : error->message;
            if (error != nullptr) g_error_free(error);
            throw std::runtime_error("cannot create RTSP listener: " + message);
        }
        source_id_ = g_source_attach(source_, context_);
        if (source_id_ == 0) throw std::runtime_error("cannot attach RTSP listener to event loop");
        loop_thread_ = std::thread(&RtspServer::run_loop, this);
    } catch (...) {
        if (source_ != nullptr) {
            g_source_destroy(source_);
            g_source_unref(source_);
            source_ = nullptr;
        }
        if (loop_ != nullptr) g_main_loop_unref(loop_);
        if (context_ != nullptr) g_main_context_unref(context_);
        if (server_ != nullptr) g_object_unref(server_);
        loop_ = nullptr;
        context_ = nullptr;
        server_ = nullptr;
        throw;
    }
}

RtspServer::~RtspServer() {
    if (loop_ != nullptr) {
        g_main_loop_quit(loop_);
        g_main_context_wakeup(context_);
    }
    if (loop_thread_.joinable()) loop_thread_.join();
    if (source_ != nullptr) {
        g_source_destroy(source_);
        g_source_unref(source_);
    }
    {
        std::lock_guard<std::mutex> lock(appsrc_mutex_);
        if (active_appsrc_ != nullptr) gst_object_unref(active_appsrc_);
        active_appsrc_ = nullptr;
        active_media_ = nullptr;
    }
    if (loop_ != nullptr) g_main_loop_unref(loop_);
    if (context_ != nullptr) g_main_context_unref(context_);
    if (server_ != nullptr) g_object_unref(server_);
}

bool RtspServer::push_access_unit(const std::uint8_t* data, std::size_t size,
                                  std::uint64_t duration) {
    if (data == nullptr || size == 0) return false;
    std::lock_guard<std::mutex> lock(appsrc_mutex_);
    if (active_appsrc_ == nullptr) return false;
    guint64 queued_bytes = 0;
    g_object_get(active_appsrc_, "current-level-bytes", &queued_bytes, nullptr);
    if (queued_bytes >= max_queue_bytes_ || size > max_queue_bytes_ - queued_bytes) {
        ++dropped_frames_;
        return false;
    }
    GstBuffer* buffer = gst_buffer_new_allocate(nullptr, size, nullptr);
    if (buffer == nullptr) {
        ++dropped_frames_;
        return false;
    }
    if (gst_buffer_fill(buffer, 0, data, size) != size) {
        gst_buffer_unref(buffer);
        ++dropped_frames_;
        return false;
    }
    GST_BUFFER_DURATION(buffer) = duration;
    if (!gst_buffer_is_keyframe(data, size)) GST_BUFFER_FLAG_SET(buffer, GST_BUFFER_FLAG_DELTA_UNIT);
    const GstFlowReturn result = gst_app_src_push_buffer(active_appsrc_, buffer);
    if (result != GST_FLOW_OK) {
        ++dropped_frames_;
        return false;
    }
    return true;
}

std::uint64_t RtspServer::dropped_frames() const {
    std::lock_guard<std::mutex> lock(appsrc_mutex_);
    return dropped_frames_;
}

void RtspServer::configure_media(GstRTSPMediaFactory*, GstRTSPMedia* media, gpointer user_data) {
    auto* self = static_cast<RtspServer*>(user_data);
    try {
        self->on_configure_media(media);
    } catch (const std::exception& error) {
        self->media_config_failed_.store(true);
        g_warning("RTSP media configuration failed: %s", error.what());
    } catch (...) {
        self->media_config_failed_.store(true);
        g_warning("RTSP media configuration failed with an unknown error");
    }
}

void RtspServer::media_unprepared(GstRTSPMedia* media, gpointer user_data) {
    static_cast<RtspServer*>(user_data)->on_media_unprepared(media);
}

void RtspServer::on_configure_media(GstRTSPMedia* media) {
    GstElement* pipeline = gst_rtsp_media_get_element(media);
    if (pipeline == nullptr) throw std::runtime_error("RTSP media pipeline is unavailable");
    GstElement* source = gst_bin_get_by_name(GST_BIN(pipeline), "edge_src");
    g_object_unref(pipeline);
    if (source == nullptr || !GST_IS_APP_SRC(source)) {
        if (source != nullptr) gst_object_unref(source);
        throw std::runtime_error("RTSP media pipeline has no appsrc element");
    }
    GstCaps* caps = gst_caps_new_simple("video/x-h264",
                                         "stream-format", G_TYPE_STRING, "byte-stream",
                                         "alignment", G_TYPE_STRING, "au",
                                         "framerate", GST_TYPE_FRACTION, fps_, 1,
                                         nullptr);
    g_object_set(source, "is-live", TRUE, "format", GST_FORMAT_TIME, "block", FALSE,
                 "do-timestamp", TRUE, "min-latency", static_cast<gint64>(0),
                 "max-bytes", static_cast<guint64>(max_queue_bytes_),
                 "caps", caps, nullptr);
    gst_caps_unref(caps);

    std::lock_guard<std::mutex> lock(appsrc_mutex_);
    if (active_appsrc_ != nullptr) gst_object_unref(active_appsrc_);
    active_appsrc_ = GST_APP_SRC(source);
    active_media_ = media;
    g_signal_connect(media, "unprepared", G_CALLBACK(&RtspServer::media_unprepared), this);
}

void RtspServer::on_media_unprepared(GstRTSPMedia* media) {
    std::lock_guard<std::mutex> lock(appsrc_mutex_);
    if (active_media_ == media) {
        if (active_appsrc_ != nullptr) gst_object_unref(active_appsrc_);
        active_appsrc_ = nullptr;
        active_media_ = nullptr;
    }
}

void RtspServer::run_loop() {
    g_main_loop_run(loop_);
}

}  // namespace yunshao
