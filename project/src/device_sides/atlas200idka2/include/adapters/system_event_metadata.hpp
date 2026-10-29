#pragma once

#include "application/ports/event_metadata.hpp"

namespace yunshao::adapters {

class SystemEventMetadata final : public application::ports::EventMetadata {
public:
    std::string new_event_id() override;
    std::string utc_timestamp() override;
};

}  // namespace yunshao::adapters
