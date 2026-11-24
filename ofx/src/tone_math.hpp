#pragma once
// Tone stage (0.90.12): optional highlight shoulder and shadow toe on scene-linear positive.
// Host-side constants for the GPU/CPU kernels plus a direct reference used by tests.
#include "tone_params.hpp"
#include <cmath>
namespace nss {
struct ToneConstants {bool shoulder=false,toe=false,color=false;float ss=0,xs=0,h=1,ts=0,xt=0,r=1,ns=0,nt=0,hd=0,hs=0,sd=0,ss2=0,cream=0,nh=0,nsh=0;};
inline constexpr float kCreamAmount=.40f; // Highlight cream 1.0 = +40% R, -12% B (before Y rescale) at full highlight weight
inline ToneConstants toneConstants(const Tone &t){
 ToneConstants c;auto clamp01=[](float v){return std::fmin(std::fmax(v,0.f),1.f);};
 c.ss=clamp01(t.shoulder_strength);c.ts=clamp01(t.toe_strength);
 c.shoulder=t.shoulder_on!=0&&c.ss>0.f;c.toe=t.toe_on!=0&&c.ts>0.f;
 c.xs=t.shoulder_pivot;c.h=std::fmax(t.shoulder_white-t.shoulder_pivot,.05f);
 // The toe never starts above the shoulder, so the two regions stay disjoint.
 c.xt=c.shoulder?std::fmin(t.toe_pivot,c.xs):t.toe_pivot;c.r=std::fmax(c.xt-t.toe_black,.05f);
 c.ns=.18f*std::exp2(c.xs);c.nt=.18f*std::exp2(c.xt);
 // 0.90.13 color: desaturation and cream. Shadow start never above highlight start.
 // 0.90.14: each switch off is exactly amount 0.
 c.hd=t.highlight_desat_on?clamp01(t.highlight_desat):0.f;c.sd=t.shadow_desat_on?clamp01(t.shadow_desat):0.f;c.cream=t.highlight_cream_on?kCreamAmount*clamp01(t.highlight_cream):0.f;
 c.color=c.hd>0.f||c.sd>0.f||c.cream>0.f;c.hs=t.highlight_start;c.ss2=std::fmin(t.shadow_start,c.hs);
 // Thresholds: highlight weight is 0 at or below hs and shadow weight is 0 at or above ss2.
 c.nh=c.hd>0.f||c.cream>0.f?.18f*std::exp2(c.hs):3.4e38f;c.nsh=c.sd>0.f?.18f*std::exp2(c.ss2):-1.f;
 return c;
}
// Reference tone on scene-linear RGB (written in stops, independent of render_math.h).
inline void toneReference(const Tone &t,float &r,float &g,float &b){
 auto c=toneConstants(t);
 if(c.shoulder){float n=(r+g+b)/3.f;if(n>0.f){float x=std::log2(n/.18f);if(x>c.xs){
  float f=c.xs+c.h*(1.f-std::exp(-(x-c.xs)/c.h));float k=std::exp2(c.ss*(f-x));r*=k;g*=k;b*=k;}}}
 if(c.toe){float n=std::fmin(r,std::fmin(g,b));float x=std::log2(std::fmax(n,1e-30f)/.18f);if(x<c.xt){
  float y=c.xt-c.r*(1.f-std::exp(-(c.xt-x)/c.r));float o=c.ts*(.18f*std::exp2(y)-n);r+=o;g+=o;b+=o;}}
 if(c.color){float n=(r+g+b)/3.f;float x=std::log2(std::fmax(n,1e-30f)/.18f);
  auto w=[](float t){float u=std::fmin(std::fmax(t/1.5f,0.f),1.f);return u*u*(3.f-2.f*u);};
  float wh=(c.hd>0.f||c.cream>0.f)?w(x-c.hs):0.f,ws=c.sd>0.f?w(c.ss2-x):0.f;
  float Y=.2126f*r+.7152f*g+.0722f*b,wd=c.hd*wh+c.sd*ws;
  r=r+wd*(Y-r);g=g+wd*(Y-g);b=b+wd*(Y-b);
  float mx=std::fmax(r,std::fmax(g,b)),mn=std::fmin(r,std::fmin(g,b));float f=mx>0.f?std::fmax(1.f-2.f*(mx-mn)/mx,0.f):0.f,a=c.cream*wh*f*f;if(a>0.f){float cr=r*(1.f+a),cg=g,cb=b*(1.f-.3f*a),ym=.2126f*cr+.7152f*cg+.0722f*cb;if(ym>0.f){r=cr*Y/ym;g=cg*Y/ym;b=cb*Y/ym;}}}
}
inline float toneEncodeReference(int mode,float l){
 if(mode==0)return l<.018f?4.5f*l:1.099f*std::pow(l,.45f)-.099f;
 if(mode==1)return l>.00262409f?(std::log2(l+.0075f)+7.f)*.07329248f:l*10.44426855f;
 if(mode==2)return std::fmax((685.f+300.f*std::log10(l*.98921f+.01079f))/1023.f,0.f);
 if(mode==3)return l>.0078125f?(std::log2(l)+9.72f)/17.52f:10.5402377417f*l+.0729055342f;
 return l;
}
}
