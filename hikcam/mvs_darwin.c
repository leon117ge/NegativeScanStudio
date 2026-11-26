//go:build darwin && cgo

/* Runtime loader for the Hikrobot MVS SDK on macOS
   (/Library/Frameworks/MvCameraControl.framework, universal arm64 + x86_64).
   No link-time dependency: dlopen / dlsym only, so the app starts and lists
   the camera as unavailable when the SDK is not installed.

   Same C API as mvs_windows.c (declared in mvs_windows.h). The struct copies
   and the hik_* bodies below mirror mvs_windows.c; only the loader differs:
   - dlopen/dlsym instead of LoadLibrary/GetProcAddress,
   - no __stdcall (the macOS SDK uses the default C calling convention),
   - MV_CC_Initialize is called once when the SDK exports it (SDK 4.x).
   Set NSS_MVS_LIB to a full path to load a different library (CI stub). */

#include "mvs_windows.h"

#include <dlfcn.h>
#include <stdint.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>

#define MV_MAX_DEVICE_NUM 256
#define INFO_MAX_BUFFER_SIZE 64
#define MV_GIGE_DEVICE 0x00000001
#define MV_USB_DEVICE  0x00000004
#define MV_ACCESS_Exclusive 1

typedef struct {
    unsigned int nIpCfgOption;
    unsigned int nIpCfgCurrent;
    unsigned int nCurrentIp;
    unsigned int nCurrentSubNetMask;
    unsigned int nDefultGateWay;
    unsigned char chManufacturerName[32];
    unsigned char chModelName[32];
    unsigned char chDeviceVersion[32];
    unsigned char chManufacturerSpecificInfo[48];
    unsigned char chSerialNumber[16];
    unsigned char chUserDefinedName[16];
    unsigned int nNetExport;
    unsigned int nReserved[4];
} MV_GIGE_DEVICE_INFO;

typedef struct {
    unsigned char CrtlInEndPoint;
    unsigned char CrtlOutEndPoint;
    unsigned char StreamEndPoint;
    unsigned char EventEndPoint;
    unsigned short idVendor;
    unsigned short idProduct;
    unsigned int nDeviceNumber;
    unsigned char chDeviceGUID[INFO_MAX_BUFFER_SIZE];
    unsigned char chVendorName[INFO_MAX_BUFFER_SIZE];
    unsigned char chModelName[INFO_MAX_BUFFER_SIZE];
    unsigned char chFamilyName[INFO_MAX_BUFFER_SIZE];
    unsigned char chDeviceVersion[INFO_MAX_BUFFER_SIZE];
    unsigned char chManufacturerName[INFO_MAX_BUFFER_SIZE];
    unsigned char chSerialNumber[INFO_MAX_BUFFER_SIZE];
    unsigned char chUserDefinedName[INFO_MAX_BUFFER_SIZE];
    unsigned int nbcdUSB;
    unsigned int nDeviceAddress;
    unsigned int nReserved[2];
} MV_USB3_DEVICE_INFO;

typedef struct {
    unsigned short nMajorVer;
    unsigned short nMinorVer;
    unsigned int nMacAddrHigh;
    unsigned int nMacAddrLow;
    unsigned int nTLayerType;
    unsigned int nReserved[4];
    union {
        MV_GIGE_DEVICE_INFO stGigEInfo;
        MV_USB3_DEVICE_INFO stUsb3VInfo;
    } SpecialInfo;
} MV_CC_DEVICE_INFO;

typedef struct {
    unsigned int nDeviceNum;
    MV_CC_DEVICE_INFO *pDeviceInfo[MV_MAX_DEVICE_NUM];
} MV_CC_DEVICE_INFO_LIST;

typedef struct {
    float fCurValue;
    float fMax;
    float fMin;
    unsigned int nReserved[4];
} MVCC_FLOATVALUE;

typedef struct {
    unsigned int nCurValue;
    unsigned int nMax;
    unsigned int nMin;
    unsigned int nInc;
    unsigned int nReserved[4];
} MVCC_INTVALUE;

/* Newer SDKs expose 64-bit integer nodes. */
typedef struct {
    int64_t nCurValue;
    int64_t nMax;
    int64_t nMin;
    int64_t nInc;
    unsigned int nReserved[16];
} MVCC_INTVALUE_EX;

/* MV_FRAME_OUT. Differs from mvs_windows.c on purpose: enPixelType is
   "enum MvGvspPixelType", which MSVC/MinGW keep at 32 bits but clang on
   macOS (LP64) widens to 64 bits because the enum's values do not fit an
   int. Checked against the MVS SDK 4.7 macOS headers: enPixelType at 16,
   nFrameNum at 24, nHostTimeStamp at 40, nFrameLen at 48 (arm64 and x86_64).
   Pixel type values all fit in 32 bits, so hik_grab reports the low half. */
typedef struct {
    unsigned char *pBufAddr;
    unsigned short nWidth;
    unsigned short nHeight;
    uint64_t enPixelType;
    unsigned int nFrameNum;
    unsigned int nDevTimeStampHigh;
    unsigned int nDevTimeStampLow;
    unsigned int nReserved0;
    int64_t nHostTimeStamp;
    unsigned int nFrameLen;
    unsigned char pad[1024];
} hik_frame_out;

typedef int (*FN_EnumDevices)(unsigned int, MV_CC_DEVICE_INFO_LIST *);
typedef int (*FN_CreateHandle)(void **, const MV_CC_DEVICE_INFO *);
typedef int (*FN_DestroyHandle)(void *);
typedef int (*FN_OpenDevice)(void *, unsigned int, unsigned short);
typedef int (*FN_CloseDevice)(void *);
typedef int (*FN_StartGrabbing)(void *);
typedef int (*FN_StopGrabbing)(void *);
typedef int (*FN_GetImageBuffer)(void *, void *, unsigned int);
typedef int (*FN_FreeImageBuffer)(void *, void *);
typedef int (*FN_SetEnumValueByString)(void *, const char *, const char *);
typedef int (*FN_SetFloatValue)(void *, const char *, float);
typedef int (*FN_GetFloatValue)(void *, const char *, MVCC_FLOATVALUE *);
typedef int (*FN_SetIntValue)(void *, const char *, unsigned int);
typedef int (*FN_GetIntValue)(void *, const char *, MVCC_INTVALUE *);
typedef int (*FN_SetIntValueEx)(void *, const char *, int64_t);
typedef int (*FN_GetIntValueEx)(void *, const char *, MVCC_INTVALUE_EX *);
typedef int (*FN_SetBoolValue)(void *, const char *, unsigned char);
typedef int (*FN_SetCommandValue)(void *, const char *);
typedef int (*FN_ClearImageBuffer)(void *);

typedef int (*FN_Initialize)(void);

static void *hMVS;
static char loadErr[256];
static MV_CC_DEVICE_INFO_LIST gList;
static hik_frame_out gFrame;
static int gHaveFrame;

static FN_EnumDevices pEnumDevices;
static FN_CreateHandle pCreateHandle;
static FN_DestroyHandle pDestroyHandle;
static FN_OpenDevice pOpenDevice;
static FN_CloseDevice pCloseDevice;
static FN_StartGrabbing pStartGrabbing;
static FN_StopGrabbing pStopGrabbing;
static FN_GetImageBuffer pGetImageBuffer;
static FN_FreeImageBuffer pFreeImageBuffer;
static FN_SetEnumValueByString pSetEnumValueByString;
static FN_SetFloatValue pSetFloatValue;
static FN_GetFloatValue pGetFloatValue;
static FN_SetIntValue pSetIntValue;
static FN_GetIntValue pGetIntValue;
static FN_SetIntValueEx pSetIntValueEx;
static FN_GetIntValueEx pGetIntValueEx;
static FN_SetBoolValue pSetBoolValue;
static FN_SetCommandValue pSetCommandValue;
static FN_ClearImageBuffer pClearImageBuffer;

static int hik_bind(const char *name, void **fn) {
    *fn = dlsym(hMVS, name);
    return *fn != NULL;
}

static void copy_c(char *dst, int n, const unsigned char *src, int srcn) {
    int i = 0;
    if (n <= 0) {
        return;
    }
    if (srcn > n - 1) {
        srcn = n - 1;
    }
    for (; i < srcn && src[i]; i++) {
        dst[i] = (char)src[i];
    }
    dst[i] = 0;
}

int hik_load(void) {
    static const char *paths[] = {
        "/Library/Frameworks/MvCameraControl.framework/MvCameraControl",
        "/Library/Frameworks/MvCameraControl.framework/Versions/A/MvCameraControl",
        NULL
    };
    const char *override = getenv("NSS_MVS_LIB");
    FN_Initialize pInitialize = NULL;
    int i;
    if (hMVS) {
        return 0;
    }
    loadErr[0] = 0;
    if (override && override[0]) {
        hMVS = dlopen(override, RTLD_NOW | RTLD_LOCAL);
        if (!hMVS) {
            const char *why = dlerror();
            snprintf(loadErr, sizeof(loadErr),
                     "NSS_MVS_LIB=%s could not be loaded: %s", override, why ? why : "unknown error");
            return -1;
        }
    } else {
        for (i = 0; paths[i]; i++) {
            hMVS = dlopen(paths[i], RTLD_NOW | RTLD_LOCAL);
            if (hMVS) {
                break;
            }
        }
        if (!hMVS) {
            snprintf(loadErr, sizeof(loadErr),
                     "MvCameraControl.framework not found (install the Hikrobot MVS or MVS SDK Runtime for macOS)");
            return -1;
        }
    }
    if (!hik_bind("MV_CC_EnumDevices", (void **)&pEnumDevices) ||
        !hik_bind("MV_CC_CreateHandle", (void **)&pCreateHandle) ||
        !hik_bind("MV_CC_DestroyHandle", (void **)&pDestroyHandle) ||
        !hik_bind("MV_CC_OpenDevice", (void **)&pOpenDevice) ||
        !hik_bind("MV_CC_CloseDevice", (void **)&pCloseDevice) ||
        !hik_bind("MV_CC_StartGrabbing", (void **)&pStartGrabbing) ||
        !hik_bind("MV_CC_StopGrabbing", (void **)&pStopGrabbing) ||
        !hik_bind("MV_CC_GetImageBuffer", (void **)&pGetImageBuffer) ||
        !hik_bind("MV_CC_FreeImageBuffer", (void **)&pFreeImageBuffer) ||
        !hik_bind("MV_CC_SetEnumValueByString", (void **)&pSetEnumValueByString) ||
        !hik_bind("MV_CC_SetFloatValue", (void **)&pSetFloatValue) ||
        !hik_bind("MV_CC_GetFloatValue", (void **)&pGetFloatValue) ||
        !hik_bind("MV_CC_SetBoolValue", (void **)&pSetBoolValue) ||
        !hik_bind("MV_CC_SetCommandValue", (void **)&pSetCommandValue)) {
        dlclose(hMVS);
        hMVS = NULL;
        snprintf(loadErr, sizeof(loadErr),
                 "MvCameraControl is missing required functions");
        return -1;
    }
    /* Integer setters exist as 32-bit and/or 64-bit variants depending on SDK. */
    hik_bind("MV_CC_SetIntValue", (void **)&pSetIntValue);
    hik_bind("MV_CC_GetIntValue", (void **)&pGetIntValue);
    hik_bind("MV_CC_SetIntValueEx", (void **)&pSetIntValueEx);
    hik_bind("MV_CC_GetIntValueEx", (void **)&pGetIntValueEx);
    hik_bind("MV_CC_ClearImageBuffer", (void **)&pClearImageBuffer);
    if (!pSetIntValue && !pSetIntValueEx) {
        dlclose(hMVS);
        hMVS = NULL;
        snprintf(loadErr, sizeof(loadErr),
                 "MvCameraControl has no MV_CC_SetIntValue");
        return -1;
    }
    /* SDK 4.x wants MV_CC_Initialize before any other call; older SDKs lack it. */
    if (hik_bind("MV_CC_Initialize", (void **)&pInitialize) && pInitialize) {
        int ret = pInitialize();
        if (ret != 0) {
            dlclose(hMVS);
            hMVS = NULL;
            snprintf(loadErr, sizeof(loadErr),
                     "MV_CC_Initialize returned 0x%08X", (unsigned)ret);
            return -1;
        }
    }
    return 0;
}

int hik_available(void) { return hMVS != NULL; }

const char *hik_load_error(void) { return loadErr; }

int hik_enum(unsigned int layer, char *err, int errn) {
    int ret;
    if (!hMVS && hik_load() != 0) {
        if (err && errn > 0) {
            snprintf(err, errn, "%s", loadErr);
        }
        return -1;
    }
    memset(&gList, 0, sizeof(gList));
    ret = pEnumDevices(layer, &gList);
    if (ret != 0) {
        if (err && errn > 0) {
            snprintf(err, errn, "MV_CC_EnumDevices returned 0x%08X", (unsigned)ret);
        }
        return ret;
    }
    return 0;
}

int hik_count(void) { return (int)gList.nDeviceNum; }

int hik_dev_info(int index, char *model, int modeln, char *serial, int serialn,
                 char *vendor, int vendorn, char *iface, int ifacen) {
    MV_CC_DEVICE_INFO *d;
    if (index < 0 || index >= (int)gList.nDeviceNum || !gList.pDeviceInfo[index]) {
        return -1;
    }
    d = gList.pDeviceInfo[index];
    if (d->nTLayerType & MV_USB_DEVICE) {
        copy_c(model, modeln, d->SpecialInfo.stUsb3VInfo.chModelName, INFO_MAX_BUFFER_SIZE);
        copy_c(serial, serialn, d->SpecialInfo.stUsb3VInfo.chSerialNumber, INFO_MAX_BUFFER_SIZE);
        copy_c(vendor, vendorn, d->SpecialInfo.stUsb3VInfo.chVendorName, INFO_MAX_BUFFER_SIZE);
        copy_c(iface, ifacen, (const unsigned char *)"USB3", 4);
    } else if (d->nTLayerType & MV_GIGE_DEVICE) {
        copy_c(model, modeln, d->SpecialInfo.stGigEInfo.chModelName, 32);
        copy_c(serial, serialn, d->SpecialInfo.stGigEInfo.chSerialNumber, 16);
        copy_c(vendor, vendorn, d->SpecialInfo.stGigEInfo.chManufacturerName, 32);
        copy_c(iface, ifacen, (const unsigned char *)"GigE", 4);
    } else {
        copy_c(model, modeln, (const unsigned char *)"Unknown", 7);
        copy_c(serial, serialn, (const unsigned char *)"", 0);
        copy_c(vendor, vendorn, (const unsigned char *)"Hikrobot", 8);
        copy_c(iface, ifacen, (const unsigned char *)"Unknown", 7);
    }
    return 0;
}

int hik_open(int index, void **handle, char *err, int errn) {
    int ret;
    void *h = NULL;
    if (index < 0 || index >= (int)gList.nDeviceNum || !gList.pDeviceInfo[index]) {
        if (err && errn > 0) {
            snprintf(err, errn, "camera index %d is out of range", index);
        }
        return -1;
    }
    ret = pCreateHandle(&h, gList.pDeviceInfo[index]);
    if (ret != 0) {
        if (err && errn > 0) {
            snprintf(err, errn, "MV_CC_CreateHandle 0x%08X", (unsigned)ret);
        }
        return ret;
    }
    ret = pOpenDevice(h, MV_ACCESS_Exclusive, 0);
    if (ret != 0) {
        pDestroyHandle(h);
        if (err && errn > 0) {
            snprintf(err, errn, "MV_CC_OpenDevice 0x%08X", (unsigned)ret);
        }
        return ret;
    }
    *handle = h;
    return 0;
}

int hik_close(void *handle) {
    if (!handle) {
        return 0;
    }
    pCloseDevice(handle);
    return pDestroyHandle(handle);
}

int hik_set_enum_str(void *handle, const char *key, const char *val) {
    return pSetEnumValueByString(handle, key, val);
}

int hik_set_float(void *handle, const char *key, float val) {
    return pSetFloatValue(handle, key, val);
}

int hik_get_float(void *handle, const char *key, float *cur, float *minv, float *maxv) {
    MVCC_FLOATVALUE v;
    int ret;
    memset(&v, 0, sizeof(v));
    ret = pGetFloatValue(handle, key, &v);
    if (ret == 0) {
        if (cur) *cur = v.fCurValue;
        if (minv) *minv = v.fMin;
        if (maxv) *maxv = v.fMax;
    }
    return ret;
}

int hik_set_int(void *handle, const char *key, long long val) {
    if (pSetIntValueEx) {
        return pSetIntValueEx(handle, key, (int64_t)val);
    }
    return pSetIntValue(handle, key, (unsigned int)val);
}

int hik_get_int(void *handle, const char *key, long long *cur, long long *minv, long long *maxv) {
    int ret;
    if (pGetIntValueEx) {
        MVCC_INTVALUE_EX v;
        memset(&v, 0, sizeof(v));
        ret = pGetIntValueEx(handle, key, &v);
        if (ret == 0) {
            if (cur) *cur = (long long)v.nCurValue;
            if (minv) *minv = (long long)v.nMin;
            if (maxv) *maxv = (long long)v.nMax;
        }
        return ret;
    }
    if (pGetIntValue) {
        MVCC_INTVALUE v;
        memset(&v, 0, sizeof(v));
        ret = pGetIntValue(handle, key, &v);
        if (ret == 0) {
            if (cur) *cur = (long long)v.nCurValue;
            if (minv) *minv = (long long)v.nMin;
            if (maxv) *maxv = (long long)v.nMax;
        }
        return ret;
    }
    return -1;
}

int hik_set_bool(void *handle, const char *key, int val) {
    return pSetBoolValue(handle, key, val ? 1 : 0);
}

int hik_command(void *handle, const char *key) {
    return pSetCommandValue(handle, key);
}

int hik_start(void *handle) { return pStartGrabbing(handle); }

int hik_stop(void *handle) { return pStopGrabbing(handle); }

int hik_grab(void *handle, unsigned int timeout_ms,
             unsigned char **data, int *w, int *h,
             unsigned int *pixfmt, unsigned int *len) {
    int ret;
    if (gHaveFrame) {
        pFreeImageBuffer(handle, &gFrame);
        gHaveFrame = 0;
    }
    memset(&gFrame, 0, sizeof(gFrame));
    ret = pGetImageBuffer(handle, &gFrame, timeout_ms);
    if (ret != 0) {
        return ret;
    }
    gHaveFrame = 1;
    if (data) *data = gFrame.pBufAddr;
    if (w) *w = (int)gFrame.nWidth;
    if (h) *h = (int)gFrame.nHeight;
    if (pixfmt) *pixfmt = (unsigned int)gFrame.enPixelType;
    if (len) *len = gFrame.nFrameLen;
    return 0;
}

int hik_grab_free(void *handle) {
    if (!gHaveFrame) {
        return 0;
    }
    gHaveFrame = 0;
    return pFreeImageBuffer(handle, &gFrame);
}

int hik_clear(void *handle) {
    if (!pClearImageBuffer) {
        return 0;
    }
    return pClearImageBuffer(handle);
}

