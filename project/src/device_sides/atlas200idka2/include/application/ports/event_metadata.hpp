#pragma once

#include <string>

namespace microvis::application::ports {

class EventMetadata {
public:
    virtual ~EventMetadata() = default;
    virtual std::string new_event_id() = 0;
    virtual std::string utc_timestamp() = 0;
};

}  // namespace microvis::application::ports
