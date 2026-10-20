cmake_minimum_required(VERSION 3.16)
project(yunshao_atlas_edge LANGUAGES CXX)

set(CMAKE_CXX_STANDARD 17)
set(CMAKE_CXX_STANDARD_REQUIRED ON)
set(CMAKE_CXX_EXTENSIONS OFF)

if(NOT DEFINED ASCEND_TOOLKIT_HOME)
  if(DEFINED ENV{ASCEND_HOME_PATH})
    set(ASCEND_TOOLKIT_HOME "$ENV{ASCEND_HOME_PATH}")
  else()
    set(ASCEND_TOOLKIT_HOME "/usr/local/Ascend/ascend-toolkit/latest")
  endif()
endif()

find_package(OpenSSL REQUIRED)
find_package(SQLite3 REQUIRED)
find_package(OpenCV 4 REQUIRED COMPONENTS core imgproc dnn)
find_package(PkgConfig REQUIRED)
pkg_check_modules(MOSQUITTO REQUIRED IMPORTED_TARGET libmosquitto)
pkg_check_modules(FFMPEG REQUIRED IMPORTED_TARGET libavformat libavcodec libavutil)
pkg_check_modules(GSTREAMER REQUIRED IMPORTED_TARGET gstreamer-1.0 gstreamer-app-1.0 gstreamer-rtsp-server-1.0)
find_path(NLOHMANN_JSON_INCLUDE_DIR nlohmann/json.hpp REQUIRED)
find_path(ACL_INCLUDE_DIR acl/acl.h PATHS "${ASCEND_TOOLKIT_HOME}/aarch64-linux/include" REQUIRED)
set(ACL_DVPP_INCLUDE_DIR "${ACL_INCLUDE_DIR}/acl/dvpp")
set(ACL_MEDIA_INCLUDE_DIR "${ACL_INCLUDE_DIR}/acl/media")
find_library(ASCENDCL_LIBRARY ascendcl PATHS "${ASCEND_TOOLKIT_HOME}/aarch64-linux/lib64" REQUIRED)
find_library(ASCEND_DVPP_MPI_LIBRARY acl_dvpp_mpi PATHS "${ASCEND_TOOLKIT_HOME}/aarch64-linux/lib64" REQUIRED)
find_library(ASCEND_VPSS_MPI_LIBRARY acl_vpss_mpi PATHS "${ASCEND_TOOLKIT_HOME}/aarch64-linux/lib64" REQUIRED)
find_library(ASCEND_DVPP_LIBRARY acl_dvpp PATHS "${ASCEND_TOOLKIT_HOME}/aarch64-linux/lib64" REQUIRED)

add_library(edge_core
  src/common/core.cpp
  src/common/config.cpp)
target_include_directories(edge_core PUBLIC include "${NLOHMANN_JSON_INCLUDE_DIR}" "${ACL_INCLUDE_DIR}")
target_link_libraries(edge_core PUBLIC OpenSSL::Crypto SQLite::SQLite3 "${ASCENDCL_LIBRARY}")
target_compile_options(edge_core PRIVATE -Wall -Wextra -Wpedantic)

add_executable(yunshao-agent src/agent/agent.cpp)
target_include_directories(yunshao-agent PRIVATE include "${NLOHMANN_JSON_INCLUDE_DIR}" "${ACL_INCLUDE_DIR}")
target_link_libraries(yunshao-agent PRIVATE edge_core PkgConfig::MOSQUITTO)
target_compile_options(yunshao-agent PRIVATE -Wall -Wextra -Wpedantic)
set_target_properties(yunshao-agent PROPERTIES BUILD_RPATH "${ASCEND_TOOLKIT_HOME}/aarch64-linux/lib64")

add_library(model_classroom_behavior_detection SHARED plugins/model_classroom_behavior_detection.cpp)
target_include_directories(model_classroom_behavior_detection PRIVATE include "${NLOHMANN_JSON_INCLUDE_DIR}" "${ACL_INCLUDE_DIR}" ${OpenCV_INCLUDE_DIRS})
target_link_libraries(model_classroom_behavior_detection PRIVATE "${ASCENDCL_LIBRARY}" ${OpenCV_LIBS})
target_compile_options(model_classroom_behavior_detection PRIVATE -O3 -Wall -Wextra -Wpedantic)
set_target_properties(model_classroom_behavior_detection PROPERTIES
  LIBRARY_OUTPUT_DIRECTORY "${CMAKE_BINARY_DIR}/plugins"
  BUILD_RPATH "${ASCEND_TOOLKIT_HOME}/aarch64-linux/lib64")

add_executable(yunshao-classroom-runtime
  src/main.cpp
  src/manager/ai_processor.cpp
  src/manager/inference_manager.cpp
  src/manager/config_service.cpp
  src/manager/alarm_filter.cpp
  src/manager/video_stream_manager.cpp
  src/media/media_pipeline.cpp
  src/media/rtsp_server.cpp
  src/media/region_osd.cpp)
target_include_directories(yunshao-classroom-runtime PRIVATE include "${NLOHMANN_JSON_INCLUDE_DIR}" "${ACL_INCLUDE_DIR}" "${ACL_DVPP_INCLUDE_DIR}" "${ACL_MEDIA_INCLUDE_DIR}" ${OpenCV_INCLUDE_DIRS})
target_compile_definitions(yunshao-classroom-runtime PRIVATE ENABLE_DVPP_INTERFACE)
target_link_libraries(yunshao-classroom-runtime PRIVATE edge_core dl pthread "${ASCENDCL_LIBRARY}" "${ASCEND_DVPP_MPI_LIBRARY}" "${ASCEND_VPSS_MPI_LIBRARY}" "${ASCEND_DVPP_LIBRARY}" ${OpenCV_LIBS} PkgConfig::FFMPEG PkgConfig::GSTREAMER)
target_compile_options(yunshao-classroom-runtime PRIVATE -O3 -Wall -Wextra -Wpedantic)
set_target_properties(yunshao-classroom-runtime PROPERTIES BUILD_RPATH "${ASCEND_TOOLKIT_HOME}/aarch64-linux/lib64")

install(TARGETS yunshao-agent yunshao-classroom-runtime RUNTIME DESTINATION bin)
install(TARGETS model_classroom_behavior_detection LIBRARY DESTINATION lib/yunshao/plugins)
install(DIRECTORY config/ DESTINATION etc/yunshao)
install(FILES systemd/yunshao-agent.service systemd/yunshao-classroom-runtime.service DESTINATION lib/systemd/system)
install(FILES udev/99-yunshao-rgn.rules DESTINATION /etc/udev/rules.d)

include(CTest)
if(BUILD_TESTING)
  add_executable(edge_core_test tests/edge_core_test.cpp)
  target_include_directories(edge_core_test PRIVATE include "${NLOHMANN_JSON_INCLUDE_DIR}" "${ACL_INCLUDE_DIR}")
  target_link_libraries(edge_core_test PRIVATE edge_core)
  add_test(NAME edge_core_test COMMAND edge_core_test)

  add_executable(model_config_test tests/model_config_test.cpp)
  target_include_directories(model_config_test PRIVATE include "${NLOHMANN_JSON_INCLUDE_DIR}")
  target_link_libraries(model_config_test PRIVATE edge_core)
  add_test(NAME model_config_test COMMAND model_config_test)

  add_executable(model_plugin_identity_test tests/model_plugin_identity_test.cpp)
  target_include_directories(model_plugin_identity_test PRIVATE include "${NLOHMANN_JSON_INCLUDE_DIR}")
  target_link_libraries(model_plugin_identity_test PRIVATE edge_core dl)
  add_test(NAME model_plugin_identity_test
    COMMAND model_plugin_identity_test $<TARGET_FILE:model_classroom_behavior_detection>)
endif()
