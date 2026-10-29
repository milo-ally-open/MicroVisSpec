#include "adapters/atlas_evidence_frame_encoder.hpp"

#include <acl/acl.h>
#include <opencv2/imgcodecs.hpp>
#include <opencv2/imgproc.hpp>

#include <algorithm>
#include <limits>
#include <stdexcept>

namespace yunshao::adapters {

std::vector<unsigned char> AtlasEvidenceFrameEncoder::encode_jpeg(
    const application::ports::EvidenceFrameView& frame,
    const std::vector<Detection>& detections) {
    if (!frame.luma_device || frame.width <= 0 || frame.height <= 0 ||
        frame.width % 2 != 0 || frame.height % 2 != 0 ||
        frame.luma_pitch < static_cast<std::size_t>(frame.width) ||
        frame.chroma_pitch < static_cast<std::size_t>(frame.width) ||
        frame.luma_aligned_height < static_cast<std::size_t>(frame.height)) {
        throw std::invalid_argument("invalid Atlas NV21 evidence frame layout");
    }

    const auto height = static_cast<std::size_t>(frame.height);
    if (frame.luma_pitch > std::numeric_limits<std::size_t>::max() / height ||
        frame.luma_pitch > std::numeric_limits<std::size_t>::max() / frame.luma_aligned_height ||
        frame.chroma_pitch > std::numeric_limits<std::size_t>::max() / (height / 2)) {
        throw std::overflow_error("Atlas evidence frame size is out of range");
    }
    const auto luma_size = frame.luma_pitch * height;
    const auto chroma_size = frame.chroma_pitch * (height / 2);
    const auto* chroma_device = frame.chroma_device
        ? static_cast<const unsigned char*>(frame.chroma_device)
        : static_cast<const unsigned char*>(frame.luma_device) +
              frame.luma_pitch * frame.luma_aligned_height;

    std::vector<unsigned char> luma(luma_size);
    std::vector<unsigned char> chroma(chroma_size);
    if (aclrtMemcpy(luma.data(), luma.size(), frame.luma_device, luma.size(), ACL_MEMCPY_DEVICE_TO_HOST) != ACL_SUCCESS ||
        aclrtMemcpy(chroma.data(), chroma.size(), chroma_device, chroma.size(), ACL_MEMCPY_DEVICE_TO_HOST) != ACL_SUCCESS) {
        throw std::runtime_error("cannot copy evidence frame from VPSS");
    }

    cv::Mat nv21(frame.height * 3 / 2, frame.width, CV_8UC1);
    for (int row = 0; row < frame.height; ++row) {
        std::copy_n(luma.data() + static_cast<std::size_t>(row) * frame.luma_pitch,
                    frame.width, nv21.ptr(row));
    }
    for (int row = 0; row < frame.height / 2; ++row) {
        std::copy_n(chroma.data() + static_cast<std::size_t>(row) * frame.chroma_pitch,
                    frame.width, nv21.ptr(frame.height + row));
    }

    cv::Mat image;
    cv::cvtColor(nv21, image, cv::COLOR_YUV2BGR_NV21);
    for (const auto& detection : detections) {
        const cv::Rect box = cv::Rect(detection.x, detection.y,
                                      detection.width, detection.height) &
                             cv::Rect(0, 0, frame.width, frame.height);
        if (box.empty()) continue;
        cv::rectangle(image, box, cv::Scalar(32, 210, 64), 2);
        cv::putText(image, detection.label, cv::Point(box.x, std::max(18, box.y - 4)),
                    cv::FONT_HERSHEY_SIMPLEX, 0.5, cv::Scalar(32, 210, 64), 1);
    }

    std::vector<unsigned char> jpeg;
    if (!cv::imencode(".jpg", image, jpeg, {cv::IMWRITE_JPEG_QUALITY, 85})) {
        throw std::runtime_error("cannot encode evidence JPEG");
    }
    return jpeg;
}

}  // namespace yunshao::adapters
