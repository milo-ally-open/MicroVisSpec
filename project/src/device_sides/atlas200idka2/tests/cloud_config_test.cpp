#include "common/cloud_config.hpp"
#include "config_fixture.hpp"
#include "media/h264_parameter_sets.hpp"
#include <cassert>
#include <chrono>
#include <iostream>

int main() {
    using namespace yunshao;
    H264ParameterSets headers;
    const std::vector<std::uint8_t> initial{0,0,0,1,0x67,0x42,0,0,1,0x68,0x11};
    headers.observe(initial.data(), initial.size());
    const auto retained = headers.prefix();
    assert((retained == std::vector<std::uint8_t>{0,0,0,1,0x67,0x42,0,0,0,1,0x68,0x11}));
    const std::vector<std::uint8_t> later_idr{0,0,1,0x65,0x22};
    headers.observe(later_idr.data(), later_idr.size());
    assert(headers.prefix() == retained); // A late RTSP reader receives SPS/PPS before this IDR.
    const auto root = std::filesystem::temp_directory_path() / ("yunshao-cloud-test-" +
        std::to_string(std::chrono::steady_clock::now().time_since_epoch().count()));
    write_json_atomic(root / "models.json", test_fixture::models());
    write_json_atomic(root / "streams_config.json", test_fixture::streams());
    auto rules = test_fixture::alarm_rules(false);
    for (const auto* name : {"reading", "writing"}) {
        auto rule = rules["alarm_rules"][0];
        rule["class_name"] = name;
        rule["event_type"] = std::string("classroom.behavior.") + name;
        rules["alarm_rules"].push_back(rule);
    }
    write_json_atomic(root / "alarm_rules.json", rules);
    Json command{{"schema_version", 1}, {"device_id", "test-device"},
        {"command_id", "123e4567-e89b-12d3-a456-426614174001"}, {"sent_at", "2026-10-22T00:00:00Z"},
        {"algorithm_key", "classroom_behavior_detection"}, {"algorithm_version", "1.0.0"},
        {"expected_version", 1}, {"parameters", Json{{"classroom_rules", Json{
            {"hand-raising", Json{{"enabled", true}, {"confidence_threshold", 0.7},
                {"minimum_duration_ms", 1500}, {"cooldown_ms", 30000}, {"severity", "medium"}}},
            {"reading", Json{{"enabled", false}}}, {"writing", Json{{"enabled", false}}}}}}}};
    auto wrong = command; wrong["device_id"] = "other-device";
    bool rejected = false;
    try { apply_cloud_rules(root, "test-device", "model_classroom_behavior_detection", "1.0.0", wrong); }
    catch (const std::exception&) { rejected = true; }
    assert(rejected && read_json_file(root / "alarm_rules.json") == rules);
    apply_cloud_rules(root, "test-device", "model_classroom_behavior_detection", "1.0.0", command);
    const auto config = EdgeAppConfig::load(root);
    assert(config.alarm_config_version == 1);
    assert(config.find_rule("model_classroom_behavior_detection", "hand-raising")->enabled);
    assert(config.find_rule("model_classroom_behavior_detection", "hand-raising")->min_duration_ms == 1500);
    assert(!config.find_rule("model_classroom_behavior_detection", "reading")->enabled);
    auto retry = command; retry["sent_at"] = "2026-10-22T00:00:05Z";
    apply_cloud_rules(root, "test-device", "model_classroom_behavior_detection", "1.0.0", retry);
    const auto persisted = read_json_file(root / "alarm_rules.json");
    auto invalid = command; invalid["expected_version"] = 2;
    invalid["parameters"]["classroom_rules"]["hand-raising"]["confidence_threshold"] = 1.5;
    rejected = false;
    try { apply_cloud_rules(root, "test-device", "model_classroom_behavior_detection", "1.0.0", invalid); }
    catch (const std::exception&) { rejected = true; }
    assert(rejected && read_json_file(root / "alarm_rules.json") == persisted);
    Json internal{{"attributes", Json{{"model_id", "model_classroom_behavior_detection"},
        {"behavior_class", "hand-raising"}}}};
    const auto wire = cloud_event_payload(internal);
    assert(!wire["attributes"].contains("model_id") && internal["attributes"].contains("model_id"));
    assert(wire["attributes"]["behavior_class"] == "hand-raising");
    std::filesystem::remove_all(root);
    std::cout << "Cloud command validation, retry, atomicity and wire contract passed\n";
}
