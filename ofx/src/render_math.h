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
inline int nss_tone_active(NSS_PARAMS p){return p[12]!=0.f&&p[1]==0.f&&(p[38]!=0.f||p[42]!=0.f);}
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
