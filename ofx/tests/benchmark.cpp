#include "baseline/pipeline.hpp"
#include "../src/cpu_render.hpp"
#include "../vendor/json/json.hpp"
#include <fstream>
#include <iostream>
#include <chrono>
#include <cmath>
#include <algorithm>
#include <cstdlib>
using Clock=std::chrono::steady_clock;
__attribute__((noinline)) void before(const float *source,float *dest,size_t n,nss_v090::Pipeline pipe){for(size_t i=0;i<n;i++){auto v=pipe.pixel({source[4*i],source[4*i+1],source[4*i+2]});dest[4*i]=v.x;dest[4*i+1]=v.y;dest[4*i+2]=v.z;dest[4*i+3]=source[4*i+3];}}
__attribute__((noinline)) void after(float *source,float *dest,int width,int height,const nss::Pipeline &pipe,unsigned threads){nss::Prepared p(pipe);int window[]={0,0,width,height};nss::FrameView src{source,{0,0,width,height},width*16},dst{dest,{0,0,width,height},width*16};nss::CpuJob job{p,src,dst,window,false};nss::parallelCpu(job,threads);}
int main(int argc,char **argv){if(argc<5)return 2;int width=std::atoi(argv[2]),height=std::atoi(argv[3]),runs=std::atoi(argv[4]);size_t n=size_t(width)*height;std::vector<float> input(n*4),old(n*4),output(n*4);std::ifstream f(argv[1],std::ios::binary);f.read(reinterpret_cast<char*>(input.data()),input.size()*4);if(size_t(f.gcount())!=input.size()*4)return 3;
 nss_v090::Pipeline a;a.density.base_r=a.density.base_g=a.density.base_b=.5f;nss::Pipeline b;
 a.positive.film_wb=0;a.positive.bal_r=1.57f;a.positive.bal_b=1.f; // Equivalent measured 500T Base, unchanged old implementation.
 unsigned threads=nss::cpuThreads(height);std::vector<double> ta,tb,tc;
 auto measure=[&](auto &&fn){auto start=Clock::now();fn();return std::chrono::duration<double,std::milli>(Clock::now()-start).count();};
 before(input.data(),old.data(),n,a);after(input.data(),output.data(),width,height,b,1);after(input.data(),output.data(),width,height,b,threads);
 double absolute=0,scaled=0,checksum=0;for(size_t i=0;i<output.size();i++){if(!std::isfinite(old[i])||!std::isfinite(output[i]))return 4;double error=std::abs(double(old[i])-double(output[i]));absolute=std::max(absolute,error);scaled=std::max(scaled,error/std::max(1.,std::abs(double(old[i]))));checksum+=output[i];}if(scaled>1e-5)return 5;
 for(int r=0;r<runs;r++){
  ta.push_back(measure([&]{before(input.data(),old.data(),n,a);}));
  tb.push_back(measure([&]{after(input.data(),output.data(),width,height,b,1);}));
  tc.push_back(measure([&]{after(input.data(),output.data(),width,height,b,threads);}));
  std::cerr<<"pair "<<r+1<<": "<<ta.back()<<" / "<<tb.back()<<" / "<<tc.back()<<" ms\n";
 }
 // Consume every result outside the timed section; verify the work again.
 double checksum2=0;for(float v:output)checksum2+=v;if(std::abs(checksum2-checksum)>1e-3)return 6;
 auto stats=[](std::vector<double> v){std::sort(v.begin(),v.end());return nlohmann::json{{"median_ms",v[v.size()/2]},{"min_ms",v.front()},{"max_ms",v.back()}};};
 std::cout<<nlohmann::json{{"width",width},{"height",height},{"pixels",n},{"runs",runs},{"threads",threads},{"before_ms",ta},{"prepared_1thread_ms",tb},{"prepared_parallel_ms",tc},{"before",stats(ta)},{"prepared_1thread",stats(tb)},{"prepared_parallel",stats(tc)},{"max_absolute_error",absolute},{"max_scaled_error",scaled},{"checksum",checksum},{"failures",0},{"scope","CPU full-frame in-memory RGBA render only; no decode, UI, metadata I/O, GPU or Resolve overhead"}}.dump(2)<<"\n";
 return 0;
}
