// WPD (Windows Portable Devices) transport: raw PTP through the MTP class
// driver's vendor-extension commands (WPD_COMMAND_MTP_EXT_*).
#define COBJMACROS
#define INITGUID
#include <windows.h>
#include <objbase.h>
#include <portabledeviceapi.h>
#include <portabledevicetypes.h>
#include <portabledevice.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include "wpd_windows.h"

// WpdMtpExtensions.h (not in mingw-w64).
#define MTPX {0x4d545058, 0x1a2e, 0x4106, {0xa3, 0x57, 0x77, 0x1e, 0x08, 0x19, 0xfc, 0x56}}
static const PROPERTYKEY FBM_CMD_GET_VENDOR_OPCODES = {MTPX, 11};
static const PROPERTYKEY FBM_CMD_EXEC_NO_DATA = {MTPX, 12};
static const PROPERTYKEY FBM_CMD_EXEC_READ = {MTPX, 13};
static const PROPERTYKEY FBM_CMD_EXEC_WRITE = {MTPX, 14};
static const PROPERTYKEY FBM_CMD_READ_DATA = {MTPX, 15};
static const PROPERTYKEY FBM_CMD_WRITE_DATA = {MTPX, 16};
static const PROPERTYKEY FBM_CMD_END_TRANSFER = {MTPX, 17};
static const PROPERTYKEY FBM_P_OPCODE = {MTPX, 1001};
static const PROPERTYKEY FBM_P_PARAMS = {MTPX, 1002};
static const PROPERTYKEY FBM_P_RESP_CODE = {MTPX, 1003};
static const PROPERTYKEY FBM_P_RESP_PARAMS = {MTPX, 1004};
static const PROPERTYKEY FBM_P_VENDOR_OPCODES = {MTPX, 1005};
static const PROPERTYKEY FBM_P_CONTEXT = {MTPX, 1006};
static const PROPERTYKEY FBM_P_TOTAL_SIZE = {MTPX, 1007};
static const PROPERTYKEY FBM_P_TO_READ = {MTPX, 1008};
static const PROPERTYKEY FBM_P_READ = {MTPX, 1009};
static const PROPERTYKEY FBM_P_TO_WRITE = {MTPX, 1010};
static const PROPERTYKEY FBM_P_WRITTEN = {MTPX, 1011};
static const PROPERTYKEY FBM_P_DATA = {MTPX, 1012};
static const PROPERTYKEY FBM_P_OPTIMAL = {MTPX, 1013};

static void seterr(char *err, int n, const char *step, HRESULT hr) {
	if (err && n > 0) snprintf(err, n, "%s failed (HRESULT 0x%08lX)", step, (unsigned long)hr);
}

static HRESULT com_init(void) {
	HRESULT hr = CoInitializeEx(NULL, COINIT_MULTITHREADED);
	if (hr == S_OK || hr == S_FALSE || hr == RPC_E_CHANGED_MODE) return S_OK;
	return hr;
}

// wpd_list writes "id\tname\tmanufacturer\tdescription\n" lines (UTF-16) into a
// malloc'd buffer.
int wpd_list(wchar_t **out, char *err, int errn) {
	*out = NULL;
	HRESULT hr = com_init();
	if (FAILED(hr)) { seterr(err, errn, "CoInitializeEx", hr); return -1; }
	IPortableDeviceManager *mgr = NULL;
	hr = CoCreateInstance(&CLSID_PortableDeviceManager, NULL, CLSCTX_INPROC_SERVER, &IID_IPortableDeviceManager, (void **)&mgr);
	if (FAILED(hr)) { seterr(err, errn, "Create PortableDeviceManager (WPD unavailable)", hr); return -1; }
	IPortableDeviceManager_RefreshDeviceList(mgr);
	DWORD n = 0;
	hr = IPortableDeviceManager_GetDevices(mgr, NULL, &n);
	if (FAILED(hr)) { IPortableDeviceManager_Release(mgr); seterr(err, errn, "GetDevices", hr); return -1; }
	size_t cap = 4096, len = 0;
	wchar_t *buf = (wchar_t *)calloc(cap, sizeof(wchar_t));
	if (n > 0) {
		LPWSTR *ids = (LPWSTR *)calloc(n, sizeof(LPWSTR));
		hr = IPortableDeviceManager_GetDevices(mgr, ids, &n);
		for (DWORD i = 0; SUCCEEDED(hr) && i < n; i++) {
			wchar_t name[512] = L"", manu[512] = L"", desc[512] = L"";
			DWORD k = 512; IPortableDeviceManager_GetDeviceFriendlyName(mgr, ids[i], name, &k);
			k = 512; IPortableDeviceManager_GetDeviceManufacturer(mgr, ids[i], manu, &k);
			k = 512; IPortableDeviceManager_GetDeviceDescription(mgr, ids[i], desc, &k);
			size_t need = wcslen(ids[i]) + wcslen(name) + wcslen(manu) + wcslen(desc) + 8;
			if (len + need >= cap) { cap = (len + need) * 2; buf = (wchar_t *)realloc(buf, cap * sizeof(wchar_t)); }
			len += swprintf(buf + len, cap - len, L"%ls\t%ls\t%ls\t%ls\n", ids[i], name, manu, desc);
			CoTaskMemFree(ids[i]);
		}
		free(ids);
	}
	IPortableDeviceManager_Release(mgr);
	*out = buf;
	return (int)n;
}

void wpd_free(void *p) { free(p); }

void *wpd_open(const wchar_t *id, char *err, int errn) {
	HRESULT hr = com_init();
	if (FAILED(hr)) { seterr(err, errn, "CoInitializeEx", hr); return NULL; }
	IPortableDeviceValues *ci = NULL;
	hr = CoCreateInstance(&CLSID_PortableDeviceValues, NULL, CLSCTX_INPROC_SERVER, &IID_IPortableDeviceValues, (void **)&ci);
	if (FAILED(hr)) { seterr(err, errn, "Create PortableDeviceValues", hr); return NULL; }
	IPortableDeviceValues_SetStringValue(ci, &WPD_CLIENT_NAME, L"Film Base Meter");
	IPortableDeviceValues_SetUnsignedIntegerValue(ci, &WPD_CLIENT_MAJOR_VERSION, 0);
	IPortableDeviceValues_SetUnsignedIntegerValue(ci, &WPD_CLIENT_MINOR_VERSION, 3);
	IPortableDeviceValues_SetUnsignedIntegerValue(ci, &WPD_CLIENT_REVISION, 0);
	IPortableDeviceValues_SetUnsignedIntegerValue(ci, &WPD_CLIENT_DESIRED_ACCESS, GENERIC_READ | GENERIC_WRITE);
	IPortableDeviceValues_SetUnsignedIntegerValue(ci, &WPD_CLIENT_SECURITY_QUALITY_OF_SERVICE, SECURITY_IMPERSONATION);
	IPortableDevice *dev = NULL;
	hr = CoCreateInstance(&CLSID_PortableDeviceFTM, NULL, CLSCTX_INPROC_SERVER, &IID_IPortableDevice, (void **)&dev);
	if (FAILED(hr)) { IPortableDeviceValues_Release(ci); seterr(err, errn, "Create PortableDevice", hr); return NULL; }
	hr = IPortableDevice_Open(dev, id, ci);
	if (hr == E_ACCESSDENIED) {
		IPortableDeviceValues_SetUnsignedIntegerValue(ci, &WPD_CLIENT_DESIRED_ACCESS, GENERIC_READ);
		hr = IPortableDevice_Open(dev, id, ci);
	}
	IPortableDeviceValues_Release(ci);
	if (FAILED(hr)) { IPortableDevice_Release(dev); seterr(err, errn, "IPortableDevice::Open", hr); return NULL; }
	return dev;
}

void wpd_close(void *d) {
	if (!d) return;
	com_init();
	IPortableDevice *dev = (IPortableDevice *)d;
	IPortableDevice_Close(dev);
	IPortableDevice_Release(dev);
}

static HRESULT new_cmd(const PROPERTYKEY *cmd, IPortableDeviceValues **out) {
	HRESULT hr = CoCreateInstance(&CLSID_PortableDeviceValues, NULL, CLSCTX_INPROC_SERVER, &IID_IPortableDeviceValues, (void **)out);
	if (FAILED(hr)) return hr;
	IPortableDeviceValues_SetGuidValue(*out, &WPD_PROPERTY_COMMON_COMMAND_CATEGORY, &cmd->fmtid);
	IPortableDeviceValues_SetUnsignedIntegerValue(*out, &WPD_PROPERTY_COMMON_COMMAND_ID, cmd->pid);
	return S_OK;
}

static HRESULT wpd_send(IPortableDevice *dev, IPortableDeviceValues *in, IPortableDeviceValues **res) {
	HRESULT hr = IPortableDevice_SendCommand(dev, 0, in, res);
	if (FAILED(hr)) return hr;
	HRESULT dhr = S_OK;
	if (SUCCEEDED(IPortableDeviceValues_GetErrorValue(*res, &WPD_PROPERTY_COMMON_HRESULT, &dhr)) && FAILED(dhr)) {
		IPortableDeviceValues_Release(*res);
		*res = NULL;
		return dhr;
	}
	return S_OK;
}

static void read_response(IPortableDeviceValues *res, wpd_result *r) {
	ULONG code = 0;
	if (SUCCEEDED(IPortableDeviceValues_GetUnsignedIntegerValue(res, &FBM_P_RESP_CODE, &code))) r->resp = code;
	IPortableDevicePropVariantCollection *rp = NULL;
	if (SUCCEEDED(IPortableDeviceValues_GetIPortableDevicePropVariantCollectionValue(res, &FBM_P_RESP_PARAMS, &rp)) && rp) {
		DWORD n = 0;
		IPortableDevicePropVariantCollection_GetCount(rp, &n);
		for (DWORD i = 0; i < n && i < 5; i++) {
			PROPVARIANT pv; PropVariantInit(&pv);
			if (SUCCEEDED(IPortableDevicePropVariantCollection_GetAt(rp, i, &pv))) {
				r->rparams[i] = pv.ulVal;
				r->nrparams = i + 1;
			}
			PropVariantClear(&pv);
		}
		IPortableDevicePropVariantCollection_Release(rp);
	}
}

// wpd_exec runs one PTP operation. mode: 0 no data, 1 read, 2 write.
int wpd_exec(void *d, unsigned op, const unsigned *params, int nparams, int mode,
             const unsigned char *wdata, unsigned long long wlen, wpd_result *r) {
	IPortableDevice *dev = (IPortableDevice *)d;
	memset(r, 0, sizeof(*r));
	HRESULT hr = com_init();
	if (FAILED(hr)) { seterr(r->err, sizeof(r->err), "CoInitializeEx", hr); return -1; }
	const PROPERTYKEY *cmd = mode == 0 ? &FBM_CMD_EXEC_NO_DATA : mode == 1 ? &FBM_CMD_EXEC_READ : &FBM_CMD_EXEC_WRITE;
	IPortableDeviceValues *in = NULL, *res = NULL;
	IPortableDevicePropVariantCollection *pc = NULL;
	LPWSTR ctx = NULL;
	int rc = -1;
	if (FAILED(hr = new_cmd(cmd, &in))) { seterr(r->err, sizeof(r->err), "Create command", hr); goto done; }
	IPortableDeviceValues_SetUnsignedIntegerValue(in, &FBM_P_OPCODE, op);
	hr = CoCreateInstance(&CLSID_PortableDevicePropVariantCollection, NULL, CLSCTX_INPROC_SERVER, &IID_IPortableDevicePropVariantCollection, (void **)&pc);
	if (FAILED(hr)) { seterr(r->err, sizeof(r->err), "Create param collection", hr); goto done; }
	for (int i = 0; i < nparams; i++) {
		PROPVARIANT pv; PropVariantInit(&pv);
		pv.vt = VT_UI4; pv.ulVal = params[i];
		IPortableDevicePropVariantCollection_Add(pc, &pv);
	}
	IPortableDeviceValues_SetIPortableDevicePropVariantCollectionValue(in, &FBM_P_PARAMS, pc);
	if (mode == 2) IPortableDeviceValues_SetUnsignedLargeIntegerValue(in, &FBM_P_TOTAL_SIZE, wlen);
	if (FAILED(hr = wpd_send(dev, in, &res))) { seterr(r->err, sizeof(r->err), "SendCommand(execute)", hr); goto done; }
	if (mode == 0) { read_response(res, r); rc = 0; goto done; }

	if (FAILED(hr = IPortableDeviceValues_GetStringValue(res, &FBM_P_CONTEXT, &ctx))) { seterr(r->err, sizeof(r->err), "Get transfer context", hr); goto done; }
	ULONG chunk = 0;
	IPortableDeviceValues_GetUnsignedIntegerValue(res, &FBM_P_OPTIMAL, &chunk);
	if (chunk < 4096 || chunk > (1u << 22)) chunk = 256 * 1024;
	if (mode == 1) {
		ULONGLONG total = 0;
		IPortableDeviceValues_GetUnsignedLargeIntegerValue(res, &FBM_P_TOTAL_SIZE, &total);
		IPortableDeviceValues_Release(res); res = NULL;
		r->data = (unsigned char *)malloc(total ? (size_t)total : 1);
		if (!r->data) { snprintf(r->err, sizeof(r->err), "out of memory (%llu bytes)", (unsigned long long)total); goto end; }
		unsigned long long got = 0;
		while (got < total) {
			IPortableDeviceValues *rin = NULL, *rres = NULL;
			ULONG want = (ULONG)((total - got) < chunk ? (total - got) : chunk);
			if (FAILED(hr = new_cmd(&FBM_CMD_READ_DATA, &rin))) { seterr(r->err, sizeof(r->err), "Create read", hr); goto end; }
			IPortableDeviceValues_SetStringValue(rin, &FBM_P_CONTEXT, ctx);
			IPortableDeviceValues_SetUnsignedIntegerValue(rin, &FBM_P_TO_READ, want);
			BYTE *tmp = (BYTE *)calloc(want, 1);
			IPortableDeviceValues_SetBufferValue(rin, &FBM_P_DATA, tmp, want);
			free(tmp);
			hr = wpd_send(dev, rin, &rres);
			IPortableDeviceValues_Release(rin);
			if (FAILED(hr)) { seterr(r->err, sizeof(r->err), "SendCommand(read data)", hr); goto end; }
			ULONG nread = 0;
			IPortableDeviceValues_GetUnsignedIntegerValue(rres, &FBM_P_READ, &nread);
			BYTE *b = NULL; DWORD blen = 0;
			if (SUCCEEDED(IPortableDeviceValues_GetBufferValue(rres, &FBM_P_DATA, &b, &blen)) && b) {
				if (nread > blen) nread = blen;
				if (got + nread > total) nread = (ULONG)(total - got);
				memcpy(r->data + got, b, nread);
				CoTaskMemFree(b);
			} else nread = 0;
			IPortableDeviceValues_Release(rres);
			if (nread == 0) break;
			got += nread;
		}
		r->datalen = got;
	} else {
		IPortableDeviceValues_Release(res); res = NULL;
		unsigned long long sent = 0;
		while (sent < wlen) {
			IPortableDeviceValues *win = NULL, *wres = NULL;
			ULONG n = (ULONG)((wlen - sent) < chunk ? (wlen - sent) : chunk);
			if (FAILED(hr = new_cmd(&FBM_CMD_WRITE_DATA, &win))) { seterr(r->err, sizeof(r->err), "Create write", hr); goto end; }
			IPortableDeviceValues_SetStringValue(win, &FBM_P_CONTEXT, ctx);
			IPortableDeviceValues_SetUnsignedIntegerValue(win, &FBM_P_TO_WRITE, n);
			IPortableDeviceValues_SetBufferValue(win, &FBM_P_DATA, (BYTE *)(wdata + sent), n);
			hr = wpd_send(dev, win, &wres);
			IPortableDeviceValues_Release(win);
			if (FAILED(hr)) { seterr(r->err, sizeof(r->err), "SendCommand(write data)", hr); goto end; }
			ULONG nw = n;
			IPortableDeviceValues_GetUnsignedIntegerValue(wres, &FBM_P_WRITTEN, &nw);
			IPortableDeviceValues_Release(wres);
			if (nw == 0) break;
			sent += nw;
		}
	}
end:
	{
		IPortableDeviceValues *ein = NULL, *eres = NULL;
		HRESULT ehr = new_cmd(&FBM_CMD_END_TRANSFER, &ein);
		if (SUCCEEDED(ehr)) {
			IPortableDeviceValues_SetStringValue(ein, &FBM_P_CONTEXT, ctx);
			ehr = wpd_send(dev, ein, &eres);
			IPortableDeviceValues_Release(ein);
			if (SUCCEEDED(ehr)) { read_response(eres, r); IPortableDeviceValues_Release(eres); }
			else if (!r->err[0]) seterr(r->err, sizeof(r->err), "SendCommand(end transfer)", ehr);
		}
		rc = r->err[0] ? -1 : 0;
	}
done:
	if (ctx) CoTaskMemFree(ctx);
	if (pc) IPortableDevicePropVariantCollection_Release(pc);
	if (res) IPortableDeviceValues_Release(res);
	if (in) IPortableDeviceValues_Release(in);
	return rc;
}

int wpd_vendor_ops(void *d, unsigned *ops, int max, char *err, int errn) {
	IPortableDevice *dev = (IPortableDevice *)d;
	HRESULT hr = com_init();
	IPortableDeviceValues *in = NULL, *res = NULL;
	if (FAILED(hr = new_cmd(&FBM_CMD_GET_VENDOR_OPCODES, &in))) { seterr(err, errn, "Create command", hr); return -1; }
	hr = wpd_send(dev, in, &res);
	IPortableDeviceValues_Release(in);
	if (FAILED(hr)) { seterr(err, errn, "GET_SUPPORTED_VENDOR_OPCODES", hr); return -1; }
	IPortableDevicePropVariantCollection *c = NULL;
	int n = 0;
	if (SUCCEEDED(IPortableDeviceValues_GetIPortableDevicePropVariantCollectionValue(res, &FBM_P_VENDOR_OPCODES, &c)) && c) {
		DWORD k = 0;
		IPortableDevicePropVariantCollection_GetCount(c, &k);
		for (DWORD i = 0; i < k && n < max; i++) {
			PROPVARIANT pv; PropVariantInit(&pv);
			if (SUCCEEDED(IPortableDevicePropVariantCollection_GetAt(c, i, &pv))) ops[n++] = pv.ulVal;
			PropVariantClear(&pv);
		}
		IPortableDevicePropVariantCollection_Release(c);
	}
	IPortableDeviceValues_Release(res);
	return n;
}
