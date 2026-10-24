cmake_minimum_required(VERSION 3.16)
# The Deck Link kit, as an ESP-IDF component.
list(APPEND EXTRA_COMPONENT_DIRS ${CMAKE_CURRENT_LIST_DIR}/../../deck-link)
include($ENV{IDF_PATH}/tools/cmake/project.cmake)
project(t4s3-panel)
