#pragma once
// Build-time platform switches for the render backends. Every value can be
// overridden with -D on the compiler command line (tests do this).
//
// Windows and Linux keep CUDA and OpenCL; both drivers are loaded at run time,
// so the plugin still loads on machines without them. macOS has neither
// driver: there the GPU backend is Metal (added separately), and the CPU path
// is the reference and the fallback.
#ifndef NSS_WITH_CUDA
#ifdef __APPLE__
#define NSS_WITH_CUDA 0
#else
#define NSS_WITH_CUDA 1
#endif
#endif
#ifndef NSS_WITH_OPENCL
#ifdef __APPLE__
#define NSS_WITH_OPENCL 0
#else
#define NSS_WITH_OPENCL 1
#endif
#endif
// CPU worker cap. 0 means no fixed cap: use the hardware thread count reported
// by the OS at run time (all performance and efficiency cores on Apple Silicon).
// Windows and Linux keep the shipped cap of 8.
#ifndef NSS_CPU_THREAD_CAP
#ifdef __APPLE__
#define NSS_CPU_THREAD_CAP 0
#else
#define NSS_CPU_THREAD_CAP 8
#endif
#endif
// Render engine choice shown to the user. The Windows text is unchanged.
#ifndef NSS_RENDER_DEVICE_OPTIONS
#if NSS_WITH_CUDA || NSS_WITH_OPENCL
#define NSS_RENDER_DEVICE_OPTIONS "Auto (CUDA/OpenCL if offered)|CPU"
#else
#define NSS_RENDER_DEVICE_OPTIONS "Auto (GPU if offered)|CPU"
#endif
#endif
