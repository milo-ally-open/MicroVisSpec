#include "application/alarm_event_publisher.hpp"

#include <stdexcept>

namespace yunshao::application {

AlarmEventPublisher::AlarmEventPublisher(ports::AlarmEventSink& event_outbox,
                                         ports::AlarmEvidenceOutbox& evidence_outbox,
                                         ports::EventMetadata& event_metadata)
    : event_outbox_(event_outbox), evidence_outbox_(evidence_outbox),
      event_metadata_(event_metadata) {}

nlohmann::json AlarmEventPublisher::publish(const AlarmEventData& data,
                                            const std::vector<unsigned char>& jpeg) {
    if (data.device_id.empty()) {
        throw std::runtime_error("YUNSHAO_DEVICE_ID is required before enabling any alarm rule");
    }

    const std::string event_id = event_metadata_.new_event_id();
    evidence_outbox_.persist_best_effort(event_id, jpeg);

    nlohmann::json event{{"schema_version", 1}, {"device_id", data.device_id}, {"event_id", event_id},
                         {"camera_id", data.camera_id}, {"event_type", data.event_type},
                         {"severity", data.severity}, {"occurred_at", event_metadata_.utc_timestamp()},
                         {"sent_at", event_metadata_.utc_timestamp()},
                         {"algorithm_key", data.algorithm_key}, {"algorithm_version", data.algorithm_version},
                         {"attributes", nlohmann::json{{"model_id", data.model_id},
                                                      {data.class_attribute, data.class_name},
                                                      {"confidence", data.confidence},
                                                      {"rule_config_version", data.rule_config_version},
                                                      {"observed_duration_ms", data.observed_duration_ms}}}};
    return event_outbox_.enqueue(event);
}

}  // namespace yunshao::application
