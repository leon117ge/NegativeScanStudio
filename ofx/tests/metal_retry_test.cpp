// Bounded retry of a failed Metal pipeline build (ofx/src/metal_retry.hpp, used by metal_render.mm).
#include "../src/metal_retry.hpp"
#include <cstdio>
#include <cstdlib>
static void require(bool ok,const char *what){if(!ok){std::fprintf(stderr,"FAIL: %s\n",what);std::exit(1);}}
int main(){
 using nss::MetalRetry;using std::chrono::seconds;using std::chrono::milliseconds;
 MetalRetry r;auto t0=MetalRetry::Clock::now();
 require(r.allow(t0)&&!r.exhausted(),"first build allowed");
 r.failed(t0);
 require(!r.allow(t0)&&!r.allow(t0+milliseconds(1999)),"no retry inside the first backoff");
 require(r.allow(t0+seconds(2)),"retry after 2 s");
 auto t1=t0+seconds(2);r.failed(t1);
 require(!r.allow(t1+seconds(7))&&r.allow(t1+seconds(8)),"second backoff 8 s");
 auto t2=t1+seconds(8);r.failed(t2);
 require(r.exhausted()&&r.attempts==MetalRetry::kMaxAttempts,"three attempts at most");
 require(!r.allow(t2+seconds(3600)),"stays on CPU after the last attempt");
 std::printf("PASS: Metal pipeline retry: %d attempts, backoff 2 s then 8 s, then CPU until restart\n",MetalRetry::kMaxAttempts);
}
