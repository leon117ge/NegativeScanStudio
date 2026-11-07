#pragma once
#include "prepared.hpp"
#include <thread>
#include <vector>
#include <algorithm>
namespace nss {
struct FrameView {
 float *data;int bounds[4];int stride;
 float *at(int x,int y) const {if(x<bounds[0]||x>=bounds[2]||y<bounds[1]||y>=bounds[3])return nullptr;return reinterpret_cast<float*>(reinterpret_cast<char*>(data)+ptrdiff_t(y-bounds[1])*stride)+4*(x-bounds[0]);}
};
struct CpuJob {
 const Prepared &prepared;FrameView source,dest;const int *window;bool premult;
 int (*aborted)(void*)=nullptr;void *effect=nullptr;
 void rows(unsigned index,unsigned count) const {
  int start=window[1]+int(int64_t(window[3]-window[1])*index/count),end=window[1]+int(int64_t(window[3]-window[1])*(index+1)/count);
  bool inside=window[0]>=source.bounds[0]&&window[2]<=source.bounds[2]&&start>=source.bounds[1]&&end<=source.bounds[3]&&window[0]>=dest.bounds[0]&&window[2]<=dest.bounds[2]&&start>=dest.bounds[1]&&end<=dest.bounds[3];
  for(int y=start;y<end;y++){
   if(aborted&&aborted(effect))return;
   if(inside){const float *in=source.at(window[0],y);float *out=dest.at(window[0],y);for(int x=window[0];x<window[2];x++,in+=4,out+=4)prepared.rgba(in,out,premult);}
   else for(int x=window[0];x<window[2];x++){float *out=dest.at(x,y),*in=source.at(x,y);if(!out)continue;if(in)prepared.rgba(in,out,premult);else std::fill(out,out+4,0.f);}
  }
 }
};
inline unsigned cpuThreads(int rows){return unsigned(std::max(1,std::min({int(std::thread::hardware_concurrency()),8,std::max(1,rows/32)})));}
inline void parallelCpu(const CpuJob &job,unsigned count){
 count=std::max(1u,count);if(count==1){job.rows(0,1);return;}
 std::vector<std::thread> threads;threads.reserve(count-1);
 // Join successfully started threads even if resource allocation fails midway.
 try{for(unsigned i=1;i<count;i++)threads.emplace_back([&,i]{job.rows(i,count);});}
 catch(...){for(auto &t:threads)t.join();job.rows(0,1);return;}
 job.rows(0,count);for(auto &t:threads)t.join();
}
}
