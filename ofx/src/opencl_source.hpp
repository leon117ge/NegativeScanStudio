#pragma once
namespace nss {inline constexpr const char *openclSource=R"NSSCL(#define NSS_PARAMS __constant const float *
#define NSS_EXP exp
#define NSS_EXP2 exp2
#define NSS_LOG log
#define NSS_LOG2 log2
#define NSS_POW pow
#define NSS_TANH tanh
#define NSS_MIN fmin
#define NSS_MAX fmax
// Shared C++/OpenCL/CUDA scalar math. Constants are prepared once per render.
#ifndef NSS_PARAMS
#define NSS_PARAMS const float *
#endif
inline float nss_di_decode(float v){return v>.02740668f?NSS_EXP2(v/.07329248f-7.f)-.0075f:v/10.44426855f;}
// Density (or transmittance) code for one channel.
inline float nss_code(float value,NSS_PARAMS p,int c){
 float tr=NSS_MAX((value-p[9+c])*p[5]/NSS_MAX(p[6+c],.000001f),.000001f);
 float code=p[3]+NSS_LOG(tr)*p[4];
 if(p[1]!=0.f)code=tr;
 if(p[2]!=0.f)code=NSS_MIN(NSS_MAX(code,0.f),1.f);
 return code;
}
// Scene-linear positive for one channel (after the optional Contrast curve).
inline float nss_linear(float code,NSS_PARAMS p,int c){
 float d=(code*1023.f-95.f)*.002f;
 float l=p[22+c]*(NSS_EXP(NSS_MIN(NSS_MAX(2.302585f*(d-p[15])/p[16+c],-30.f),20.f))-p[19+c])/(1.f-p[19+c]);
 l=NSS_MIN(NSS_MAX(l,0.f),64.f);
 if(p[14]!=0.f){float y=.18f*NSS_POW(l/.18f,p[25]);l=y>p[26]?p[26]+(1.f-p[26])*NSS_TANH((y-p[26])/(1.f-p[26])):y;}
 return l;
}
inline float nss_encode(float l,NSS_PARAMS p){
 int mode=(int)p[13];
 if(mode==0)return l<.018f?4.5f*l:1.099f*NSS_POW(l,.45f)-.099f;
 if(mode==1)return l>.00262409f?(NSS_LOG2(l+.0075f)+7.f)*.07329248f:l*10.44426855f;
 if(mode==2)return NSS_MAX((685.f+300.f*.4342944819f*NSS_LOG(l*.98921f+.01079f))/1023.f,0.f);
 if(mode==3)return l>.0078125f?(NSS_LOG2(l)+9.72f)/17.52f:10.5402377417f*l+.0729055342f;
 return l;
}
// 0.90.11 per-channel path. Used unchanged whenever the Tone stage is inactive.
inline float nss_channel(float value,NSS_PARAMS p,int c){
 float code=nss_code(value,p,c);
 if(p[12]==0.f||p[1]!=0.f)return code;
 return nss_encode(nss_linear(code,p,c),p);
}
// Tone stage (0.90.12). Active only for the positive stage and density output.
inline int nss_tone_active(NSS_PARAMS p){return p[12]!=0.f&&p[1]==0.f&&(p[38]!=0.f||p[42]!=0.f||p[48]!=0.f);}
inline float nss_tone_norm(float r,float g,float b){return (r+g+b)*.33333334f;}
inline float nss_tone_min(float r,float g,float b){return NSS_MIN(r,NSS_MIN(g,b));}
// Highlight shoulder: gain applied equally to R,G,B (keeps R:G:B). x=log2(n/0.18).
// f=xs+h*(1-exp(-(x-xs)/h)) approaches xs+h; y=x+s*(f-x). Slope 1 at xs (C1), monotonic.
inline float nss_shoulder_gain(float n,NSS_PARAMS p){
 if(p[38]==0.f||!(n>p[46]))return 1.f;
 float x=NSS_LOG2(n/.18f);
 float f=p[40]+p[41]*(1.f-NSS_EXP(-(x-p[40])/p[41]));
 return NSS_EXP2(p[39]*(f-x));
}
// Shadow toe on the darkest channel m: the same offset is added to R,G,B (keeps HSV hue).
// g=xt-r*(1-exp(-(xt-x)/r)) approaches xt-r (toe black); m becomes m+s*(0.18*2^g-m).
// Slope 1 at xt (C1). Using the darkest channel keeps every channel monotonic along any hue line;
// pure black lifts to the neutral floor s*0.18*2^black.
inline float nss_toe_offset(float n,NSS_PARAMS p){
 if(p[42]==0.f||!(n<p[47]))return 0.f;
 float x=NSS_LOG2(NSS_MAX(n,1e-30f)/.18f);
 float g=p[44]-p[45]*(1.f-NSS_EXP(-(p[44]-x)/p[45]));
 return p[43]*(.18f*NSS_EXP2(g)-n);
}
// Tone color (0.90.13): highlight/shadow desaturation and highlight cream, after shoulder and toe.
// Region weight from the RGB mean in stops: smoothstep over 1.5 stops past the start (C1, reaches 1).
// Desaturation mixes toward Rec709 luminance Y (Y and HSV hue unchanged). Cream multiplies R,G,B by
// (1+a, 1, 1-0.3a), a=0.4*strength*w_highlight*max(1-2*saturation,0)^2, measured AFTER desaturation (so
// desaturation cannot cancel it), then rescales so Y is unchanged: near-neutral highlights turn warm
// cream, saturated colors keep their hue.
inline float nss_tone_w(float t){float u=NSS_MIN(NSS_MAX(t*(1.f/1.5f),0.f),1.f);return u*u*(3.f-2.f*u);}
inline float nss_tone_luma(float r,float g,float b){return .2126f*r+.7152f*g+.0722f*b;}
inline void nss_tone_color(float *r,float *g,float *b,NSS_PARAMS p){
 float n=nss_tone_norm(*r,*g,*b);
 if(!(n>p[54])&&!(n<p[55]))return;
 float x=NSS_LOG2(NSS_MAX(n,1e-30f)/.18f);
 float wh=nss_tone_w(x-p[50]),ws=nss_tone_w(p[52]-x);
 float y=nss_tone_luma(*r,*g,*b),wd=p[49]*wh+p[51]*ws;
 *r+=wd*(y-*r);*g+=wd*(y-*g);*b+=wd*(y-*b);
 float mx=NSS_MAX(*r,NSS_MAX(*g,*b)),mn=NSS_MIN(*r,NSS_MIN(*g,*b));
 float f=mx>0.f?NSS_MAX(1.f-2.f*(mx-mn)/mx,0.f):0.f,a=p[53]*wh*f*f; // cream fades out by saturation 0.5
 if(a>0.f){float cr=*r*(1.f+a),cg=*g,cb=*b*(1.f-.3f*a),ym=nss_tone_luma(cr,cg,cb);
  if(ym>0.f){float s=y/ym;*r=cr*s;*g=cg*s;*b=cb*s;}}
}
// The build embeds render_math.h before this source.
__kernel void nss_render(__global const float *source,__global float *dest,__constant const float *p,
 int sx1,int sy1,int sx2,int sy2,int sourceStride,int dx1,int dy1,int dx2,int dy2,int destStride,
 int wx1,int wy1,int width,int height,int premult){
 int x=wx1+(int)get_global_id(0),y=wy1+(int)get_global_id(1);
 if(x>=wx1+width||y>=wy1+height||x<dx1||x>=dx2||y<dy1||y>=dy2)return;
 int oi=(y-dy1)*destStride+(x-dx1)*4;
 if(x<sx1||x>=sx2||y<sy1||y>=sy2){vstore4((float4)(0),0,dest+oi);return;}
 float4 rgba=vload4(0,source+(y-sy1)*sourceStride+(x-sx1)*4);float3 v=rgba.xyz;
 if(premult&&rgba.w!=0.f)v/=rgba.w;
 if(p[0]!=0.f)v=(float3)(nss_di_decode(v.x),nss_di_decode(v.y),nss_di_decode(v.z));
 if(p[27]!=0.f)v=(float3)(v.x+p[37]*(p[28]*v.x+p[29]*v.y+p[30]*v.z-v.x),v.y+p[37]*(p[31]*v.x+p[32]*v.y+p[33]*v.z-v.y),v.z+p[37]*(p[34]*v.x+p[35]*v.y+p[36]*v.z-v.z));
 if(!nss_tone_active(p))v=(float3)(nss_channel(v.x,p,0),nss_channel(v.y,p,1),nss_channel(v.z,p,2));
 else{v=(float3)(nss_linear(nss_code(v.x,p,0),p,0),nss_linear(nss_code(v.y,p,1),p,1),nss_linear(nss_code(v.z,p,2),p,2));
  v*=nss_shoulder_gain(nss_tone_norm(v.x,v.y,v.z),p);float t=nss_toe_offset(nss_tone_min(v.x,v.y,v.z),p);
  float r=v.x+t,g=v.y+t,b=v.z+t;if(p[48]!=0.f)nss_tone_color(&r,&g,&b,p);
  v=(float3)(nss_encode(r,p),nss_encode(g,p),nss_encode(b,p));}
 if(premult)v*=rgba.w;
 vstore4((float4)(v,rgba.w),0,dest+oi);
}
)NSSCL";}
