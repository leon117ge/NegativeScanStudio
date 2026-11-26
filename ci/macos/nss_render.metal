// Metal port of ofx/src/render.cl / render_cuda.cu for the macOS proof build.
// ci/macos/make_msl.sh writes render_math_msl.h from ofx/src/render_math.h (same math,
// plain pointer parameters gain the MSL 'thread' address space). Build with -fno-fast-math.
#include <metal_stdlib>
using namespace metal;
#define NSS_EXP precise::exp
#define NSS_EXP2 precise::exp2
#define NSS_LOG precise::log
#define NSS_LOG2 precise::log2
#define NSS_POW precise::pow
#define NSS_TANH precise::tanh
#define NSS_MIN precise::fmin
#define NSS_MAX precise::fmax
#define NSS_PARAMS constant const float *
#include "render_math_msl.h"
struct NssArgs {
 int sx1,sy1,sx2,sy2,sourceStride,dx1,dy1,dx2,dy2,destStride,wx1,wy1,width,height,premult;
};
kernel void nss_render_metal(device const float *source [[buffer(0)]],device float *dest [[buffer(1)]],
 constant const float *p [[buffer(2)]],constant NssArgs &a [[buffer(3)]],uint2 gid [[thread_position_in_grid]]){
 if(int(gid.x)>=a.width||int(gid.y)>=a.height)return;
 int x=a.wx1+int(gid.x),y=a.wy1+int(gid.y);
 if(x<a.dx1||x>=a.dx2||y<a.dy1||y>=a.dy2)return;
 long oi=long(y-a.dy1)*a.destStride+long(x-a.dx1)*4;
 if(x<a.sx1||x>=a.sx2||y<a.sy1||y>=a.sy2){dest[oi]=0.f;dest[oi+1]=0.f;dest[oi+2]=0.f;dest[oi+3]=0.f;return;}
 long si=long(y-a.sy1)*a.sourceStride+long(x-a.sx1)*4;
 float r=source[si],g=source[si+1],b=source[si+2],al=source[si+3];
 if(a.premult&&al!=0.f){r/=al;g/=al;b/=al;}
 if(p[0]!=0.f){r=nss_di_decode(r);g=nss_di_decode(g);b=nss_di_decode(b);}
 if(p[27]!=0.f){float nr=r+p[37]*(p[28]*r+p[29]*g+p[30]*b-r),ng=g+p[37]*(p[31]*r+p[32]*g+p[33]*b-g),nb=b+p[37]*(p[34]*r+p[35]*g+p[36]*b-b);r=nr;g=ng;b=nb;}
 if(!nss_tone_active(p)){r=nss_channel(r,p,0);g=nss_channel(g,p,1);b=nss_channel(b,p,2);}
 else{r=nss_linear(nss_code(r,p,0),p,0);g=nss_linear(nss_code(g,p,1),p,1);b=nss_linear(nss_code(b,p,2),p,2);
  float k=nss_shoulder_gain(nss_tone_norm(r,g,b),p);r*=k;g*=k;b*=k;float t=nss_toe_offset(nss_tone_min(r,g,b),p);r+=t;g+=t;b+=t;
  if(p[48]!=0.f)nss_tone_color(&r,&g,&b,p);
  r=nss_encode(r,p);g=nss_encode(g,p);b=nss_encode(b,p);}
 if(a.premult){r*=al;g*=al;b*=al;}
 dest[oi]=r;dest[oi+1]=g;dest[oi+2]=b;dest[oi+3]=al;
}
