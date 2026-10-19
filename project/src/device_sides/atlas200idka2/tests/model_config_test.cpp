#include "config_fixture.hpp"

#include <cassert>
#include <iostream>
#include <stdexcept>

using yunshao::EdgeAppConfig;
using yunshao::Json;

int main() {
    const EdgeAppConfig config = test_fixture::config();
    assert(config.models.size() == 2);
    assert(config.streams.size() == 2);
    assert(config.max_loaded_models == 1);
    assert(config.outbox_max_bytes == 1048576);
    assert(config.require_model("model_classroom_behavior_detection").classes.front() == "hand-raising");
    assert(config.require_model("model_room_occupancy_detection").input_width == 416);
    assert(config.require_stream("room-b").model_id == "model_room_occupancy_detection");
    assert(config.find_rule("model_classroom_behavior_detection", "hand-raising") != nullptr);
    assert(!config.find_rule("model_classroom_behavior_detection", "hand-raising")->enabled);

    bool rejected = false;
    auto invalid_models = test_fixture::models();
    invalid_models["models"][0]["model_id"] = "classroom_behavior";
    try { (void)EdgeAppConfig::parse(invalid_models, test_fixture::streams(), test_fixture::alarm_rules(false)); }
    catch (const std::invalid_argument&) { rejected = true; }
    assert(rejected);

    rejected = false;
    invalid_models = test_fixture::models();
    invalid_models["models"][0]["plugin_library"] = "libmodel_other_detection.so";
    try { (void)EdgeAppConfig::parse(invalid_models, test_fixture::streams(), test_fixture::alarm_rules(false)); }
    catch (const std::invalid_argument&) { rejected = true; }
    assert(rejected);

    rejected = false;
    invalid_models = test_fixture::models();
    invalid_models["models"].push_back(invalid_models["models"][0]);
    try { (void)EdgeAppConfig::parse(invalid_models, test_fixture::streams(), test_fixture::alarm_rules(false)); }
    catch (const std::invalid_argument&) { rejected = true; }
    assert(rejected);

    rejected = false;
    invalid_models = test_fixture::models();
    invalid_models["models"][0]["backend"] = "ax_engine";
    try { (void)EdgeAppConfig::parse(invalid_models, test_fixture::streams(), test_fixture::alarm_rules(false)); }
    catch (const std::invalid_argument&) { rejected = true; }
    assert(rejected);

    rejected = false;
    invalid_models = test_fixture::models();
    invalid_models["models"][0]["input"]["width"] = 1024;
    try { (void)EdgeAppConfig::parse(invalid_models, test_fixture::streams(), test_fixture::alarm_rules(false)); }
    catch (const std::invalid_argument&) { rejected = true; }
    assert(rejected);

    rejected = false;
    auto invalid_streams = test_fixture::streams();
    invalid_streams["streams"][0]["model_id"] = "missing_model";
    try { (void)EdgeAppConfig::parse(test_fixture::models(), invalid_streams, test_fixture::alarm_rules(false)); }
    catch (const std::invalid_argument&) { rejected = true; }
    assert(rejected);

    rejected = false;
    auto invalid_rules = test_fixture::alarm_rules(false);
    invalid_rules["alarm_rules"][0]["enabled"] = true;
    invalid_rules["alarm_rules"][0]["min_duration_ms"] = 0;
    try { (void)EdgeAppConfig::parse(test_fixture::models(), test_fixture::streams(), invalid_rules); }
    catch (const std::invalid_argument&) { rejected = true; }
    assert(rejected);

    std::cout << "config-driven model, stream, and alarm checks passed\n";
}
