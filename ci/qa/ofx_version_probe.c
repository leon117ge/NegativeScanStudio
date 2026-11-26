/* Prints the OFX plugin identifiers and versions of a built plugin binary as
 * JSON, for check_mac_build.py --probe. macOS/Linux: dlopen; no host needed.
 * Usage: ofx_version_probe path/to/Contents/MacOS/NegativeScanStudio.ofx */
#include <dlfcn.h>
#include <stdio.h>
#include "ofxCore.h"

int main(int argc, char **argv) {
  if (argc != 2) {
    fprintf(stderr, "usage: %s plugin-binary\n", argv[0]);
    return 2;
  }
  void *h = dlopen(argv[1], RTLD_NOW | RTLD_LOCAL);
  if (!h) {
    fprintf(stderr, "dlopen failed: %s\n", dlerror());
    return 1;
  }
  int (*count)(void) = (int (*)(void))dlsym(h, "OfxGetNumberOfPlugins");
  OfxPlugin *(*get)(int) = (OfxPlugin * (*)(int)) dlsym(h, "OfxGetPlugin");
  if (!count || !get) {
    fprintf(stderr, "OfxGetNumberOfPlugins/OfxGetPlugin not exported\n");
    return 1;
  }
  int n = count();
  printf("{\"plugins\":[");
  for (int i = 0; i < n; i++) {
    OfxPlugin *p = get(i);
    printf("%s{\"id\":\"%s\",\"api\":\"%s\",\"api_version\":%d,\"major\":%u,\"minor\":%u}", i ? "," : "",
           p->pluginIdentifier, p->pluginApi, p->apiVersion, p->pluginVersionMajor, p->pluginVersionMinor);
  }
  printf("]}\n");
  return 0;
}
