/* Runtime loader for Hikrobot MvCameraControl.dll (Windows).
   No link-time dependency: LoadLibrary / GetProcAddress only. */
#ifndef HIKCAM_MVS_WINDOWS_H
#define HIKCAM_MVS_WINDOWS_H

#ifdef __cplusplus
extern "C" {
#endif

int hik_load(void);
int hik_available(void);
const char *hik_load_error(void);
int hik_enum(unsigned int layer, char *err, int errn);
int hik_count(void);
int hik_dev_info(int index, char *model, int modeln, char *serial, int serialn,
                 char *vendor, int vendorn, char *iface, int ifacen);
int hik_open(int index, void **handle, char *err, int errn);
int hik_close(void *handle);
int hik_set_enum_str(void *handle, const char *key, const char *val);
int hik_set_float(void *handle, const char *key, float val);
int hik_get_float(void *handle, const char *key, float *cur, float *minv, float *maxv);
int hik_set_int(void *handle, const char *key, long long val);
int hik_get_int(void *handle, const char *key, long long *cur, long long *minv, long long *maxv);
int hik_set_bool(void *handle, const char *key, int val);
int hik_command(void *handle, const char *key);
int hik_start(void *handle);
int hik_stop(void *handle);
int hik_grab(void *handle, unsigned int timeout_ms,
             unsigned char **data, int *w, int *h,
             unsigned int *pixfmt, unsigned int *len);
int hik_grab_free(void *handle);

#ifdef __cplusplus
}
#endif
#endif
