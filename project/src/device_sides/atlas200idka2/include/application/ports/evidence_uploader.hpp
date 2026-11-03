#pragma once

#include <string>

namespace microvis::application::ports {

// Upload locally persisted evidence after its event is acknowledged.
class EvidenceUploader {
public:
    virtual ~EvidenceUploader() = default;
    virtual void upload_acknowledged(const std::string& device_id) = 0;
};

}  // namespace microvis::application::ports
