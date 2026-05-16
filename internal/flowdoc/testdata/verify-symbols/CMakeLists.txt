cmake_minimum_required(VERSION 3.20)
project(recmeet)

add_executable(recmeet src/main.cpp)
add_executable(recmeet-daemon src/daemon.cpp)
add_library(recmeet-core STATIC src/core.cpp)
add_subdirectory(vendor)

set(BUILD_FLAGS "-O2")

function(emit_targets)
    message("ok")
endfunction()
