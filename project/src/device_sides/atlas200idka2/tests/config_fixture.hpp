#pragma once

#include "common/config.hpp"

namespace test_fixture {

using yunshao::EdgeAppConfig;
using yunshao::Json;

inline Json model_profile(const std::string& id, const std::string& algorithm_key,
                          Json classes, int width, int height) {
    return Json{{"model_id", id}, {"algorithm_key", algorithm_key}, {"format", "om"},
                {"model_file", "model.om"}, {"soc_version", "Ascend310B4"},
                {"model_family", "yolo11"}, {"backend", "ascendcl"}, {"decoder", "yolo_v11"},
                {"plugin_library", "lib" + id + ".so"}, {"enabled", true},
                {"input", Json{{"width", width}, {"height", height}, {"pixel_format", "nv21"},
                                {"preprocessing", "static_aipp"}}},
                {"classes", std::move(classes)}, {"confidence_threshold", 0.25},
                {"nms_threshold", 0.45}, {"max_detections", 120}};
}

inline Json models() {
    return Json{{"schema_version", 1},
                {"runtime", Json{{"npu_device_id", 0}, {"max_loaded_models", 1},
                                  {"outbox_max_bytes", 1048576}, {"max_event_payload_bytes", 65536},
                                  {"event_retry_base_seconds", 1}, {"event_retry_max_seconds", 120},
                                  {"capabilities", Json{
                                      {"formats", Json::array({"om"})},
                                      {"backends", Json::array({"ascendcl"})},
                                      {"model_families", Json::array({"yolo11"})},
                                      {"decoders", Json::array({"yolo_v11"})},
                                      {"pixel_formats", Json::array({"nv21"})},
                                      {"preprocessing", Json::array({"static_aipp"})},
                                      {"soc_versions", Json::array({"Ascend310B4"})},
                                      {"input_shapes", Json::array({Json{{"width", 640}, {"height", 640}},
                                                                     Json{{"width", 416}, {"height", 416}}})}}}}},
                {"models", Json::array({
                    model_profile("model_classroom_behavior_detection", "classroom_behavior_detection",
                                  Json::array({"hand-raising", "reading", "writing"}), 640, 640),
                    model_profile("model_room_occupancy_detection", "room_occupancy", Json::array({"person", "chair"}), 416, 416)})}};
}

inline Json streams() {
    return Json{{"schema_version", 1}, {"global_settings", Json{{"output_fps", 15}}},
                {"streams", Json::array({
                    Json{{"stream_id", "room-a"}, {"camera_id", "camera-a"},
                         {"source_ref", "CAMERA_ROOM_A_URL"}, {"model_id", "model_classroom_behavior_detection"},
                         {"enabled", true}, {"enable_ai", true}, {"live_source", true}, {"output_width", 1280},
                         {"output_height", 720}, {"output_fps", 15}, {"ai_fps", 15},
                         {"output_bitrate_kbps", 1500}, {"input_timeout_ms", 5000},
                         {"reconnect_initial_delay_ms", 1000}, {"reconnect_max_delay_ms", 30000},
                         {"rtsp_transport", "tcp"},
                         {"rtsp_output", Json{{"enabled", true}, {"listen_address", "0.0.0.0"},
                                                {"port", 8554}, {"mount_path", "/room-a"},
                                                {"max_queue_ms", 1000},
                                                {"username_ref", "RTSP_ROOM_A_USERNAME"},
                                                {"password_ref", "RTSP_ROOM_A_PASSWORD"}}}},
                    Json{{"stream_id", "room-b"}, {"camera_id", "camera-b"},
                         {"source_ref", "CAMERA_ROOM_B_URL"}, {"model_id", "model_room_occupancy_detection"},
                         {"enabled", true}, {"enable_ai", true}, {"live_source", true}, {"output_width", 1280},
                         {"output_height", 720}, {"output_fps", 15}, {"ai_fps", 15},
                         {"output_bitrate_kbps", 1500}, {"input_timeout_ms", 5000},
                         {"reconnect_initial_delay_ms", 1000}, {"reconnect_max_delay_ms", 30000},
                         {"rtsp_transport", "tcp"},
                         {"rtsp_output", Json{{"enabled", false}, {"listen_address", "0.0.0.0"},
                                                {"port", 8555}, {"mount_path", "/room-b"},
                                                {"max_queue_ms", 1000},
                                                {"username_ref", "RTSP_ROOM_B_USERNAME"},
                                                {"password_ref", "RTSP_ROOM_B_PASSWORD"}}}}})}};
}

inline Json alarm_rules(bool enabled) {
    return Json{{"schema_version", 1}, {"config_version", 3},
                {"allowed_severities", Json::array({"low", "medium", "high", "critical"})},
                {"alarm_rules", Json::array({
                    Json{{"model_id", "model_classroom_behavior_detection"}, {"class_name", "hand-raising"},
                         {"event_type", "classroom.behavior.hand-raising"}, {"class_attribute", "behavior_class"},
                         {"enabled", enabled}, {"severity", "medium"}, {"confidence_threshold", 0.5},
                         {"min_duration_ms", enabled ? 1000 : 0}, {"cooldown_ms", 5000}},
                    Json{{"model_id", "model_room_occupancy_detection"}, {"class_name", "person"},
                         {"event_type", "room.occupancy.person"}, {"class_attribute", "class_name"},
                         {"enabled", false}, {"severity", "low"}, {"confidence_threshold", 0.5},
                         {"min_duration_ms", 0}, {"cooldown_ms", 0}}})}};
}

inline EdgeAppConfig config(bool alarm_enabled = false) {
    return EdgeAppConfig::parse(models(), streams(), alarm_rules(alarm_enabled));
}

}  // namespace test_fixture
