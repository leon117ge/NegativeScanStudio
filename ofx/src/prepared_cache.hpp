#pragma once
#include "prepared.hpp"
#include <mutex>
#include <cstdint>
#include <optional>
namespace nss {
struct PreparedCache {
 std::mutex mutex;std::optional<Pipeline> input;std::optional<Prepared> prepared;uint64_t builds=0;
 Prepared get(const Pipeline &p){std::lock_guard<std::mutex> lock(mutex);
  if(!input||!samePipeline(*input,p)){prepared.emplace(p);input=p;++builds;}return *prepared;}
};
}
