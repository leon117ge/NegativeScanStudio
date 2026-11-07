#pragma once
// Minimal public CUDA Driver ABI bindings; no proprietary SDK headers are redistributed.
// API contracts: https://docs.nvidia.com/cuda/cuda-driver-api/
#include "prepared.hpp"
#include "cuda_source.hpp"
#include <cstdint>
#include <cstring>
#include <map>
#include <mutex>
#include <memory>
#include <vector>
#include <stdexcept>
#ifdef _WIN32
#ifndef NOMINMAX
#define NOMINMAX
#endif
#include <windows.h>
#define NSS_CUDA_CALL __stdcall
#else
#include <dlfcn.h>
#define NSS_CUDA_CALL
#endif
namespace nss {
using CudaContext=void*;using CudaModule=void*;using CudaFunction=void*;using CudaStream=void*;using CudaPointer=uint64_t;
struct CudaCopy2D {
 size_t srcX,srcY;int srcType;const void *srcHost;CudaPointer srcDevice;void *srcArray;size_t srcPitch;
 size_t dstX,dstY;int dstType;void *dstHost;CudaPointer dstDevice;void *dstArray;size_t dstPitch;
 size_t width,height;
};
static_assert(sizeof(CudaCopy2D)==128,"CUDA 64-bit memcpy ABI");
struct CudaApi {
 int (NSS_CUDA_CALL *init)(unsigned)=nullptr;
 int (NSS_CUDA_CALL *getCurrent)(CudaContext*)=nullptr;
 int (NSS_CUDA_CALL *streamContext)(CudaStream,CudaContext*)=nullptr;
 int (NSS_CUDA_CALL *pushCurrent)(CudaContext)=nullptr;
 int (NSS_CUDA_CALL *popCurrent)(CudaContext*)=nullptr;
 int (NSS_CUDA_CALL *pointerAttribute)(void*,int,CudaPointer)=nullptr;
 int (NSS_CUDA_CALL *loadModule)(CudaModule*,const void*)=nullptr;
 int (NSS_CUDA_CALL *getFunction)(CudaFunction*,CudaModule,const char*)=nullptr;
 int (NSS_CUDA_CALL *unloadModule)(CudaModule)=nullptr;
 int (NSS_CUDA_CALL *launch)(CudaFunction,unsigned,unsigned,unsigned,unsigned,unsigned,unsigned,unsigned,CudaStream,void**,void**)=nullptr;
 int (NSS_CUDA_CALL *synchronize)(CudaStream)=nullptr;
 int (NSS_CUDA_CALL *hostAlloc)(void**,size_t,unsigned)=nullptr;
 int (NSS_CUDA_CALL *hostFree)(void*)=nullptr;
 int (NSS_CUDA_CALL *copy2DAsync)(const CudaCopy2D*,CudaStream)=nullptr;
 int (NSS_CUDA_CALL *errorString)(int,const char**)=nullptr;
 bool available=false;std::string availability="NVIDIA CUDA driver not found";
 CudaApi(){
#ifdef _WIN32
  auto module=LoadLibraryExW(L"nvcuda.dll",nullptr,LOAD_LIBRARY_SEARCH_SYSTEM32);
  auto symbol=[&](const char *name){return module?GetProcAddress(module,name):nullptr;};
#else
  auto module=dlopen("libcuda.so.1",RTLD_NOW|RTLD_LOCAL);
  auto symbol=[&](const char *name){return module?dlsym(module,name):nullptr;};
#endif
  if(!module)return;
#define LOAD(member,name) {auto p=symbol(name);static_assert(sizeof(p)==sizeof(member));std::memcpy(&member,&p,sizeof(member));if(!member){availability=std::string("Missing CUDA symbol: ")+name;return;}}
  LOAD(init,"cuInit");LOAD(getCurrent,"cuCtxGetCurrent");LOAD(streamContext,"cuStreamGetCtx");LOAD(pushCurrent,"cuCtxPushCurrent_v2");LOAD(popCurrent,"cuCtxPopCurrent_v2");
  LOAD(pointerAttribute,"cuPointerGetAttribute");LOAD(loadModule,"cuModuleLoadData");LOAD(getFunction,"cuModuleGetFunction");LOAD(unloadModule,"cuModuleUnload");
  LOAD(launch,"cuLaunchKernel");LOAD(synchronize,"cuStreamSynchronize");LOAD(hostAlloc,"cuMemHostAlloc");LOAD(hostFree,"cuMemFreeHost");LOAD(copy2DAsync,"cuMemcpy2DAsync_v2");LOAD(errorString,"cuGetErrorString");
#undef LOAD
  int status=init(0);available=status==0;if(available)availability="CUDA driver available";else{const char *reason=nullptr;errorString(status,&reason);availability=reason?reason:"CUDA initialization failed";}
 }
 static CudaApi &get(){static CudaApi api;return api;}
 void check(int status,const char *action){if(status){const char *reason=nullptr;if(errorString)errorString(status,&reason);throw std::runtime_error(std::string(action)+": "+(reason?reason:std::to_string(status)));}}
};
struct CudaScope {
 CudaApi &api;bool pushed=false;
 explicit CudaScope(CudaPointer source,CudaStream stream=nullptr):api(CudaApi::get()){
  if(!api.available)throw std::runtime_error("CUDA driver unavailable");
  CudaContext target=nullptr,current=nullptr;int pointerStatus=api.pointerAttribute(&target,1,source);
  if(stream&&api.streamContext){CudaContext queued=nullptr;api.check(api.streamContext(stream,&queued),"CUDA stream context");if(pointerStatus==0&&target!=queued)throw std::runtime_error("CUDA source/stream context mismatch");target=queued;}
  else api.check(pointerStatus,"CUDA source context");
  if(!target)throw std::runtime_error("Missing CUDA source context");
  api.check(api.getCurrent(&current),"CUDA current context");if(current!=target){api.check(api.pushCurrent(target),"CUDA context push");pushed=true;}
 }
 ~CudaScope(){if(pushed){CudaContext unused=nullptr;api.popCurrent(&unused);}}
};
struct CudaRender {
 struct Module {
  CudaContext context=nullptr;CudaModule module=nullptr;CudaFunction kernel=nullptr;
  ~Module(){auto &api=CudaApi::get();if(!module||!api.available)return;CudaContext unused=nullptr;
   if(api.pushCurrent(context)==0){api.unloadModule(module);api.popCurrent(&unused);}}
 };
 std::mutex mutex;std::map<CudaContext,std::shared_ptr<Module>> modules;std::map<CudaContext,std::string> failures;uint64_t moduleBuilds=0;
 std::shared_ptr<Module> moduleForCurrent(){
  auto &api=CudaApi::get();CudaContext context=nullptr;api.check(api.getCurrent(&context),"CUDA context");
  std::lock_guard<std::mutex> lock(mutex);auto found=modules.find(context);if(found!=modules.end())return found->second;
  auto failed=failures.find(context);if(failed!=failures.end())throw std::runtime_error(failed->second);
  auto entry=std::make_shared<Module>();entry->context=context;
  try{++moduleBuilds;api.check(api.loadModule(&entry->module,cudaPTX),"CUDA PTX JIT (CUDA12 driver required)");
   api.check(api.getFunction(&entry->kernel,entry->module,"nss_render_cuda"),"CUDA kernel");modules[context]=entry;return entry;}
  catch(const std::exception &x){failures[context]=x.what();throw;}
 }
 std::vector<float> readPatch(void *stream,void *input,const int *bounds,int stride,const int *rect){
  if(stride<=0)throw std::runtime_error("Unsupported CUDA pick stride");
  CudaScope scope(uint64_t(reinterpret_cast<uintptr_t>(input)),stream);auto &api=scope.api;
  size_t width=size_t(rect[2]-rect[0])*16,height=size_t(rect[3]-rect[1]);void *pinned=nullptr;
  api.check(api.hostAlloc(&pinned,width*height,0),"CUDA patch allocation");
  struct Pinned {CudaApi &api;void *p;~Pinned(){api.hostFree(p);}} owner{api,pinned};
  CudaCopy2D copy{};copy.srcX=size_t(rect[0]-bounds[0])*16;copy.srcY=size_t(rect[1]-bounds[1]);copy.srcType=2;
  copy.srcDevice=uint64_t(reinterpret_cast<uintptr_t>(input));copy.srcPitch=size_t(stride);copy.dstType=1;copy.dstHost=pinned;copy.dstPitch=width;copy.width=width;copy.height=height;
  api.check(api.copy2DAsync(&copy,stream),"CUDA patch copy");api.check(api.synchronize(stream),"CUDA patch ready");
  auto first=static_cast<float*>(pinned);return std::vector<float>(first,first+width*height/sizeof(float));
 }
 void enqueue(void *stream,bool streamProvided,void *input,void *output,const int *src,const int *dst,int srcStride,int dstStride,const int *window,bool premult,const Prepared &p){
  if(!input||!output||srcStride%4||dstStride%4)throw std::runtime_error("Unsupported CUDA layout");
  CudaPointer source=uint64_t(reinterpret_cast<uintptr_t>(input)),dest=uint64_t(reinterpret_cast<uintptr_t>(output));CudaScope scope(source,stream);auto entry=moduleForCurrent();
  int sourcePitch=srcStride/4,destPitch=dstStride/4,w=window[2]-window[0],h=window[3]-window[1],premultiplied=int(premult);
  if(w<=0||h<=0)return;
  void *args[]={&source,&dest,const_cast<float*>(p.values.data()),const_cast<int*>(src),const_cast<int*>(src+1),const_cast<int*>(src+2),const_cast<int*>(src+3),&sourcePitch,
   const_cast<int*>(dst),const_cast<int*>(dst+1),const_cast<int*>(dst+2),const_cast<int*>(dst+3),&destPitch,const_cast<int*>(window),const_cast<int*>(window+1),&w,&h,&premultiplied};
  scope.api.check(scope.api.launch(entry->kernel,unsigned((w+15)/16),unsigned((h+15)/16),1,16,16,1,0,stream,args,nullptr),"CUDA launch");
  // OFX: supplied stream -> enqueue only. No stream property -> finish this stream.
  if(!streamProvided)scope.api.check(scope.api.synchronize(stream),"CUDA default stream");
 }
};
}
#undef NSS_CUDA_CALL
