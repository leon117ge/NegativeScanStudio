// Optional developer check against installed official CUDA headers. None are vendored.
#include <cuda.h>
#include "../src/cuda.hpp"
#include <cstddef>
static_assert(sizeof(nss::CudaCopy2D)==sizeof(CUDA_MEMCPY2D));
#define OFFSET(ours,sdk) static_assert(offsetof(nss::CudaCopy2D,ours)==offsetof(CUDA_MEMCPY2D,sdk))
OFFSET(srcX,srcXInBytes);OFFSET(srcY,srcY);OFFSET(srcType,srcMemoryType);OFFSET(srcHost,srcHost);OFFSET(srcDevice,srcDevice);OFFSET(srcArray,srcArray);OFFSET(srcPitch,srcPitch);
OFFSET(dstX,dstXInBytes);OFFSET(dstY,dstY);OFFSET(dstType,dstMemoryType);OFFSET(dstHost,dstHost);OFFSET(dstDevice,dstDevice);OFFSET(dstArray,dstArray);OFFSET(dstPitch,dstPitch);OFFSET(width,WidthInBytes);OFFSET(height,Height);
static_assert(sizeof(nss::CudaPointer)==sizeof(CUdeviceptr));static_assert(sizeof(CUmemorytype)==sizeof(int));
int main(){}
