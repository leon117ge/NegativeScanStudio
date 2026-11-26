// Metal vs CPU parity check for the NSS OFX render math (macOS proof build).
// Usage: metal_parity <nss_render.metallib>. Prints SKIP and exits 0 when no Metal device exists.
#import <Foundation/Foundation.h>
#import <Metal/Metal.h>
#include "../../ofx/src/prepared.hpp"
#include <cmath>
#include <cstdio>
#include <vector>
#include <algorithm>
struct NssArgs {int sx1,sy1,sx2,sy2,sourceStride,dx1,dy1,dx2,dy2,destStride,wx1,wy1,width,height,premult;};
int main(int argc,char **argv){@autoreleasepool{
 if(argc<2){fprintf(stderr,"usage: metal_parity lib.metallib\n");return 2;}
 id<MTLDevice> dev=MTLCreateSystemDefaultDevice();
 if(!dev){printf("SKIP: no Metal device on this runner\n");return 0;}
 printf("Metal device: %s, unified memory: %d\n",dev.name.UTF8String,(int)dev.hasUnifiedMemory);
 NSError *err=nil;
 id<MTLLibrary> lib=[dev newLibraryWithURL:[NSURL fileURLWithPath:@(argv[1])] error:&err];
 if(!lib){fprintf(stderr,"FAIL: load metallib: %s\n",err.localizedDescription.UTF8String);return 1;}
 id<MTLComputePipelineState> pso=[dev newComputePipelineStateWithFunction:[lib newFunctionWithName:@"nss_render_metal"] error:&err];
 if(!pso){fprintf(stderr,"FAIL: pipeline: %s\n",err.localizedDescription.UTF8String);return 1;}
 const int W=1024,H=256;std::vector<float> in(size_t(W)*H*4);
 for(int y=0;y<H;y++)for(int x=0;x<W;x++){float *q=&in[(size_t(y)*W+x)*4];
  q[0]=float(x)/W*1.2f;q[1]=float(y)/H;q[2]=float((x*7+y*13)%W)/W;q[3]=1.f;}
 int failures=0;
 for(int variant=0;variant<3;variant++){
  nss::Pipeline pipe;if(variant==1)pipe.stage=0;if(variant==2){pipe.tone.shoulder_on=0;pipe.tone.toe_on=0;}
  nss::Prepared prep(pipe);
  std::vector<float> cpu(in.size());
  for(size_t i=0;i<in.size();i+=4)prep.rgba(&in[i],&cpu[i],false);
  // Shared storage: CPU and GPU see the same pages (unified memory, no copies).
  id<MTLBuffer> src=[dev newBufferWithBytes:in.data() length:in.size()*4 options:MTLResourceStorageModeShared];
  id<MTLBuffer> dst=[dev newBufferWithLength:in.size()*4 options:MTLResourceStorageModeShared];
  NssArgs a{0,0,W,H,W*4,0,0,W,H,W*4,0,0,W,H,0};
  id<MTLCommandQueue> queue=[dev newCommandQueue];
  id<MTLCommandBuffer> cmd=[queue commandBuffer];
  id<MTLComputeCommandEncoder> enc=[cmd computeCommandEncoder];
  [enc setComputePipelineState:pso];[enc setBuffer:src offset:0 atIndex:0];[enc setBuffer:dst offset:0 atIndex:1];
  [enc setBytes:prep.values.data() length:prep.values.size()*4 atIndex:2];[enc setBytes:&a length:sizeof a atIndex:3];
  // Threadgroup size from the pipeline itself, not a fixed per-chip constant.
  NSUInteger tw=pso.threadExecutionWidth,th=std::max<NSUInteger>(1,pso.maxTotalThreadsPerThreadgroup/tw);
  [enc dispatchThreads:MTLSizeMake(W,H,1) threadsPerThreadgroup:MTLSizeMake(tw,th,1)];
  [enc endEncoding];[cmd commit];[cmd waitUntilCompleted];
  if(cmd.error){fprintf(stderr,"FAIL: command buffer: %s\n",cmd.error.localizedDescription.UTF8String);return 1;}
  const float *gpu=(const float*)dst.contents;double worstAbs=0,worstRel=0;size_t bad=0;
  for(size_t i=0;i<cpu.size();i++){double d=std::fabs(double(gpu[i])-cpu[i]),r=d/std::max(1e-6,std::fabs(double(cpu[i])));
   if(!std::isfinite(gpu[i])||d>1e-4)bad++;worstAbs=std::max(worstAbs,d);worstRel=std::max(worstRel,std::min(r,d));}
  printf("variant %d: threadgroup %lux%lu, worst abs %.3g, values over 1e-4: %zu of %zu\n",variant,(unsigned long)tw,(unsigned long)th,worstAbs,bad,cpu.size());
  if(bad)failures++;
 }
 if(failures){printf("FAIL: Metal output differs from CPU by more than 1e-4\n");return 1;}
 printf("PASS: Metal matches CPU within 1e-4 on all variants\n");return 0;
}}
