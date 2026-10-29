#pragma once

#include "application/ports/evidence_frame_encoder.hpp"

namespace yunshao::adapters {

class AtlasEvidenceFrameEncoder final : public application::ports::EvidenceFrameEncoder {
public:
    std::vector<unsigned char> encode_jpeg(
        const application::ports::EvidenceFrameView& frame,
        const std::vector<Detection>& detections) override;
};

}  // namespace yunshao::adapters
