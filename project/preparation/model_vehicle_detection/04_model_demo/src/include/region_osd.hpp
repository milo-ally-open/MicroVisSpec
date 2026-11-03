#pragma once

#include <acl/dvpp/hi_region.h>
#include <acl/media/hi_mpi_vpss.h>
#include <opencv2/core.hpp>
#include <opencv2/imgproc.hpp>

#include <algorithm>
#include <stdexcept>
#include <string>
#include <vector>

class RegionOsd {
public:
    RegionOsd(int group, int channel, int width, int height)
        : group_(group), channel_(channel), width_(width), height_(height) {
        if (width_ < 2 || height_ < 2) throw std::runtime_error("invalid VPSS Region canvas size");
        hi_rgn_attr region{};
        region.type = HI_RGN_OVERLAYEX;
        region.attr.overlayex.pixel_format = HI_PIXEL_FORMAT_ARGB_8888;
        region.attr.overlayex.bg_color = 0;
        region.attr.overlayex.size.width = static_cast<hi_u32>(width_);
        region.attr.overlayex.size.height = static_cast<hi_u32>(height_);
        region.attr.overlayex.canvas_num = 1;
        require(hi_mpi_rgn_create(handle_, &region), "hi_mpi_rgn_create");
        created_ = true;
        channel_desc_.mod_id = HI_ID_VPSS;
        channel_desc_.dev_id = group_;
        channel_desc_.chn_id = channel_;
        hi_rgn_chn_attr display{};
        display.is_show = HI_TRUE;
        display.type = HI_RGN_OVERLAYEX;
        display.attr.overlayex_chn.point = {0, 0};
        display.attr.overlayex_chn.fg_alpha = 255;
        display.attr.overlayex_chn.bg_alpha = 0;
        display.attr.overlayex_chn.layer = 0;
        require(hi_mpi_rgn_attach_to_chn(handle_, &channel_desc_, &display), "hi_mpi_rgn_attach_to_chn");
        attached_ = true;
    }

    RegionOsd(const RegionOsd&) = delete;
    RegionOsd& operator=(const RegionOsd&) = delete;

    ~RegionOsd() {
        if (attached_) hi_mpi_rgn_detach_from_chn(handle_, &channel_desc_);
        if (created_) hi_mpi_rgn_destroy(handle_);
    }

    template <typename DetectionT>
    void update(const std::vector<DetectionT>& detections, const std::vector<std::string>& labels) {
        require(hi_mpi_rgn_get_canvas_info(handle_, &canvas_), "hi_mpi_rgn_get_canvas_info");
        if (canvas_.virt_addr == nullptr || canvas_.stride < static_cast<hi_u32>(width_ * 4)) {
            throw std::runtime_error("invalid Region canvas address or stride");
        }
        cv::Mat image(height_, width_, CV_8UC4, canvas_.virt_addr, canvas_.stride);
        image.setTo(cv::Scalar(0, 0, 0, 0));
        for (const auto& detection : detections) {
            const int left = std::clamp(detection.box.x, 0, width_ - 1);
            const int top = std::clamp(detection.box.y, 0, height_ - 1);
            const int right = std::clamp(detection.box.x + detection.box.width, left + 1, width_);
            const int bottom = std::clamp(detection.box.y + detection.box.height, top + 1, height_);
            cv::rectangle(image, cv::Rect(left, top, right - left, bottom - top), cv::Scalar(0, 220, 120, 255), 2, cv::LINE_8);
            const std::string label = detection.class_id >= 0 && detection.class_id < static_cast<int>(labels.size())
                ? labels[static_cast<size_t>(detection.class_id)] + " " + cv::format("%.2f", detection.confidence)
                : std::string("object ") + cv::format("%.2f", detection.confidence);
            int baseline = 0;
            const cv::Size text = cv::getTextSize(label, cv::FONT_HERSHEY_SIMPLEX, 0.55, 1, &baseline);
            const int text_y = std::max(text.height + 4, top);
            cv::rectangle(image, cv::Rect(left, std::max(0, text_y - text.height - 6),
                          std::min(text.width + 10, width_ - left), std::min(text.height + 8, height_)),
                          cv::Scalar(0, 0, 0, 210), cv::FILLED);
            cv::putText(image, label, cv::Point(left + 5, text_y), cv::FONT_HERSHEY_SIMPLEX, 0.55,
                        cv::Scalar(0, 220, 120, 255), 1, cv::LINE_8);
        }
        require(hi_mpi_rgn_update_canvas(handle_), "hi_mpi_rgn_update_canvas");
    }

private:
    static void require(hi_s32 result, const char* operation) {
        if (result != HI_SUCCESS) throw std::runtime_error(std::string(operation) + " failed with media error " + std::to_string(result));
    }

    static constexpr hi_rgn_handle handle_ = 0;
    int group_;
    int channel_;
    int width_;
    int height_;
    hi_mpp_chn channel_desc_{};
    hi_rgn_canvas_info canvas_{};
    bool created_ = false;
    bool attached_ = false;
};
