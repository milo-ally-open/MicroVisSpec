cmake_minimum_required(VERSION 3.16)
project(yunshao_atlas_edge LANGUAGES CXX)

set(CMAKE_CXX_STANDARD 17)
set(CMAKE_CXX_STANDARD_REQUIRED ON)
set(CMAKE_CXX_EXTENSIONS OFF)

option(YUNSHAO_AGENT_ONLY "Build the agent target for remote first-time provisioning" OFF)

set(YUNSHAO_CANN_ENV_FILE "/etc/yunshao/cann.env" CACHE FILEPATH
  "CANN runtime environment file shared with systemd services")
if(NOT DEFINED ASCEND_TOOLKIT_HOME AND EXISTS "${YUNSHAO_CANN_ENV_FILE}")
  file(STRINGS "${YUNSHAO_CANN_ENV_FILE}" _cann_toolkit_home_line
    REGEX "^ASCEND_TOOLKIT_HOME=")
  if(_cann_toolkit_home_line)
    string(REGEX REPLACE "^ASCEND_TOOLKIT_HOME=" "" ASCEND_TOOLKIT_HOME
      "${_cann_toolkit_home_line}")
  endif()
endif()
if(NOT DEFINED ASCEND_TOOLKIT_HOME)
  if(DEFINED ENV{ASCEND_HOME_PATH})
    set(ASCEND_TOOLKIT_HOME "$ENV{ASCEND_HOME_PATH}")
  else()
    set(ASCEND_TOOLKIT_HOME "/usr/local/Ascend/ascend-toolkit/latest")
  endif()
endif()

find_package(OpenSSL REQUIRED)
find_package(SQLite3 REQUIRED)
find_package(PkgConfig REQUIRED)
pkg_check_modules(MOSQUITTO REQUIRED IMPORTED_TARGET libmosquitto)
find_path(NLOHMANN_JSON_INCLUDE_DIR nlohmann/json.hpp REQUIRED)
find_path(ACL_INCLUDE_DIR acl/acl.h PATHS "${ASCEND_TOOLKIT_HOME}/aarch64-linux/include" REQUIRED)
set(ACL_DVPP_INCLUDE_DIR "${ACL_INCLUDE_DIR}/acl/dvpp")
set(ACL_MEDIA_INCLUDE_DIR "${ACL_INCLUDE_DIR}/acl/media")
find_library(ASCENDCL_LIBRARY ascendcl PATHS "${ASCEND_TOOLKIT_HOME}/aarch64-linux/lib64" REQUIRED)
find_library(ASCEND_DVPP_MPI_LIBRARY acl_dvpp_mpi PATHS "${ASCEND_TOOLKIT_HOME}/aarch64-linux/lib64" REQUIRED)
find_library(ASCEND_VPSS_MPI_LIBRARY acl_vpss_mpi PATHS "${ASCEND_TOOLKIT_HOME}/aarch64-linux/lib64" REQUIRED)
find_library(ASCEND_DVPP_LIBRARY acl_dvpp PATHS "${ASCEND_TOOLKIT_HOME}/aarch64-linux/lib64" REQUIRED)

# Keep installed CLI/service binaries runnable without relying on an interactive
# shell. Include the toolkit and driver paths supplied by the board's CANN setup.
set(YUNSHAO_ASCEND_RUNTIME_LIB_DIRS
  "${ASCEND_TOOLKIT_HOME}/aarch64-linux/lib64"
  "${ASCEND_TOOLKIT_HOME}/lib64")
set(_cann_configured_ld_library_path "")
if(EXISTS "${YUNSHAO_CANN_ENV_FILE}")
  file(STRINGS "${YUNSHAO_CANN_ENV_FILE}" _cann_ld_library_path_line
    REGEX "^LD_LIBRARY_PATH=")
  if(_cann_ld_library_path_line)
    string(REGEX REPLACE "^LD_LIBRARY_PATH=" "" _cann_configured_ld_library_path
      "${_cann_ld_library_path_line}")
  endif()
endif()
if(_cann_configured_ld_library_path)
  string(REPLACE ":" ";" _ascend_env_library_dirs "${_cann_configured_ld_library_path}")
elseif(DEFINED ENV{LD_LIBRARY_PATH})
  string(REPLACE ":" ";" _ascend_env_library_dirs "$ENV{LD_LIBRARY_PATH}")
else()
  set(_ascend_env_library_dirs "")
endif()
if(_ascend_env_library_dirs)
  foreach(_library_dir IN LISTS _ascend_env_library_dirs)
    string(FIND "${_library_dir}" "${ASCEND_TOOLKIT_HOME}/" _toolkit_path_position)
    if(_toolkit_path_position EQUAL 0 OR _library_dir MATCHES "/davinci/driver/lib64")
      list(APPEND YUNSHAO_ASCEND_RUNTIME_LIB_DIRS "${_library_dir}")
    endif()
  endforeach()
endif()
list(REMOVE_DUPLICATES YUNSHAO_ASCEND_RUNTIME_LIB_DIRS)

function(configure_ascend_runtime_rpath target_name)
  set_target_properties(${target_name} PROPERTIES
    BUILD_RPATH "${YUNSHAO_ASCEND_RUNTIME_LIB_DIRS}"
    INSTALL_RPATH "${YUNSHAO_ASCEND_RUNTIME_LIB_DIRS}"
    INSTALL_RPATH_USE_LINK_PATH TRUE)
  target_link_options(${target_name} PRIVATE -Wl,--disable-new-dtags)
endfunction()

add_library(edge_core
  src/common/core.cpp
  src/common/cloud_config.cpp
  src/common/config.cpp)
target_include_directories(edge_core PUBLIC include "${NLOHMANN_JSON_INCLUDE_DIR}" "${ACL_INCLUDE_DIR}")
target_link_libraries(edge_core PUBLIC OpenSSL::Crypto "${ASCENDCL_LIBRARY}")
target_compile_options(edge_core PRIVATE -Wall -Wextra -Wpedantic)

add_library(edge_event_outbox_adapter src/adapters/sqlite_event_outbox.cpp)
target_include_directories(edge_event_outbox_adapter PUBLIC include "${NLOHMANN_JSON_INCLUDE_DIR}")
target_link_libraries(edge_event_outbox_adapter PUBLIC SQLite::SQLite3)
target_compile_options(edge_event_outbox_adapter PRIVATE -Wall -Wextra -Wpedantic)

add_library(edge_mqtt_transport_adapter src/adapters/mosquitto_mqtt_transport.cpp)
target_include_directories(edge_mqtt_transport_adapter PUBLIC include)
target_link_libraries(edge_mqtt_transport_adapter PUBLIC PkgConfig::MOSQUITTO)
target_compile_options(edge_mqtt_transport_adapter PRIVATE -Wall -Wextra -Wpedantic)

add_executable(yunshao-agent
  src/agent/agent.cpp
  src/agent/activation.cpp
  src/application/agent_activation_workflow.cpp
  src/application/agent_run_loop.cpp
  src/application/agent_cloud_command_processor.cpp
  src/application/agent_cloud_cycle.cpp
  src/application/agent_applied_rule_acknowledger.cpp
  src/application/agent_command_acknowledger.cpp
  src/application/agent_mqtt_inbox.cpp
  src/application/agent_mqtt_session.cpp
  src/adapters/agent_cloud_rule_command_settings.cpp
  src/adapters/agent_applied_cloud_rule_state.cpp
  src/adapters/file_activation_lease_writer.cpp
  src/adapters/file_agent_configuration_store.cpp
  src/adapters/curl_https_transport.cpp
  src/adapters/https_evidence_uploader.cpp
  src/adapters/system_event_metadata.cpp
  src/adapters/systemd_agent_service_lifecycle.cpp
  src/adapters/unix_agent_control_server.cpp)
find_package(CURL REQUIRED)
target_include_directories(yunshao-agent PRIVATE include "${NLOHMANN_JSON_INCLUDE_DIR}" "${ACL_INCLUDE_DIR}")
target_link_libraries(yunshao-agent PRIVATE edge_core edge_event_outbox_adapter edge_mqtt_transport_adapter CURL::libcurl)
target_compile_options(yunshao-agent PRIVATE -Wall -Wextra -Wpedantic)
configure_ascend_runtime_rpath(yunshao-agent)

install(TARGETS yunshao-agent RUNTIME DESTINATION bin COMPONENT agent)
install(FILES systemd/yunshao-agent.service DESTINATION lib/systemd/system COMPONENT agent)

if(NOT YUNSHAO_AGENT_ONLY)
  find_package(OpenCV 4 REQUIRED COMPONENTS core imgproc imgcodecs dnn)
  pkg_check_modules(FFMPEG REQUIRED IMPORTED_TARGET libavformat libavcodec libavutil)
  pkg_check_modules(GSTREAMER REQUIRED IMPORTED_TARGET gstreamer-1.0 gstreamer-app-1.0 gstreamer-rtsp-server-1.0)

  add_library(model_classroom_behavior_detection SHARED plugins/model_classroom_behavior_detection.cpp)
  target_include_directories(model_classroom_behavior_detection PRIVATE include "${NLOHMANN_JSON_INCLUDE_DIR}" "${ACL_INCLUDE_DIR}" ${OpenCV_INCLUDE_DIRS})
  target_link_libraries(model_classroom_behavior_detection PRIVATE "${ASCENDCL_LIBRARY}" ${OpenCV_LIBS})
  target_compile_options(model_classroom_behavior_detection PRIVATE -O3 -Wall -Wextra -Wpedantic)
  set_target_properties(model_classroom_behavior_detection PROPERTIES
    LIBRARY_OUTPUT_DIRECTORY "${CMAKE_BINARY_DIR}/plugins"
    BUILD_RPATH "${YUNSHAO_ASCEND_RUNTIME_LIB_DIRS}"
    INSTALL_RPATH "${YUNSHAO_ASCEND_RUNTIME_LIB_DIRS}"
    INSTALL_RPATH_USE_LINK_PATH TRUE)
  target_link_options(model_classroom_behavior_detection PRIVATE -Wl,--disable-new-dtags)

  add_executable(yunshao-classroom-runtime
    src/main.cpp
    src/application/activation_lease_gate.cpp
    src/application/alarm_event_publisher.cpp
    src/adapters/agent_event_socket_outbox.cpp
    src/adapters/file_activation_lease_status.cpp
    src/adapters/file_alarm_evidence_outbox.cpp
    src/adapters/system_event_metadata.cpp
    src/adapters/ai_model_plugin.cpp
    src/manager/ai_processor.cpp
    src/manager/inference_manager.cpp
    src/manager/config_service.cpp
    src/manager/alarm_filter.cpp
    src/manager/video_stream_manager.cpp
    src/adapters/atlas_evidence_frame_encoder.cpp
    src/adapters/file_runtime_rule_state_reader.cpp
    src/media/media_pipeline.cpp
    src/media/rtsp_server.cpp
    src/media/region_osd.cpp)
  target_include_directories(yunshao-classroom-runtime PRIVATE include "${NLOHMANN_JSON_INCLUDE_DIR}" "${ACL_INCLUDE_DIR}" "${ACL_DVPP_INCLUDE_DIR}" "${ACL_MEDIA_INCLUDE_DIR}" ${OpenCV_INCLUDE_DIRS})
  target_compile_definitions(yunshao-classroom-runtime PRIVATE ENABLE_DVPP_INTERFACE)
  target_link_libraries(yunshao-classroom-runtime PRIVATE edge_core dl pthread "${ASCENDCL_LIBRARY}" "${ASCEND_DVPP_MPI_LIBRARY}" "${ASCEND_VPSS_MPI_LIBRARY}" "${ASCEND_DVPP_LIBRARY}" ${OpenCV_LIBS} PkgConfig::FFMPEG PkgConfig::GSTREAMER)
  target_compile_options(yunshao-classroom-runtime PRIVATE -O3 -Wall -Wextra -Wpedantic)
  configure_ascend_runtime_rpath(yunshao-classroom-runtime)

  install(TARGETS yunshao-classroom-runtime RUNTIME DESTINATION bin COMPONENT runtime)
  install(TARGETS model_classroom_behavior_detection LIBRARY DESTINATION lib/yunshao/plugins COMPONENT runtime)
  install(DIRECTORY config/ DESTINATION /etc/yunshao COMPONENT config)
  install(FILES systemd/yunshao-classroom-runtime.service DESTINATION lib/systemd/system COMPONENT runtime)
  install(FILES udev/99-yunshao-rgn.rules DESTINATION /etc/udev/rules.d COMPONENT runtime)
endif()

include(CTest)
if(BUILD_TESTING AND NOT YUNSHAO_AGENT_ONLY)
  add_executable(cloud_config_test tests/cloud_config_test.cpp)
  target_link_libraries(cloud_config_test PRIVATE edge_core)
  configure_ascend_runtime_rpath(cloud_config_test)
  add_test(NAME cloud_config_test COMMAND cloud_config_test)
  add_executable(edge_core_test tests/edge_core_test.cpp)
  target_include_directories(edge_core_test PRIVATE include "${NLOHMANN_JSON_INCLUDE_DIR}" "${ACL_INCLUDE_DIR}")
  target_link_libraries(edge_core_test PRIVATE edge_core edge_event_outbox_adapter)
  configure_ascend_runtime_rpath(edge_core_test)
  add_test(NAME edge_core_test COMMAND edge_core_test)

  add_executable(model_config_test tests/model_config_test.cpp)
  target_include_directories(model_config_test PRIVATE include "${NLOHMANN_JSON_INCLUDE_DIR}")
  target_link_libraries(model_config_test PRIVATE edge_core)
  configure_ascend_runtime_rpath(model_config_test)
  add_test(NAME model_config_test COMMAND model_config_test)

  add_executable(model_plugin_identity_test tests/model_plugin_identity_test.cpp)
  target_include_directories(model_plugin_identity_test PRIVATE include "${NLOHMANN_JSON_INCLUDE_DIR}")
  target_link_libraries(model_plugin_identity_test PRIVATE edge_core dl)
  configure_ascend_runtime_rpath(model_plugin_identity_test)
  add_test(NAME model_plugin_identity_test
    COMMAND model_plugin_identity_test $<TARGET_FILE:model_classroom_behavior_detection>)
  # These tests use assertions; keep them active in release builds on the board.
  foreach(_test_target cloud_config_test edge_core_test model_config_test model_plugin_identity_test)
    target_compile_options(${_test_target} PRIVATE -UNDEBUG)
  endforeach()
endif()
