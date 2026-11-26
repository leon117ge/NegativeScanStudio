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
// Rendering controls: ONE mechanism for every platform and build.
// Choice indices never change: 0 = Auto, 1 = CPU, default 0 (saved projects keep their meaning).
// - Windows/Linux: hidden as in 0.90.14, NSS_RENDER_DEVICE_OPTIONS_HIDDEN
//   "Auto (CUDA/OpenCL if offered)|CPU".
// - macOS: visible (Metal vs CPU checks in the host), NSS_RENDER_DEVICE_OPTIONS. With the Metal
//   path (-DNSS_METAL) "Auto (Metal)|CPU"; a CPU-only macOS build shows "Auto|CPU" (both render
//   on the CPU).
// plugin.cpp picks the branch at run time with if(NSS_MAC_RENDER_UI), so the macOS strings are
// also compiled (unused) into Windows/Linux; they keep the Metal text there, which leaves the
// Windows translation unit identical to feat/metal-render.
// Tests that emulate one build on another OS set NSS_MAC_RENDER_UI / NSS_RENDER_AUTO_METAL.
#ifndef NSS_MAC_RENDER_UI
#if defined(__APPLE__)
#define NSS_MAC_RENDER_UI 1
#else
#define NSS_MAC_RENDER_UI 0
#endif
#endif
#ifndef NSS_RENDER_AUTO_METAL
#if defined(NSS_METAL)
#define NSS_RENDER_AUTO_METAL 1
#else
#define NSS_RENDER_AUTO_METAL 0
#endif
#endif
#define NSS_RENDER_DEVICE_OPTIONS_HIDDEN "Auto (CUDA/OpenCL if offered)|CPU"
#if NSS_RENDER_AUTO_METAL || !NSS_MAC_RENDER_UI
#define NSS_RENDER_DEVICE_OPTIONS "Auto (Metal)|CPU"
#define NSS_RENDER_DEVICE_HINT "Auto renders with Metal when the host offers it, otherwise on the CPU. CPU always renders on the CPU (for Metal vs CPU checks)."
#define NSS_RENDER_INFO_HINT "Shows the engine used by the last render (Metal with the GPU name, or CPU) and why."
#else
#define NSS_RENDER_DEVICE_OPTIONS "Auto|CPU"
#define NSS_RENDER_DEVICE_HINT "This build renders on the CPU only; Auto and CPU give the same result."
#define NSS_RENDER_INFO_HINT "Shows the engine used by the last render (CPU) and why."
#endif
