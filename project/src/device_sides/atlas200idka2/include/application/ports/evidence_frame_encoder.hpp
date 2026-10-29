#pragma once

#include "ai_interface.hpp"

#include <cstddef>
#include <vector>

namespace yunshao::application::ports {

struct EvidenceFrameView {
    const void* luma_device = nullptr;
    const void* chroma_device = nullptr;
    int width = 0;
    int height = 0;
    std::size_t luma_pitch = 0;
    std::size_t chroma_pitch = 0;
    std::size_t luma_aligned_height = 0;
};

class EvidenceFrameEncoder {
public:
    virtual ~EvidenceFrameEncoder() = default;
    virtual std::vector<unsigned char> encode_jpeg(
        const EvidenceFrameView& frame,
        const std::vector<Detection>& detections) = 0;
};

}  // namespace yunshao::application::ports
