#pragma once
#define CL_TARGET_OPENCL_VERSION 120
#include "../vendor/opencl/CL/cl.h"
#include "opencl_source.hpp"
#include "prepared.hpp"
#include <mutex>
#include <string>
#include <cstring>
#include <vector>
#include <stdexcept>
#ifdef _WIN32
#ifndef NOMINMAX
#define NOMINMAX
#endif
#include <windows.h>
#else
#include <dlfcn.h>
#endif
namespace nss {
struct CLApi {
#define NSS_CL_FUNCTIONS(X) \
 X(clGetCommandQueueInfo) X(clCreateProgramWithSource) X(clBuildProgram) X(clGetProgramBuildInfo) X(clReleaseProgram) \
 X(clEnqueueReadBufferRect) X(clCreateKernel) X(clReleaseKernel) X(clSetKernelArg) X(clCreateBuffer) X(clReleaseMemObject) X(clEnqueueNDRangeKernel)
#define DECLARE(name) decltype(&::name) name=nullptr;
 NSS_CL_FUNCTIONS(DECLARE)
#undef DECLARE
 bool available=false;
 CLApi(){
#ifdef _WIN32
  auto module=LoadLibraryExW(L"OpenCL.dll",nullptr,LOAD_LIBRARY_SEARCH_SYSTEM32);
  auto symbol=[&](const char *name){return module?GetProcAddress(module,name):nullptr;};
#else
  void *module=dlopen("libOpenCL.so.1",RTLD_NOW|RTLD_LOCAL);
  auto symbol=[&](const char *name){return module?dlsym(module,name):nullptr;};
#endif
  if(!module)return;
#define LOAD(name) {auto address=symbol(#name);static_assert(sizeof(address)==sizeof(name));std::memcpy(&name,&address,sizeof(name));if(!name)return;}
 NSS_CL_FUNCTIONS(LOAD)
#undef LOAD
  available=true;
 }
 static CLApi &get(){static CLApi a;return a;}
};
struct CLRender {
 std::mutex mutex;cl_context context=nullptr;cl_device_id device=nullptr;cl_program program=nullptr;std::string buildError;
 ~CLRender(){if(program)CLApi::get().clReleaseProgram(program);}
 // Optional viewer-pick prepass only: download a small rectangle, never a full frame.
 std::vector<float> readPatch(void *hostQueue,void *input,const int *bounds,int stride,const int *rect){
  auto &api=CLApi::get();if(!api.available||!hostQueue||!input||stride<=0)throw std::runtime_error("Cannot sample OpenCL source");
  size_t width=size_t(rect[2]-rect[0]),height=size_t(rect[3]-rect[1]);std::vector<float> pixels(width*height*4);
  size_t origin[]={size_t(rect[0]-bounds[0])*4*sizeof(float),size_t(rect[1]-bounds[1]),0},zero[]={0,0,0},region[]={width*4*sizeof(float),height,1};
  auto status=api.clEnqueueReadBufferRect(reinterpret_cast<cl_command_queue>(hostQueue),reinterpret_cast<cl_mem>(input),CL_TRUE,origin,zero,region,size_t(stride),0,width*4*sizeof(float),0,pixels.data(),0,nullptr,nullptr);
  if(status!=CL_SUCCESS)throw std::runtime_error("OpenCL pick read failed "+std::to_string(status));
  return pixels;
 }
 void enqueue(void *hostQueue,void *input,void *output,const int *src,const int *dst,int sourceStride,int destStride,const int *window,bool premult,const Prepared &p){
  auto &api=CLApi::get();if(!api.available)throw std::runtime_error("OpenCL runtime unavailable");
  if(!hostQueue||!input||!output||sourceStride<=0||destStride<=0||sourceStride%4||destStride%4)throw std::runtime_error("Unsupported OpenCL buffer layout");
  auto queue=reinterpret_cast<cl_command_queue>(hostQueue);cl_context ctx=nullptr;cl_device_id dev=nullptr;
  auto ok=[](cl_int r){if(r!=CL_SUCCESS)throw std::runtime_error("OpenCL error "+std::to_string(r));};
  ok(api.clGetCommandQueueInfo(queue,CL_QUEUE_CONTEXT,sizeof(ctx),&ctx,nullptr));ok(api.clGetCommandQueueInfo(queue,CL_QUEUE_DEVICE,sizeof(dev),&dev,nullptr));
  std::lock_guard<std::mutex> guard(mutex);
  if(ctx!=context||dev!=device){if(program)api.clReleaseProgram(program);program=nullptr;context=ctx;device=dev;buildError.clear();}
  if(!buildError.empty())throw std::runtime_error(buildError);
  if(!program){
   cl_int error;const char *code=openclSource;program=api.clCreateProgramWithSource(ctx,1,&code,nullptr,&error);ok(error);
   error=api.clBuildProgram(program,1,&dev,"-cl-std=CL1.2",nullptr,nullptr);
   if(error!=CL_SUCCESS){size_t n=0;api.clGetProgramBuildInfo(program,dev,CL_PROGRAM_BUILD_LOG,0,nullptr,&n);std::string log(n,'\0');if(n)api.clGetProgramBuildInfo(program,dev,CL_PROGRAM_BUILD_LOG,n,log.data(),nullptr);buildError="OpenCL compile failed "+std::to_string(error)+": "+log;api.clReleaseProgram(program);program=nullptr;throw std::runtime_error(buildError);}
  }
  cl_int error;cl_kernel kernel=api.clCreateKernel(program,"nss_render",&error);ok(error);
  struct Kernel {CLApi &a;cl_kernel k;~Kernel(){a.clReleaseKernel(k);}} owner{api,kernel};
  cl_mem constants=api.clCreateBuffer(ctx,CL_MEM_READ_ONLY|CL_MEM_COPY_HOST_PTR,sizeof(p.values),const_cast<float*>(p.values.data()),&error);ok(error);
  struct Memory {CLApi&a;cl_mem m;~Memory(){a.clReleaseMemObject(m);}} memory{api,constants};
  cl_mem source=reinterpret_cast<cl_mem>(input),dest=reinterpret_cast<cl_mem>(output);int arg=0;
  auto set=[&](size_t size,const void *data){ok(api.clSetKernelArg(kernel,arg++,size,data));};
  set(sizeof(source),&source);set(sizeof(dest),&dest);set(sizeof(constants),&constants);
  for(int c=0;c<4;c++)set(sizeof(int),src+c);
  int stride=sourceStride/4;set(sizeof(int),&stride);
  for(int c=0;c<4;c++)set(sizeof(int),dst+c);
  stride=destStride/4;set(sizeof(int),&stride);
  int values[]={window[0],window[1],window[2]-window[0],window[3]-window[1],int(premult)};
  for(int v:values)set(sizeof(int),&v);
  if(values[2]<=0||values[3]<=0)return;
  size_t size[]={size_t(values[2]),size_t(values[3])};
  ok(api.clEnqueueNDRangeKernel(queue,kernel,2,nullptr,size,nullptr,0,nullptr,nullptr));
  // Host owns queue and buffers. Do not finish/wait or release host handles.
 }
};
}
