//go:build darwin && cgo

// C interface of icc_darwin.m (ImageCaptureCore raw PTP pass-through).
#ifndef NSS_ICC_DARWIN_H
#define NSS_ICC_DARWIN_H

typedef struct {
	char name[256];
	char serial[128];
	char uuid[64];
	int vid, pid;
	int canPTP;
} icc_cam;

// icc_list fills up to max cameras and returns the count, or -1 with err set.
int icc_list(icc_cam *out, int max, double timeout, char *err, int errlen);

// icc_open opens a session on the camera with this UUID; NULL with err set on failure.
void *icc_open(const char *uuid, double timeout, char *err, int errlen);

// icc_send sends one PTP command container (plus data-out when hasOut) and
// returns malloc'd copies of the response container and the data-in phase.
int icc_send(void *h, const unsigned char *cmd, int cmdlen, const unsigned char *out, long long outlen, int hasOut,
	double timeout, unsigned char **resp, int *resplen, unsigned char **data, long long *datalen, char *err, int errlen);

// icc_close closes the session and releases the handle.
void icc_close(void *h, double timeout);

#endif
