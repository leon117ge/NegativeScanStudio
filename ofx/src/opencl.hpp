#pragma once
#define CL_TARGET_OPENCL_VERSION 120
#include "../vendor/opencl/CL/cl.h"
#include "opencl_source.hpp"
#include "prepared.hpp"
#include <mutex>
#include <map>
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
 struct Program {
  cl_program program=nullptr;cl_kernel kernel=nullptr;std::string error;
  ~Program(){auto &api=CLApi::get();if(kernel)api.clReleaseKernel(kernel);if(program)api.clReleaseProgram(program);}
 };
 std::mutex mutex;std::map<std::pair<uintptr_t,uintptr_t>,std::unique_ptr<Program>> programs;
 uint64_t programBuilds=0,kernelBuilds=0;
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
  auto key=std::make_pair(reinterpret_cast<uintptr_t>(ctx),reinterpret_cast<uintptr_t>(dev));auto &cached=programs[key];if(!cached)cached=std::make_unique<Program>();auto &entry=*cached;
  if(!entry.error.empty())throw std::runtime_error(entry.error);
  if(!entry.program){
   cl_int status;const char *code=openclSource;entry.program=api.clCreateProgramWithSource(ctx,1,&code,nullptr,&status);ok(status);++programBuilds;
   status=api.clBuildProgram(entry.program,1,&dev,"-cl-std=CL1.2",nullptr,nullptr);
   if(status!=CL_SUCCESS){size_t n=0;api.clGetProgramBuildInfo(entry.program,dev,CL_PROGRAM_BUILD_LOG,0,nullptr,&n);std::string log(n,'\0');if(n)api.clGetProgramBuildInfo(entry.program,dev,CL_PROGRAM_BUILD_LOG,n,log.data(),nullptr);entry.error="OpenCL compile failed "+std::to_string(status)+": "+log;throw std::runtime_error(entry.error);}
  }
  cl_int error;if(!entry.kernel){entry.kernel=api.clCreateKernel(entry.program,"nss_render",&error);ok(error);++kernelBuilds;}
  auto kernel=entry.kernel;
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
