#pragma once
#include <cstdint>
#include "rust/cxx.h"

::std::int32_t run_app(bool start_hidden);
bool request_background(bool autostart);
::std::int64_t free_bytes(::rust::Str path);
