cmake_minimum_required(VERSION 3.16)
project(atlas_yolo_acl_demo LANGUAGES CXX)

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

find_package(OpenCV 4 REQUIRED COMPONENTS core imgproc imgcodecs videoio dnn)
find_path(ACL_INCLUDE_DIR acl/acl.h PATHS "${ASCEND_TOOLKIT_HOME}/aarch64-linux/include" REQUIRED)
find_library(ASCENDCL_LIBRARY ascendcl PATHS "${ASCEND_TOOLKIT_HOME}/aarch64-linux/lib64" REQUIRED)

add_executable(atlas_yolo_acl_demo src/main.cpp)
target_include_directories(atlas_yolo_acl_demo PRIVATE "${ACL_INCLUDE_DIR}" ${OpenCV_INCLUDE_DIRS})
target_link_libraries(atlas_yolo_acl_demo PRIVATE "${ASCENDCL_LIBRARY}" ${OpenCV_LIBS})
target_compile_options(atlas_yolo_acl_demo PRIVATE -O3 -Wall -Wextra -Wpedantic)
set_target_properties(atlas_yolo_acl_demo PROPERTIES BUILD_RPATH "${ASCEND_TOOLKIT_HOME}/aarch64-linux/lib64")
