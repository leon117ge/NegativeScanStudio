// Test-only helpers for metal_plugin_test.cpp: a device, a queue and buffers, handed to C++ as
// retained void* (the same form Resolve uses for kOfxImagePropData and the Metal command queue).
#import <Foundation/Foundation.h>
#import <Metal/Metal.h>
#include <cstddef>
extern "C" void *nssTestQueue(){id<MTLDevice> device=MTLCreateSystemDefaultDevice();if(!device)return nullptr;id<MTLCommandQueue> queue=[device newCommandQueue];return queue?(void*)CFBridgingRetain(queue):nullptr;}
extern "C" void *nssTestBuffer(void *queue,size_t bytes,int shared){id<MTLCommandQueue> q=(__bridge id<MTLCommandQueue>)queue;
 id<MTLBuffer> buffer=[q.device newBufferWithLength:bytes options:shared?MTLResourceStorageModeShared:MTLResourceStorageModePrivate];return buffer?(void*)CFBridgingRetain(buffer):nullptr;}
extern "C" void *nssTestContents(void *buffer){id<MTLBuffer> b=(__bridge id<MTLBuffer>)buffer;return b.storageMode==MTLStorageModeShared?b.contents:nullptr;}
extern "C" void nssTestRelease(void *object){if(object)CFBridgingRelease(object);}
extern "C" const char *nssTestDeviceName(void *queue){id<MTLCommandQueue> q=(__bridge id<MTLCommandQueue>)queue;return q.device.name.UTF8String;}
extern "C" int nssTestUnifiedMemory(void *queue){id<MTLCommandQueue> q=(__bridge id<MTLCommandQueue>)queue;return q.device.hasUnifiedMemory?1:0;}
// The test (not the plugin) waits, so it can read the result.
extern "C" int nssTestWait(void *queue){@autoreleasepool{id<MTLCommandQueue> q=(__bridge id<MTLCommandQueue>)queue;id<MTLCommandBuffer> c=[q commandBuffer];[c commit];[c waitUntilCompleted];return c.status==MTLCommandBufferStatusCompleted?1:0;}}
