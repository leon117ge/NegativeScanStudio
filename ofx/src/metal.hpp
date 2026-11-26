#pragma once
// Metal render path interface (macOS). Plain C++ so plugin.cpp stays one C++ translation unit.
// The Objective-C++ implementation lives in metal_render.mm and is linked only into macOS builds
// compiled with -DNSS_METAL. Every other build (Windows, Linux, macOS CPU-only) sees an
// unavailable API, so the Metal property is declared "false" and render never takes this path.
#include <atomic>
#include <cstddef>
#include <memory>
#include <mutex>
#include <string>
namespace nss {
// Kernel argument block. Same layout as NssArgs in render.metal. Strides are in floats.
struct MetalArgs {int sx1,sy1,sx2,sy2,sourceStride,dx1,dy1,dx2,dy2,destStride,wx1,wy1,width,height,premult;};
static_assert(sizeof(MetalArgs)==15*sizeof(int),"MetalArgs must match NssArgs in render.metal");
// Per-instance status. Completion handlers run on a Metal thread after render returned, so an
// asynchronous command-buffer error is recorded here and the next render falls back to the CPU.
struct MetalStatus {
 std::atomic<int> failures{0};
 void fail(const std::string &text){{std::lock_guard<std::mutex> lock(mutex);last=text;}failures.fetch_add(1);}
 std::string error(){std::lock_guard<std::mutex> lock(mutex);return last;}
private:
 std::mutex mutex;std::string last;
};
// Test-only fault injection for the forced-failure tests. Production code never sets it.
enum MetalFault:int {kMetalFaultNone=0,kMetalFaultNoDevice=1,kMetalFaultCompile=2,kMetalFaultPipeline=3,kMetalFaultEncode=4};
struct MetalApi {
 bool available=false;
 std::string availability="Metal: not built in (Windows/Linux or macOS CPU-only build)";
 // Encode one dispatch on the host's command queue with the host's MTLBuffers and commit it
 // WITHOUT waiting; the host orders its later work on the same queue. Returns false with a
 // reason when nothing was committed (no device, compile/pipeline error, bad layout, encode error).
 bool (*encode)(void *queue,void *source,void *dest,const float *params,size_t count,const MetalArgs &args,
  const std::shared_ptr<MetalStatus> &status,std::string &device,std::string &error)=nullptr;
 // CPU address of a shared-storage (unified memory) MTLBuffer, or nullptr for GPU-private storage.
 void *(*contents)(void *buffer)=nullptr;
 // Wait for work already committed to the host queue. Used only by the CPU fallback and the
 // optional viewer gray picker, never by the normal Metal render.
 bool (*drain)(void *queue)=nullptr;
 std::atomic<int> fault{kMetalFaultNone};
 std::atomic<int> pipelineBuilds{0};
 static MetalApi &get();
};
#if defined(__APPLE__) && defined(NSS_METAL)
void metalInstall(MetalApi &api); // metal_render.mm; must not throw
inline MetalApi &MetalApi::get(){static MetalApi api;static std::once_flag once;std::call_once(once,[]{try{metalInstall(api);}catch(...){api.available=false;api.availability="Metal: initialisation failed";}});return api;}
#else
inline MetalApi &MetalApi::get(){static MetalApi api;return api;}
#endif
}
