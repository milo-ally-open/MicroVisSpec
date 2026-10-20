#include "media/media_pipeline.hpp"

#include <acl/dvpp/hi_dvpp_vdec.h>
#include <acl/dvpp/hi_dvpp_venc.h>
#include <acl/dvpp/hi_mpi_sys.h>
#include <acl/media/hi_mpi_vpss.h>

extern "C" {
#include <libavcodec/avcodec.h>
#include <libavcodec/bsf.h>
#include <libavformat/avformat.h>
#include <libavutil/avutil.h>
#include <libavutil/dict.h>
}

#include <array>
#include <algorithm>
#include <atomic>
#include <chrono>
#include <cstdint>
#include <fstream>
#include <memory>
#include <stdexcept>
#include <string>
#include <vector>

class MediaPipeline::Impl {
public:
    Impl(const std::string& input, const std::string& output, int output_fps,
         int infer_width, int infer_height, int output_width, int output_height,
         int input_timeout_ms, int output_bitrate_kbps, const std::string& rtsp_transport,
         yunshao::RtspServer* rtsp_server)
        : infer_width_(infer_width), infer_height_(infer_height),
          output_width_(align16(output_width)), output_height_(align16(output_height)),
          fps_(output_fps), input_timeout_ms_(input_timeout_ms),
          output_bitrate_kbps_(output_bitrate_kbps), rtsp_transport_(rtsp_transport),
          rtsp_server_(rtsp_server) {
        if (infer_width_ < 2 || infer_height_ < 2 || output_width < 2 || output_height < 2 || fps_ < 1) {
            throw std::invalid_argument("configured inference/output dimensions and frame rate must be positive");
        }
        if (!output.empty()) {
            output_.open(output, std::ios::binary);
            if (!output_) throw std::runtime_error("cannot create H.264 output: " + output);
        }
        try {
            if (input_timeout_ms_ < 1 || output_bitrate_kbps_ < 1) {
                throw std::invalid_argument("input timeout and output bitrate must be positive");
            }
            open_input(input);
            initialize_media();
        } catch (...) {
            release();
            throw;
        }
    }

    Impl(const Impl&) = delete;
    Impl& operator=(const Impl&) = delete;

    ~Impl() { release(); }

    int width() const { return width_; }
    int height() const { return height_; }
    int output_width() const { return output_width_; }
    int output_height() const { return output_height_; }
    int fps() const { return fps_; }
    int infer_width() const { return infer_width_; }
    int infer_height() const { return infer_height_; }
    int group() const { return group_; }
    int output_channel() const { return output_channel_; }

    bool next_inference_frame(hi_video_frame_info& frame) {
        while (true) {
            if (try_get_inference_frame(frame, 0)) return true;
            if (input_eof_) return drain_decoder(frame);
            AVPacket* packet = av_packet_alloc();
            if (packet == nullptr) throw std::runtime_error("av_packet_alloc failed");
            set_input_deadline();
            const int read_result = av_read_frame(format_, packet);
            if (read_result == AVERROR_EOF) {
                av_packet_free(&packet);
                flush_filter();
                send_eos();
                input_eof_ = true;
                continue;
            }
            if (read_result < 0) {
                av_packet_free(&packet);
                throw InputSourceError(ffmpeg_error("av_read_frame", read_result).what());
            }
            if (packet->stream_index == video_stream_) filter_packet(packet);
            av_packet_free(&packet);
            if (try_get_inference_frame(frame, 250)) return true;
        }
    }

    void get_output_frame(hi_video_frame_info& frame) {
        require(hi_mpi_vpss_get_chn_frame(group_, output_channel_, &frame, 1500), "hi_mpi_vpss_get_chn_frame(output)");
    }

    void release_inference_frame(const hi_video_frame_info& frame) {
        require(hi_mpi_vpss_release_chn_frame(group_, infer_channel_, &frame), "hi_mpi_vpss_release_chn_frame(inference)");
    }

    void release_output_frame(const hi_video_frame_info& frame) {
        require(hi_mpi_vpss_release_chn_frame(group_, output_channel_, &frame), "hi_mpi_vpss_release_chn_frame(output)");
    }

    void encode_frame(const hi_video_frame_info& frame) {
        require(hi_mpi_venc_send_frame(venc_channel_, &frame, 1500), "hi_mpi_venc_send_frame");
        std::array<hi_venc_pack, 16> packs{};
        hi_venc_stream stream{};
        stream.pack = packs.data();
        stream.pack_cnt = static_cast<hi_u32>(packs.size());
        require(hi_mpi_venc_get_stream(venc_channel_, &stream, 1500), "hi_mpi_venc_get_stream");
        std::vector<std::uint8_t> access_unit;
        try {
            for (hi_u32 i = 0; i < stream.pack_cnt; ++i) {
                const hi_venc_pack& pack = stream.pack[i];
                if (pack.addr == nullptr || pack.len <= pack.offset) continue;
                access_unit.insert(access_unit.end(), pack.addr + pack.offset, pack.addr + pack.len);
            }
            if (output_.is_open() && !access_unit.empty()) {
                output_.write(reinterpret_cast<const char*>(access_unit.data()),
                              static_cast<std::streamsize>(access_unit.size()));
                if (!output_) throw std::runtime_error("writing H.264 output failed");
            }
        } catch (...) {
            hi_mpi_venc_release_stream(venc_channel_, &stream);
            throw;
        }
        require(hi_mpi_venc_release_stream(venc_channel_, &stream), "hi_mpi_venc_release_stream");
        if (rtsp_server_ != nullptr && !access_unit.empty()) {
            const std::uint64_t duration = GST_SECOND / static_cast<std::uint64_t>(fps_);
            rtsp_server_->push_access_unit(access_unit.data(), access_unit.size(), duration);
            if (rtsp_server_->failed()) throw std::runtime_error("RTSP media pipeline failed to configure");
        }
        ++encoded_frames_;
    }

    size_t encoded_frames() const { return encoded_frames_; }

private:
    static void require(hi_s32 result, const char* operation) {
        if (result != HI_SUCCESS) throw std::runtime_error(std::string(operation) + " failed with media error " + std::to_string(result));
    }

    static std::runtime_error ffmpeg_error(const char* operation, int code) {
        std::array<char, AV_ERROR_MAX_STRING_SIZE> text{};
        av_strerror(code, text.data(), text.size());
        return std::runtime_error(std::string(operation) + ": " + text.data());
    }

    static int align16(int value) { return (value + 15) & ~15; }

    static int interrupt_input(void* opaque) {
        const auto* self = static_cast<const Impl*>(opaque);
        const auto deadline = self->input_deadline_ns_.load(std::memory_order_relaxed);
        if (deadline == 0) return 0;
        const auto now = std::chrono::duration_cast<std::chrono::nanoseconds>(
            std::chrono::steady_clock::now().time_since_epoch()).count();
        return now >= deadline ? 1 : 0;
    }

    void set_input_deadline() {
        const auto deadline = std::chrono::steady_clock::now() + std::chrono::milliseconds(input_timeout_ms_);
        input_deadline_ns_.store(std::chrono::duration_cast<std::chrono::nanoseconds>(
            deadline.time_since_epoch()).count(), std::memory_order_relaxed);
    }

    void open_input(const std::string& input) {
        format_ = avformat_alloc_context();
        if (format_ == nullptr) throw std::runtime_error("avformat_alloc_context failed");
        format_->interrupt_callback.callback = &Impl::interrupt_input;
        format_->interrupt_callback.opaque = this;
        AVDictionary* options = nullptr;
        const std::string timeout_us = std::to_string(static_cast<long long>(input_timeout_ms_) * 1000LL);
        av_dict_set(&options, "rw_timeout", timeout_us.c_str(), 0);
        if (input.rfind("rtsp://", 0) == 0 || input.rfind("rtsps://", 0) == 0) {
            av_dict_set(&options, "rtsp_transport", rtsp_transport_.c_str(), 0);
            av_dict_set(&options, "stimeout", timeout_us.c_str(), 0);
        } else if (input.rfind("udp://", 0) == 0) {
            av_dict_set(&options, "timeout", timeout_us.c_str(), 0);
        }
        set_input_deadline();
        int result = avformat_open_input(&format_, input.c_str(), nullptr, &options);
        av_dict_free(&options);
        if (result < 0) throw InputSourceError(ffmpeg_error("avformat_open_input", result).what());
        set_input_deadline();
        result = avformat_find_stream_info(format_, nullptr);
        if (result < 0) throw InputSourceError(ffmpeg_error("avformat_find_stream_info", result).what());
        video_stream_ = av_find_best_stream(format_, AVMEDIA_TYPE_VIDEO, -1, -1, nullptr, 0);
        if (video_stream_ < 0) throw InputSourceError(ffmpeg_error("av_find_best_stream", video_stream_).what());
        AVCodecParameters* parameters = format_->streams[video_stream_]->codecpar;
        if (parameters->codec_id != AV_CODEC_ID_H264) throw std::runtime_error("hardware VDEC currently requires H.264 input");
        width_ = parameters->width & ~1;
        height_ = parameters->height & ~1;
        if (width_ < 64 || height_ < 64) throw std::runtime_error("input video dimensions are invalid");
        AVRational rate = format_->streams[video_stream_]->avg_frame_rate;
        if (rate.num <= 0 || rate.den <= 0) rate = format_->streams[video_stream_]->r_frame_rate;
        if (rate.num > 0 && rate.den > 0) {
            const int detected_fps = static_cast<int>(av_q2d(rate) + 0.5);
            if (detected_fps >= 1 && detected_fps <= 120) fps_ = detected_fps;
        }
        const AVBitStreamFilter* bsf = av_bsf_get_by_name("h264_mp4toannexb");
        if (bsf == nullptr) throw std::runtime_error("FFmpeg h264_mp4toannexb filter is unavailable");
        result = av_bsf_alloc(bsf, &filter_);
        if (result < 0) throw ffmpeg_error("av_bsf_alloc", result);
        result = avcodec_parameters_copy(filter_->par_in, parameters);
        if (result < 0) throw ffmpeg_error("avcodec_parameters_copy", result);
        filter_->time_base_in = format_->streams[video_stream_]->time_base;
        result = av_bsf_init(filter_);
        if (result < 0) throw ffmpeg_error("av_bsf_init", result);
    }

    void initialize_media() {
        require(hi_mpi_sys_init(), "hi_mpi_sys_init");
        sys_initialized_ = true;
        hi_pic_buf_attr picture_attr{};
        picture_attr.width = static_cast<hi_u32>(width_);
        picture_attr.height = static_cast<hi_u32>(height_);
        picture_attr.align = 0;
        picture_attr.bit_width = HI_DATA_BIT_WIDTH_8;
        picture_attr.pixel_format = HI_PIXEL_FORMAT_YUV_SEMIPLANAR_420;
        picture_attr.compress_mode = HI_COMPRESS_MODE_NONE;

        hi_vdec_chn_attr decoder_attr{};
        decoder_attr.type = HI_PT_H264;
        decoder_attr.mode = HI_VDEC_SEND_MODE_FRAME;
        decoder_attr.pic_width = static_cast<hi_u32>(width_);
        decoder_attr.pic_height = static_cast<hi_u32>(height_);
        decoder_attr.stream_buf_size = static_cast<hi_u32>(static_cast<size_t>(width_) * height_ * 3 / 2);
        decoder_attr.frame_buf_size = hi_vdec_get_pic_buf_size(HI_PT_H264, &picture_attr);
        decoder_attr.frame_buf_cnt = 16;
        decoder_attr.video_attr.ref_frame_num = 12;
        decoder_attr.video_attr.temporal_mvp_en = HI_TRUE;
        decoder_attr.video_attr.tmv_buf_size = hi_vdec_get_tmv_buf_size(HI_PT_H264, width_, height_);
        require(hi_mpi_vdec_create_chn(decoder_channel_, &decoder_attr), "hi_mpi_vdec_create_chn");
        decoder_created_ = true;
        hi_vdec_chn_param decoder_param{};
        require(hi_mpi_vdec_get_chn_param(decoder_channel_, &decoder_param), "hi_mpi_vdec_get_chn_param");
        decoder_param.video_param.dec_mode = HI_VIDEO_DEC_MODE_IPB;
        decoder_param.video_param.out_order = HI_VIDEO_OUT_ORDER_DISPLAY;
        decoder_param.video_param.compress_mode = HI_COMPRESS_MODE_HFBC;
        decoder_param.video_param.video_format = HI_VIDEO_FORMAT_TILE_64x16;
        decoder_param.display_frame_num = 3;
        require(hi_mpi_vdec_set_chn_param(decoder_channel_, &decoder_param), "hi_mpi_vdec_set_chn_param");

        hi_mpp_chn decoder{};
        decoder.mod_id = HI_ID_VDEC;
        decoder.dev_id = decoder_channel_;
        decoder.chn_id = 0;
        hi_mpp_chn vpss{};
        vpss.mod_id = HI_ID_VPSS;
        vpss.dev_id = group_;
        vpss.chn_id = 0;
        require(hi_mpi_vdec_start_recv_stream(decoder_channel_), "hi_mpi_vdec_start_recv_stream");
        decoder_started_ = true;

        hi_vpss_grp_attr group_attr{};
        group_attr.max_width = static_cast<hi_u32>((width_ + 15) & ~15);
        group_attr.max_height = static_cast<hi_u32>((height_ + 15) & ~15);
        group_attr.pixel_format = HI_PIXEL_FORMAT_YUV_SEMIPLANAR_420;
        group_attr.dynamic_range = HI_DYNAMIC_RANGE_SDR8;
        group_attr.frame_rate = { -1, -1 };
        group_attr.nr_en = HI_FALSE;
        require(hi_mpi_vpss_create_grp(group_, &group_attr), "hi_mpi_vpss_create_grp");
        group_created_ = true;

        hi_vpss_chn_attr infer_attr{};
        infer_attr.chn_mode = HI_VPSS_CHN_MODE_USER;
        infer_attr.width = static_cast<hi_u32>(infer_width_);
        infer_attr.height = static_cast<hi_u32>(infer_height_);
        infer_attr.video_format = HI_VIDEO_FORMAT_LINEAR;
        infer_attr.pixel_format = HI_PIXEL_FORMAT_YVU_SEMIPLANAR_420;
        infer_attr.dynamic_range = HI_DYNAMIC_RANGE_SDR8;
        infer_attr.compress_mode = HI_COMPRESS_MODE_NONE;
        infer_attr.frame_rate = { -1, -1 };
        infer_attr.depth = 2;
        infer_attr.aspect_ratio.mode = HI_ASPECT_RATIO_AUTO;
        infer_attr.aspect_ratio.bg_color = 0x727272;
        require(hi_mpi_vpss_set_chn_attr(group_, infer_channel_, &infer_attr), "hi_mpi_vpss_set_chn_attr(inference)");
        require(hi_mpi_vpss_enable_chn(group_, infer_channel_), "hi_mpi_vpss_enable_chn(inference)");

        hi_vpss_chn_attr output_attr = infer_attr;
        output_attr.width = static_cast<hi_u32>(output_width_);
        output_attr.height = static_cast<hi_u32>(output_height_);
        output_attr.depth = 2;
        output_attr.aspect_ratio.mode = HI_ASPECT_RATIO_NONE;
        require(hi_mpi_vpss_set_chn_attr(group_, output_channel_, &output_attr), "hi_mpi_vpss_set_chn_attr(output)");
        require(hi_mpi_vpss_enable_chn(group_, output_channel_), "hi_mpi_vpss_enable_chn(output)");
        require(hi_mpi_vpss_start_grp(group_), "hi_mpi_vpss_start_grp");
        group_started_ = true;

        require(hi_mpi_sys_bind(&decoder, &vpss), "hi_mpi_sys_bind(VDEC,VPSS)");
        bound_ = true;

        hi_venc_chn_attr encoder_attr{};
        encoder_attr.venc_attr.type = HI_PT_H264;
        encoder_attr.venc_attr.max_pic_width = static_cast<hi_u32>(output_width_);
        encoder_attr.venc_attr.max_pic_height = static_cast<hi_u32>(output_height_);
        encoder_attr.venc_attr.pic_width = static_cast<hi_u32>(output_width_);
        encoder_attr.venc_attr.pic_height = static_cast<hi_u32>(output_height_);
        encoder_attr.venc_attr.buf_size = static_cast<hi_u32>(static_cast<size_t>(output_width_) * output_height_);
        encoder_attr.venc_attr.profile = 0;
        encoder_attr.venc_attr.is_by_frame = HI_TRUE;
        encoder_attr.rc_attr.rc_mode = HI_VENC_RC_MODE_H264_CBR;
        encoder_attr.rc_attr.h264_cbr.gop = static_cast<hi_u32>(fps_);
        encoder_attr.rc_attr.h264_cbr.stats_time = 1;
        encoder_attr.rc_attr.h264_cbr.src_frame_rate = static_cast<hi_u32>(fps_);
        encoder_attr.rc_attr.h264_cbr.dst_frame_rate = static_cast<hi_u32>(fps_);
        encoder_attr.rc_attr.h264_cbr.bit_rate = static_cast<hi_u32>(output_bitrate_kbps_);
        encoder_attr.gop_attr.gop_mode = HI_VENC_GOP_MODE_NORMAL_P;
        encoder_attr.gop_attr.normal_p.ip_qp_delta = 0;
        require(hi_mpi_venc_create_chn(venc_channel_, &encoder_attr), "hi_mpi_venc_create_chn");
        encoder_created_ = true;
        hi_venc_start_param start{};
        start.recv_pic_num = -1;
        require(hi_mpi_venc_start_chn(venc_channel_, &start), "hi_mpi_venc_start_chn");
        encoder_started_ = true;
    }

    void filter_packet(AVPacket* packet) {
        const int result = av_bsf_send_packet(filter_, packet);
        if (result < 0) throw ffmpeg_error("av_bsf_send_packet", result);
        receive_filtered_packets();
    }

    void flush_filter() {
        if (filter_flushed_) return;
        const int result = av_bsf_send_packet(filter_, nullptr);
        if (result < 0 && result != AVERROR_EOF) throw ffmpeg_error("av_bsf_send_packet(flush)", result);
        receive_filtered_packets();
        filter_flushed_ = true;
    }

    void receive_filtered_packets() {
        AVPacket* packet = av_packet_alloc();
        if (packet == nullptr) throw std::runtime_error("av_packet_alloc failed");
        while (true) {
            const int result = av_bsf_receive_packet(filter_, packet);
            if (result == AVERROR(EAGAIN) || result == AVERROR_EOF) break;
            if (result < 0) {
                av_packet_free(&packet);
                throw ffmpeg_error("av_bsf_receive_packet", result);
            }
            hi_vdec_stream stream{};
            stream.addr = packet->data;
            stream.len = static_cast<hi_u32>(packet->size);
            stream.pts = packet->pts == AV_NOPTS_VALUE ? 0 : static_cast<hi_u64>(packet->pts);
            stream.end_of_frame = HI_TRUE;
            require(hi_mpi_vdec_send_stream(decoder_channel_, &stream, nullptr, 1000), "hi_mpi_vdec_send_stream");
            av_packet_unref(packet);
        }
        av_packet_free(&packet);
    }

    void send_eos() {
        hi_vdec_stream stream{};
        stream.end_of_stream = HI_TRUE;
        require(hi_mpi_vdec_send_stream(decoder_channel_, &stream, nullptr, 1000), "hi_mpi_vdec_send_stream(EOS)");
    }

    bool try_get_inference_frame(hi_video_frame_info& frame, hi_s32 timeout_ms) {
        const hi_s32 result = hi_mpi_vpss_get_chn_frame(group_, infer_channel_, &frame, timeout_ms);
        if (result == HI_SUCCESS) return true;
        if (result == HI_ERR_VPSS_BUF_EMPTY || result == HI_ERR_VPSS_NOBUF || result == HI_ERR_VPSS_BUSY) return false;
        throw std::runtime_error("hi_mpi_vpss_get_chn_frame(inference) failed with media error " + std::to_string(result));
    }

    bool drain_decoder(hi_video_frame_info& frame) {
        if (!eos_sent_) {
            send_eos();
            eos_sent_ = true;
        }
        if (try_get_inference_frame(frame, 1000)) return true;
        return false;
    }

    void release() noexcept {
        if (encoder_started_) hi_mpi_venc_stop_chn(venc_channel_);
        if (encoder_created_) hi_mpi_venc_destroy_chn(venc_channel_);
        if (decoder_started_) hi_mpi_vdec_stop_recv_stream(decoder_channel_);
        if (bound_) {
            hi_mpp_chn decoder{};
            decoder.mod_id = HI_ID_VDEC;
            decoder.dev_id = decoder_channel_;
            decoder.chn_id = 0;
            hi_mpp_chn vpss{};
            vpss.mod_id = HI_ID_VPSS;
            vpss.dev_id = group_;
            vpss.chn_id = 0;
            hi_mpi_sys_unbind(&decoder, &vpss);
        }
        if (decoder_created_) hi_mpi_vdec_destroy_chn(decoder_channel_);
        if (group_started_) hi_mpi_vpss_stop_grp(group_);
        if (group_created_) {
            hi_mpi_vpss_disable_chn(group_, output_channel_);
            hi_mpi_vpss_disable_chn(group_, infer_channel_);
            hi_mpi_vpss_destroy_grp(group_);
        }
        if (sys_initialized_) hi_mpi_sys_exit();
        if (filter_) av_bsf_free(&filter_);
        if (format_) avformat_close_input(&format_);
    }

    static constexpr hi_vdec_chn decoder_channel_ = 0;
    static constexpr hi_vpss_grp group_ = 0;
    static constexpr hi_vpss_chn infer_channel_ = 0;
    static constexpr hi_vpss_chn output_channel_ = 1;
    static constexpr hi_venc_chn venc_channel_ = 0;
    AVFormatContext* format_ = nullptr;
    AVBSFContext* filter_ = nullptr;
    int video_stream_ = -1;
    int width_ = 0;
    int height_ = 0;
    int infer_width_ = 0;
    int infer_height_ = 0;
    int output_width_ = 0;
    int output_height_ = 0;
    int fps_ = 25;
    int input_timeout_ms_ = 5000;
    int output_bitrate_kbps_ = 1500;
    std::string rtsp_transport_ = "tcp";
    std::atomic<std::int64_t> input_deadline_ns_{0};
    std::ofstream output_;
    yunshao::RtspServer* rtsp_server_ = nullptr;
    size_t encoded_frames_ = 0;
    bool sys_initialized_ = false;
    bool group_created_ = false;
    bool group_started_ = false;
    bool decoder_created_ = false;
    bool decoder_started_ = false;
    bool bound_ = false;
    bool encoder_created_ = false;
    bool encoder_started_ = false;
    bool input_eof_ = false;
    bool filter_flushed_ = false;
    bool eos_sent_ = false;
};

MediaPipeline::MediaPipeline(const std::string& input, const std::string& output, int output_fps,
                             int infer_width, int infer_height, int output_width, int output_height,
                             int input_timeout_ms, int output_bitrate_kbps, const std::string& rtsp_transport,
                             yunshao::RtspServer* rtsp_server)
    : impl_(std::make_unique<Impl>(input, output, output_fps, infer_width, infer_height,
                                   output_width, output_height, input_timeout_ms, output_bitrate_kbps,
                                   rtsp_transport, rtsp_server)) {}

MediaPipeline::~MediaPipeline() = default;

int MediaPipeline::width() const { return impl_->width(); }
int MediaPipeline::height() const { return impl_->height(); }
int MediaPipeline::output_width() const { return impl_->output_width(); }
int MediaPipeline::output_height() const { return impl_->output_height(); }
int MediaPipeline::fps() const { return impl_->fps(); }
int MediaPipeline::infer_width() const { return impl_->infer_width(); }
int MediaPipeline::infer_height() const { return impl_->infer_height(); }
int MediaPipeline::group() const { return impl_->group(); }
int MediaPipeline::output_channel() const { return impl_->output_channel(); }
bool MediaPipeline::next_inference_frame(hi_video_frame_info& frame) { return impl_->next_inference_frame(frame); }
void MediaPipeline::get_output_frame(hi_video_frame_info& frame) { impl_->get_output_frame(frame); }
void MediaPipeline::release_inference_frame(const hi_video_frame_info& frame) { impl_->release_inference_frame(frame); }
void MediaPipeline::release_output_frame(const hi_video_frame_info& frame) { impl_->release_output_frame(frame); }
void MediaPipeline::encode_frame(const hi_video_frame_info& frame) { impl_->encode_frame(frame); }
std::size_t MediaPipeline::encoded_frames() const { return impl_->encoded_frames(); }
