#include <wchar.h>
typedef struct {
	unsigned resp;
	unsigned rparams[5];
	int nrparams;
	unsigned char *data;
	unsigned long long datalen;
	char err[200];
} wpd_result;
int wpd_list(wchar_t **out, char *err, int errn);
void wpd_free(void *p);
void *wpd_open(const wchar_t *id, char *err, int errn);
void wpd_close(void *d);
int wpd_exec(void *d, unsigned op, const unsigned *params, int nparams, int mode,
             const unsigned char *wdata, unsigned long long wlen, wpd_result *r);
int wpd_vendor_ops(void *d, unsigned *ops, int max, char *err, int errn);
