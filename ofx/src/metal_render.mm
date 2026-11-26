// Metal render path for macOS (Apple Silicon and any Metal device). Objective-C++, ARC.
// Linked only into macOS builds compiled with -DNSS_METAL; see metal.hpp.
// - Uses the host's MTLCommandQueue and the host's MTLBuffers (zero copy on unified memory).
// - Encodes one compute dispatch and commits it without waitUntilCompleted.
// - Compiles the embedded MSL (metal_source.hpp, generated from render_math.h) once per MTLDevice
//   with fast math off, and caches the compute pipeline per device.
// - Threadgroup size comes from the pipeline (threadExecutionWidth, maxTotalThreadsPerThreadgroup).
// - Every failure returns false with a reason; plugin.cpp then renders on the CPU.
#if !defined(__APPLE__)
#error "metal_render.mm is macOS only"
#endif
#if !defined(NSS_METAL)
#error "compile metal_render.mm with -DNSS_METAL (plugin.cpp too), see metal.hpp"
#endif
#if !__has_feature(objc_arc)
#error "metal_render.mm must be compiled with -fobjc-arc"
#endif
#import <Foundation/Foundation.h>
#import <Metal/Metal.h>
#include "metal.hpp"
#include "metal_source.hpp"
#include <algorithm>
#include <cstdint>
#include <map>
#include <mutex>
#include <string>

namespace {
std::string describeError(NSError *error,const char *fallback){
 if(!error)return fallback;const char *text=error.localizedDescription.UTF8String;return text?text:fallback;
}
id<MTLComputePipelineState> buildPipeline(id<MTLDevice> device,NSString *source,NSString *function,std::string &error){
 MTLCompileOptions *options=[MTLCompileOptions new];
 // Metal compiles with fast math by default. The CPU reference is IEEE float, so turn it off.
#pragma clang diagnostic push
#pragma clang diagnostic ignored "-Wdeprecated-declarations"
 options.fastMathEnabled=NO;
#pragma clang diagnostic pop
#if defined(__MAC_OS_X_VERSION_MAX_ALLOWED) && __MAC_OS_X_VERSION_MAX_ALLOWED >= 150000
 if(@available(macOS 15.0,*)){options.mathMode=MTLMathModeSafe;options.mathFloatingPointFunctions=MTLMathFloatingPointFunctionsPrecise;}
#endif
 NSError *failure=nil;
 id<MTLLibrary> library=[device newLibraryWithSource:source options:options error:&failure];
 if(!library){error="Metal compile failed: "+describeError(failure,"no library");return nil;}
 id<MTLFunction> kernel=[library newFunctionWithName:function];
 if(!kernel){error="Metal pipeline failed: kernel function not found";return nil;}
 failure=nil;
 id<MTLComputePipelineState> pipeline=[device newComputePipelineStateWithFunction:kernel error:&failure];
 if(!pipeline){error="Metal pipeline failed: "+describeError(failure,"no pipeline");return nil;}
 nss::MetalApi::get().pipelineBuilds.fetch_add(1);
 return pipeline;
}
// One compiled pipeline (or the compile error) per MTLDevice. Resolve can hand different devices
// to different nodes (multi-GPU, eGPU), so the cache key is the device's registry ID.
struct CacheEntry {id<MTLDevice> device;id<MTLComputePipelineState> pipeline;std::string error;};
std::mutex cacheMutex;
std::map<uint64_t,CacheEntry> &cache(){static auto *entries=new std::map<uint64_t,CacheEntry>();return *entries;}
id<MTLComputePipelineState> pipelineFor(id<MTLDevice> device,std::string &error
#ifdef NSS_TEST_HOOKS
 ,int fault
#endif
){
#ifdef NSS_TEST_HOOKS
 // Forced failures bypass the cache so they never poison a release pipeline. Compiled only
 // into test builds (-DNSS_TEST_HOOKS); the hook strings must not appear in a release binary.
 if(fault==nss::kMetalFaultCompile)return buildPipeline(device,@"this is not Metal source",@"nss_render_metal",error);
 if(fault==nss::kMetalFaultPipeline){
  id<MTLComputePipelineState> pipeline=buildPipeline(device,@(nss::metalSource),@"nss_render_metal_missing",error);
  return pipeline;
 }
#endif
 std::lock_guard<std::mutex> lock(cacheMutex);
 auto &entry=cache()[device.registryID];
 if(entry.pipeline)return entry.pipeline;
 if(!entry.error.empty()){error=entry.error;return nil;}
 entry.device=device;
 entry.pipeline=buildPipeline(device,@(nss::metalSource),@"nss_render_metal",entry.error);
 if(!entry.pipeline)error=entry.error;
 return entry.pipeline;
}
// Bytes a buffer must hold for rows*stride floats where only the last row may be short.
bool fits(id<MTLBuffer> buffer,int x1,int y1,int x2,int y2,int stride){
 if(x2<=x1||y2<=y1)return true;
 if(stride<(x2-x1)*4)return false;
 uint64_t need=(uint64_t(y2-y1-1)*uint64_t(stride)+uint64_t(x2-x1)*4)*sizeof(float);
 return buffer.length>=need;
}
bool encode(void *queuePointer,void *sourcePointer,void *destPointer,const float *params,size_t count,const nss::MetalArgs &args,
 const std::shared_ptr<nss::MetalStatus> &status,std::string &deviceName,std::string &error){
 @autoreleasepool{
  @try{
#ifdef NSS_TEST_HOOKS
   int fault=nss::MetalApi::get().fault.load();
#endif
   id<MTLCommandQueue> queue=(__bridge id<MTLCommandQueue>)queuePointer;
   id<MTLBuffer> source=(__bridge id<MTLBuffer>)sourcePointer,dest=(__bridge id<MTLBuffer>)destPointer;
   if(!queue){error="Metal: host did not provide a command queue";return false;}
   if(!source||!dest){error="Metal: host did not provide image buffers";return false;}
   id<MTLDevice> device=queue.device;
   if(!device
#ifdef NSS_TEST_HOOKS
      ||fault==nss::kMetalFaultNoDevice
#endif
     ){error="Metal: no device";return false;}
   if(source.device!=device||dest.device!=device){error="Metal: image buffers belong to another device";return false;}
   if(count*sizeof(float)>4096){error="Metal: parameter block too large for setBytes";return false;}
   if(!fits(source,args.sx1,args.sy1,args.sx2,args.sy2,args.sourceStride)||!fits(dest,args.dx1,args.dy1,args.dx2,args.dy2,args.destStride)){
    error="Metal: image buffer smaller than its bounds and row bytes";return false;}
#ifdef NSS_TEST_HOOKS
   id<MTLComputePipelineState> pipeline=pipelineFor(device,error,fault);
#else
   id<MTLComputePipelineState> pipeline=pipelineFor(device,error);
#endif
   if(!pipeline)return false;
#ifdef NSS_TEST_HOOKS
   if(fault==nss::kMetalFaultEncode){error="Metal encode failed (forced by test)";return false;}
#endif
   deviceName=device.name.UTF8String?device.name.UTF8String:"Metal device";
   if(args.width<=0||args.height<=0)return true;
   id<MTLCommandBuffer> commands=[queue commandBuffer];
   if(!commands){error="Metal encode failed: no command buffer";return false;}
   commands.label=@"NegativeScan Studio render";
   id<MTLComputeCommandEncoder> encoder=[commands computeCommandEncoder];
   if(!encoder){error="Metal encode failed: no compute encoder";return false;}
   [encoder setComputePipelineState:pipeline];
   [encoder setBuffer:source offset:0 atIndex:0];
   [encoder setBuffer:dest offset:0 atIndex:1];
   [encoder setBytes:params length:count*sizeof(float) atIndex:2];
   [encoder setBytes:&args length:sizeof(args) atIndex:3];
   // Threadgroup shape from the pipeline itself: no per-chip constants. The kernel bounds-checks,
   // so whole threadgroups work on every Metal device (no non-uniform threadgroup requirement).
   NSUInteger width=std::max<NSUInteger>(1,pipeline.threadExecutionWidth);
   NSUInteger height=std::max<NSUInteger>(1,pipeline.maxTotalThreadsPerThreadgroup/width);
   MTLSize groups=MTLSizeMake((NSUInteger(args.width)+width-1)/width,(NSUInteger(args.height)+height-1)/height,1);
   [encoder dispatchThreadgroups:groups threadsPerThreadgroup:MTLSizeMake(width,height,1)];
   [encoder endEncoding];
   std::shared_ptr<nss::MetalStatus> keep=status;
   [commands addCompletedHandler:^(id<MTLCommandBuffer> done){
    if(done.status==MTLCommandBufferStatusError&&keep)keep->fail(describeError(done.error,"command buffer error"));
   }];
   // No waitUntilCompleted: the host owns the queue and orders its later work after this buffer.
   [commands commit];
   return true;
  }@catch(NSException *exception){
   const char *reason=exception.reason.UTF8String;error=std::string("Metal exception: ")+(reason?reason:"unknown");return false;
  }
 }
}
void *contents(void *bufferPointer){
 @autoreleasepool{
  @try{
   id<MTLBuffer> buffer=(__bridge id<MTLBuffer>)bufferPointer;
   if(!buffer||buffer.storageMode!=MTLStorageModeShared)return nullptr;
   return buffer.contents;
  }@catch(NSException *){return nullptr;}
 }
}
bool drain(void *queuePointer){
 @autoreleasepool{
  @try{
   id<MTLCommandQueue> queue=(__bridge id<MTLCommandQueue>)queuePointer;
   if(!queue)return false;
   // Command buffers on one queue execute in commit order; an empty one completes after them.
   id<MTLCommandBuffer> marker=[queue commandBuffer];
   if(!marker)return false;
   marker.label=@"NegativeScan Studio CPU fallback sync";
   [marker commit];[marker waitUntilCompleted];
   return marker.status==MTLCommandBufferStatusCompleted;
  }@catch(NSException *){return false;}
 }
}
}

namespace nss {
void metalInstall(MetalApi &api){
 @autoreleasepool{
  @try{
   NSArray<id<MTLDevice>> *devices=MTLCopyAllDevices();
   if(devices.count==0){api.available=false;api.availability="Metal: no device";return;}
   id<MTLDevice> first=devices.firstObject;
   api.encode=encode;api.contents=contents;api.drain=drain;
   api.available=true;
   api.availability=std::string("Metal: ")+(first.name.UTF8String?first.name.UTF8String:"device")+
    (first.hasUnifiedMemory?" (unified memory)":"")+(devices.count>1?" and "+std::to_string(devices.count-1)+" more":"");
  }@catch(NSException *){api.available=false;api.availability="Metal: initialisation failed";}
 }
}
}
