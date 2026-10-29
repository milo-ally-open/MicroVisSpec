#pragma once

#include <string>
#include <vector>

namespace yunshao::application::ports {

// Best-effort persistence port for local alarm evidence before agent upload.
class AlarmEvidenceOutbox {
public:
    virtual ~AlarmEvidenceOutbox() = default;
    virtual void persist_best_effort(const std::string& event_id,
                                     const std::vector<unsigned char>& jpeg) = 0;
};

}  // namespace yunshao::application::ports
