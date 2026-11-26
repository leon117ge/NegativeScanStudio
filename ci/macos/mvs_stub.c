/* Stand-in for Hikrobot's MvCameraControl, for CI and tests only.

   It is NOT the Hikrobot SDK and contains no Hikrobot code or headers: it
   exports the MV_CC_* functions that hikcam/mvs_darwin.c binds, with the
   struct layouts hikcam declares, and simulates one USB3 Vision camera that
   streams a known Mono8/10/12/16 ramp. Build it with ci/macos/build_mvs_stub.sh
   and point NSS_MVS_LIB at the result.

   Environment knobs (read at MV_CC_EnumDevices time):
     NSS_MVS_STUB_CAMERAS   number of cameras to report (default 1, max 4)
     NSS_MVS_STUB_FAIL_OPEN if set, MV_CC_OpenDevice fails with 0x80000203 */

#include <stdint.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>

#if defined(_WIN32)
#define STUB_API __declspec(dllexport)
#else
#define STUB_API __attribute__((visibility("default")))
#endif

#define MV_OK 0
#define MV_E_HANDLE 0x80000000
#define MV_E_SUPPORT 0x80000001
#define MV_E_CALLORDER 0x80000003
#define MV_E_PARAMETER 0x80000004
#define MV_E_NODATA 0x80000007
#define MV_E_ACCESS_DENIED 0x80000203
#define MV_E_GC_PROPERTY 0x80000106

#define MV_MAX_DEVICE_NUM 256
#define INFO_MAX_BUFFER_SIZE 64
#define MV_USB_DEVICE 0x00000004

typedef struct {
    unsigned int nIpCfgOption, nIpCfgCurrent, nCurrentIp, nCurrentSubNetMask, nDefultGateWay;
    unsigned char chManufacturerName[32], chModelName[32], chDeviceVersion[32];
    unsigned char chManufacturerSpecificInfo[48], chSerialNumber[16], chUserDefinedName[16];
    unsigned int nNetExport;
    unsigned int nReserved[4];
} STUB_GIGE_INFO;

typedef struct {
    unsigned char CrtlInEndPoint, CrtlOutEndPoint, StreamEndPoint, EventEndPoint;
    unsigned short idVendor, idProduct;
    unsigned int nDeviceNumber;
    unsigned char chDeviceGUID[INFO_MAX_BUFFER_SIZE], chVendorName[INFO_MAX_BUFFER_SIZE];
    unsigned char chModelName[INFO_MAX_BUFFER_SIZE], chFamilyName[INFO_MAX_BUFFER_SIZE];
    unsigned char chDeviceVersion[INFO_MAX_BUFFER_SIZE], chManufacturerName[INFO_MAX_BUFFER_SIZE];
    unsigned char chSerialNumber[INFO_MAX_BUFFER_SIZE], chUserDefinedName[INFO_MAX_BUFFER_SIZE];
    unsigned int nbcdUSB, nDeviceAddress;
    unsigned int nReserved[2];
} STUB_USB3_INFO;

typedef struct {
    unsigned short nMajorVer, nMinorVer;
    unsigned int nMacAddrHigh, nMacAddrLow, nTLayerType;
    unsigned int nReserved[4];
    union { STUB_GIGE_INFO stGigEInfo; STUB_USB3_INFO stUsb3VInfo; } SpecialInfo;
} STUB_DEVICE_INFO;

typedef struct {
    unsigned int nDeviceNum;
    STUB_DEVICE_INFO *pDeviceInfo[MV_MAX_DEVICE_NUM];
} STUB_DEVICE_INFO_LIST;

typedef struct { float fCurValue, fMax, fMin; unsigned int nReserved[4]; } STUB_FLOATVALUE;
typedef struct { int64_t nCurValue, nMax, nMin, nInc; unsigned int nReserved[16]; } STUB_INTVALUE_EX;

typedef struct {
    unsigned char *pBufAddr;
    unsigned short nWidth, nHeight;
    uint64_t enPixelType; /* enum MvGvspPixelType is 64-bit under clang on macOS */
    unsigned int nFrameNum, nDevTimeStampHigh, nDevTimeStampLow, nReserved0;
    int64_t nHostTimeStamp;
    unsigned int nFrameLen;
} STUB_FRAME_OUT;

#define STUB_MAX_W 64
#define STUB_MAX_H 48

typedef struct {
    int index, open, grabbing, triggerOn, triggerPending, outstanding;
    unsigned int pixfmt, frameNum;
    int64_t width, height, offX, offY;
    float exposure, gain, black;
    unsigned char *buf;
} stub_cam;

static STUB_DEVICE_INFO gDev[4];
static int gInit;

static void put(unsigned char *dst, size_t n, const char *s) {
    snprintf((char *)dst, n, "%s", s);
}

STUB_API int MV_CC_Initialize(void) { gInit = 1; return MV_OK; }
STUB_API int MV_CC_Finalize(void) { gInit = 0; return MV_OK; }
STUB_API unsigned int MV_CC_GetSDKVersion(void) { return 0x00000001; }

STUB_API int MV_CC_EnumDevices(unsigned int layer, STUB_DEVICE_INFO_LIST *list) {
    const char *env = getenv("NSS_MVS_STUB_CAMERAS");
    int n = env ? atoi(env) : 1, i;
    if (!list) return MV_E_PARAMETER;
    /* Like SDK 4.x: enumeration before MV_CC_Initialize is a call-order error,
       so the hikcam tests also prove the loader initialises the SDK. */
    if (!gInit) return MV_E_CALLORDER;
    if (n < 0) n = 0;
    if (n > 4) n = 4;
    memset(list, 0, sizeof(*list));
    if (!(layer & MV_USB_DEVICE)) return MV_OK;
    for (i = 0; i < n; i++) {
        char serial[32];
        STUB_DEVICE_INFO *d = &gDev[i];
        memset(d, 0, sizeof(*d));
        d->nTLayerType = MV_USB_DEVICE;
        put(d->SpecialInfo.stUsb3VInfo.chModelName, INFO_MAX_BUFFER_SIZE, "MV-STUB-U3V");
        snprintf(serial, sizeof(serial), "STUB%04d", i + 1);
        put(d->SpecialInfo.stUsb3VInfo.chSerialNumber, INFO_MAX_BUFFER_SIZE, serial);
        put(d->SpecialInfo.stUsb3VInfo.chVendorName, INFO_MAX_BUFFER_SIZE, "Hikrobot (stub)");
        list->pDeviceInfo[i] = d;
    }
    list->nDeviceNum = (unsigned)n;
    return MV_OK;
}

STUB_API int MV_CC_CreateHandle(void **handle, const STUB_DEVICE_INFO *info) {
    stub_cam *c;
    if (!handle || !info) return MV_E_PARAMETER;
    c = (stub_cam *)calloc(1, sizeof(*c));
    if (!c) return MV_E_HANDLE;
    c->index = (int)(info - gDev);
    c->width = STUB_MAX_W;
    c->height = STUB_MAX_H;
    c->pixfmt = 0x01080001; /* Mono8 */
    c->exposure = 10000.0f;
    c->buf = (unsigned char *)malloc(STUB_MAX_W * STUB_MAX_H * 2);
    *handle = c;
    return MV_OK;
}

STUB_API int MV_CC_DestroyHandle(void *h) {
    stub_cam *c = (stub_cam *)h;
    if (!c) return MV_E_HANDLE;
    free(c->buf);
    free(c);
    return MV_OK;
}

STUB_API int MV_CC_OpenDevice(void *h, unsigned int access, unsigned short key) {
    stub_cam *c = (stub_cam *)h;
    (void)access; (void)key;
    if (!c) return MV_E_HANDLE;
    if (getenv("NSS_MVS_STUB_FAIL_OPEN")) return (int)MV_E_ACCESS_DENIED;
    c->open = 1;
    return MV_OK;
}

STUB_API int MV_CC_CloseDevice(void *h) {
    stub_cam *c = (stub_cam *)h;
    if (!c) return MV_E_HANDLE;
    c->open = 0;
    c->grabbing = 0;
    return MV_OK;
}

STUB_API int MV_CC_StartGrabbing(void *h) {
    stub_cam *c = (stub_cam *)h;
    if (!c || !c->open) return MV_E_CALLORDER;
    c->grabbing = 1;
    return MV_OK;
}

STUB_API int MV_CC_StopGrabbing(void *h) {
    stub_cam *c = (stub_cam *)h;
    if (!c || !c->grabbing) return MV_E_CALLORDER;
    c->grabbing = 0;
    return MV_OK;
}

STUB_API int MV_CC_ClearImageBuffer(void *h) { return h ? MV_OK : MV_E_HANDLE; }

static int bytes_per_px(unsigned int f) { return f == 0x01080001 ? 1 : 2; }

/* Pixel value at (x, y) as an N-bit code: a ramp the tests can predict. */
static unsigned int ramp(const stub_cam *c, int x, int y, int bits) {
    unsigned int max = (1u << bits) - 1u;
    unsigned int v = (unsigned)(x + y * (int)c->width) * 37u + c->frameNum;
    return v % (max + 1u);
}

STUB_API int MV_CC_GetImageBuffer(void *h, STUB_FRAME_OUT *f, unsigned int ms) {
    stub_cam *c = (stub_cam *)h;
    int x, y, bits, bpp;
    (void)ms;
    if (!c || !f) return MV_E_PARAMETER;
    if (!c->grabbing) return MV_E_CALLORDER;
    if (c->triggerOn && !c->triggerPending) return (int)MV_E_NODATA;
    if (c->outstanding) return MV_E_CALLORDER;
    c->triggerPending = 0;
    c->frameNum++;
    switch (c->pixfmt) {
    case 0x01100003: bits = 10; break;
    case 0x01100005: bits = 12; break;
    case 0x01100007: bits = 16; break;
    default: bits = 8; break;
    }
    bpp = bytes_per_px(c->pixfmt);
    for (y = 0; y < (int)c->height; y++) {
        for (x = 0; x < (int)c->width; x++) {
            unsigned int v = ramp(c, x, y, bits);
            size_t i = (size_t)(y * c->width + x) * (size_t)bpp;
            c->buf[i] = (unsigned char)(v & 0xFF);
            if (bpp == 2) c->buf[i + 1] = (unsigned char)(v >> 8);
        }
    }
    memset(f, 0, sizeof(*f));
    f->pBufAddr = c->buf;
    f->nWidth = (unsigned short)c->width;
    f->nHeight = (unsigned short)c->height;
    f->enPixelType = c->pixfmt;
    f->nFrameNum = c->frameNum;
    f->nFrameLen = (unsigned)(c->width * c->height * bpp);
    c->outstanding = 1;
    return MV_OK;
}

STUB_API int MV_CC_FreeImageBuffer(void *h, STUB_FRAME_OUT *f) {
    stub_cam *c = (stub_cam *)h;
    (void)f;
    if (!c || !c->outstanding) return MV_E_CALLORDER;
    c->outstanding = 0;
    return MV_OK;
}

STUB_API int MV_CC_SetEnumValueByString(void *h, const char *key, const char *val) {
    stub_cam *c = (stub_cam *)h;
    if (!c || !key || !val) return MV_E_PARAMETER;
    if (!strcmp(key, "PixelFormat")) {
        if (!strcmp(val, "Mono8")) c->pixfmt = 0x01080001;
        else if (!strcmp(val, "Mono10")) c->pixfmt = 0x01100003;
        else if (!strcmp(val, "Mono12")) c->pixfmt = 0x01100005;
        else if (!strcmp(val, "Mono16")) c->pixfmt = 0x01100007;
        else return (int)MV_E_GC_PROPERTY;
        return MV_OK;
    }
    if (!strcmp(key, "TriggerMode")) { c->triggerOn = !strcmp(val, "On"); return MV_OK; }
    if (!strcmp(key, "TriggerSource") || !strcmp(key, "ExposureAuto") || !strcmp(key, "GainAuto") ||
        !strcmp(key, "AcquisitionMode") || !strcmp(key, "BlackLevelAuto")) return MV_OK;
    return (int)MV_E_SUPPORT;
}

STUB_API int MV_CC_SetFloatValue(void *h, const char *key, float v) {
    stub_cam *c = (stub_cam *)h;
    if (!c || !key) return MV_E_PARAMETER;
    if (!strcmp(key, "ExposureTime")) { c->exposure = v; return MV_OK; }
    if (!strcmp(key, "Gain")) { c->gain = v; return MV_OK; }
    if (!strcmp(key, "BlackLevel")) { c->black = v; return MV_OK; }
    return (int)MV_E_SUPPORT;
}

STUB_API int MV_CC_GetFloatValue(void *h, const char *key, STUB_FLOATVALUE *out) {
    stub_cam *c = (stub_cam *)h;
    if (!c || !key || !out) return MV_E_PARAMETER;
    memset(out, 0, sizeof(*out));
    if (!strcmp(key, "ExposureTime")) { out->fCurValue = c->exposure; out->fMin = 15; out->fMax = 1e7f; return MV_OK; }
    if (!strcmp(key, "Gain")) { out->fCurValue = c->gain; out->fMin = 0; out->fMax = 24; return MV_OK; }
    return (int)MV_E_SUPPORT;
}

static int64_t *int_node(stub_cam *c, const char *key, int64_t *maxv) {
    if (!strcmp(key, "Width")) { *maxv = STUB_MAX_W; return &c->width; }
    if (!strcmp(key, "Height")) { *maxv = STUB_MAX_H; return &c->height; }
    if (!strcmp(key, "OffsetX")) { *maxv = STUB_MAX_W - c->width; return &c->offX; }
    if (!strcmp(key, "OffsetY")) { *maxv = STUB_MAX_H - c->height; return &c->offY; }
    return NULL;
}

STUB_API int MV_CC_SetIntValueEx(void *h, const char *key, int64_t v) {
    stub_cam *c = (stub_cam *)h;
    int64_t maxv = 0, *p;
    if (!c || !key) return MV_E_PARAMETER;
    if (c->grabbing && (!strcmp(key, "Width") || !strcmp(key, "Height"))) return (int)MV_E_ACCESS_DENIED;
    p = int_node(c, key, &maxv);
    if (!p) return (int)MV_E_SUPPORT;
    if (v < 0 || v > maxv || ((!strcmp(key, "Width") || !strcmp(key, "Height")) && v < 8)) return (int)MV_E_GC_PROPERTY;
    *p = v;
    return MV_OK;
}

STUB_API int MV_CC_GetIntValueEx(void *h, const char *key, STUB_INTVALUE_EX *out) {
    stub_cam *c = (stub_cam *)h;
    int64_t maxv = 0, *p;
    if (!c || !key || !out) return MV_E_PARAMETER;
    p = int_node(c, key, &maxv);
    if (!p) return (int)MV_E_SUPPORT;
    memset(out, 0, sizeof(*out));
    out->nCurValue = *p;
    out->nMax = maxv;
    out->nInc = 1;
    return MV_OK;
}

STUB_API int MV_CC_SetBoolValue(void *h, const char *key, unsigned char v) {
    (void)v;
    if (!h || !key) return MV_E_PARAMETER;
    return (int)MV_E_SUPPORT;
}

STUB_API int MV_CC_SetCommandValue(void *h, const char *key) {
    stub_cam *c = (stub_cam *)h;
    if (!c || !key) return MV_E_PARAMETER;
    if (!strcmp(key, "TriggerSoftware")) {
        if (!c->grabbing || !c->triggerOn) return MV_E_CALLORDER;
        c->triggerPending = 1;
        return MV_OK;
    }
    return (int)MV_E_SUPPORT;
}
