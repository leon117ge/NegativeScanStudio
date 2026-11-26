// CPU worker count: at least 32 rows per worker, at most NSS_CPU_THREAD_CAP workers
// (0 = no fixed cap, every hardware thread the OS reports; the macOS default).
#include "../src/cpu_render.hpp"
#include <cstdio>
int main(){
 int hw=std::max(1,int(std::thread::hardware_concurrency()));
 int cap=NSS_CPU_THREAD_CAP>0?std::min(hw,int(NSS_CPU_THREAD_CAP)):hw;
 bool ok=int(nss::cpuThreads(1<<20))==cap&&nss::cpuThreads(31)==1&&nss::cpuThreads(0)==1&&int(nss::cpuThreads(64))==std::min(cap,2);
 std::printf("%s: hardware threads %d, NSS_CPU_THREAD_CAP %d, workers for a 4K frame %u, for 2160 rows %u\n",ok?"PASS":"FAIL",hw,int(NSS_CPU_THREAD_CAP),nss::cpuThreads(4320),nss::cpuThreads(2160));
 return ok?0:1;
}
