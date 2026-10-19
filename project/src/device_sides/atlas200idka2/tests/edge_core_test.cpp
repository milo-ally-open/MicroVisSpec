#include "common/core.hpp"
#include "config_fixture.hpp"

#include <cassert>
#include <chrono>
#include <filesystem>
#include <iostream>

using yunshao::Json;
using yunshao::EventOutbox;
using yunshao::ModelManifest;

int main() {
    ModelManifest manifest{"model_classroom_behavior_detection", "1.0.0", "om", "model.om", std::string(64, 'a'),
                           "Ascend310B4", 640, 640, {"hand-raising", "reading", "writing"}};
    ModelManifest::validate(manifest);
    const auto config = test_fixture::config(true);
    ModelManifest::validate_against_profile(manifest, config.require_model("model_classroom_behavior_detection"));
    auto wrong_chip = manifest;
    wrong_chip.soc_version = "Ascend310P";
    bool rejected = false;
    ModelManifest::validate(wrong_chip);  // Generic manifest validation does not hardcode one chip.
    try { ModelManifest::validate_against_profile(wrong_chip, config.require_model("model_classroom_behavior_detection")); }
    catch (const std::invalid_argument&) { rejected = true; }
    assert(rejected);
    auto wrong_order = manifest;
    wrong_order.classes = {"reading", "hand-raising", "writing"};
    ModelManifest::validate(wrong_order);
    rejected = false;
    try { ModelManifest::validate_against_profile(wrong_order, config.require_model("model_classroom_behavior_detection")); }
    catch (const std::invalid_argument&) { rejected = true; }
    assert(rejected);
    auto non_classroom = manifest;
    non_classroom.model_id = "model_room_occupancy_detection";
    non_classroom.soc_version = config.require_model("model_room_occupancy_detection").soc_version;
    non_classroom.input_width = config.require_model("model_room_occupancy_detection").input_width;
    non_classroom.input_height = config.require_model("model_room_occupancy_detection").input_height;
    non_classroom.classes = config.require_model("model_room_occupancy_detection").classes;
    ModelManifest::validate_against_profile(non_classroom, config.require_model("model_room_occupancy_detection"));

    const auto root = std::filesystem::temp_directory_path() /
        ("yunshao-agent-test-" + std::to_string(std::chrono::steady_clock::now().time_since_epoch().count()));
    std::filesystem::create_directories(root);
    const auto db_path = root / "agent.db";
    EventOutbox outbox(db_path, "device-test", config);
    Json event{{"schema_version", 1}, {"device_id", "device-test"},
               {"event_id", "123e4567-e89b-12d3-a456-426614174000"},
               {"camera_id", "123e4567-e89b-12d3-a456-426614174001"},
               {"event_type", "classroom.behavior.hand-raising"}, {"severity", "medium"},
               {"occurred_at", "2026-10-19T00:00:00Z"}, {"sent_at", "2026-10-19T00:00:01Z"},
               {"algorithm_key", "classroom_behavior_detection"}, {"algorithm_version", "1.0.0"},
               {"attributes", Json{{"behavior_class", "hand-raising"}, {"confidence", 0.91},
                                    {"rule_config_version", config.alarm_config_version}, {"observed_duration_ms", 1200}}}};
    std::string error;
    assert(outbox.enqueue(event, &error));
    assert(outbox.enqueue(event, &error));
    assert(outbox.due_events().size() == 1);
    Json wrong_device_ack{{"schema_version", 1}, {"device_id", "other-device"},
                          {"event_id", event["event_id"]}, {"status", "accepted"}};
    assert(!outbox.apply_ack(wrong_device_ack, &error));
    Json accepted_ack{{"schema_version", 1}, {"device_id", "device-test"},
                      {"event_id", event["event_id"]}, {"status", "accepted"}};
    assert(outbox.apply_ack(accepted_ack, &error));
    assert(outbox.counts()["acked"] == 1);

    event["event_id"] = "123e4567-e89b-12d3-a456-426614174002";
    assert(outbox.enqueue(event, &error));
    Json rejected_ack{{"schema_version", 1}, {"device_id", "device-test"},
                      {"event_id", event["event_id"]}, {"status", "rejected"}, {"reason_code", "rule_disabled"}};
    assert(outbox.apply_ack(rejected_ack, &error));
    assert(outbox.counts()["quarantined"] == 1);

    event["event_id"] = "123e4567-e89b-12d3-a456-426614174003";
    event["account_id"] = "must-not-be-trusted";
    assert(!outbox.enqueue(event, &error));
    std::filesystem::remove_all(root);
    std::cout << "edge core integration checks passed\n";
}
