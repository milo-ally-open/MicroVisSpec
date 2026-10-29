#pragma once

#include "application/ports/alarm_event_sink.hpp"
#include "application/ports/alarm_evidence_outbox.hpp"
#include "application/ports/event_metadata.hpp"

#include <nlohmann/json.hpp>

#include <string>
#include <vector>

namespace yunshao::application {

struct AlarmEventData {
    std::string device_id;
    std::string camera_id;
    std::string event_type;
    std::string severity;
    std::string algorithm_key;
    std::string algorithm_version;
    std::string model_id;
    std::string class_attribute;
    std::string class_name;
    double confidence = 0.0;
    int rule_config_version = 0;
    int observed_duration_ms = 0;
};

class AlarmEventPublisher {
public:
    AlarmEventPublisher(ports::AlarmEventSink& event_outbox,
                        ports::AlarmEvidenceOutbox& evidence_outbox,
                        ports::EventMetadata& event_metadata);

    nlohmann::json publish(const AlarmEventData& event,
                           const std::vector<unsigned char>& jpeg);

private:
    ports::AlarmEventSink& event_outbox_;
    ports::AlarmEvidenceOutbox& evidence_outbox_;
    ports::EventMetadata& event_metadata_;
};

}  // namespace yunshao::application
