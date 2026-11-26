#pragma once
// Bounded retry for a failed Metal compile or pipeline build (macOS). A failure is not cached for
// the life of the process: the build is tried again after a backoff (2 s, then 8 s), at most
// kMaxAttempts times per device, and after that the device stays on the CPU until the host
// restarts. Included only by metal_render.mm and the tests (not by the Windows/Linux build).
#include <chrono>
namespace nss {
struct MetalRetry {
 static constexpr int kMaxAttempts=3;
 using Clock=std::chrono::steady_clock;
 int attempts=0;
 Clock::time_point retryAt{};
 // True when a new build may be attempted now.
 bool allow(Clock::time_point now) const {return attempts==0||(attempts<kMaxAttempts&&now>=retryAt);}
 // Record a failed build made at 'now'.
 void failed(Clock::time_point now){attempts++;retryAt=now+std::chrono::seconds(attempts==1?2:8);}
 bool exhausted() const {return attempts>=kMaxAttempts;}
};
}
