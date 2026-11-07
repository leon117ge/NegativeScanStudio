#pragma once
#include "pipeline.hpp"
#define NSS_EXP std::exp
#define NSS_EXP2 std::exp2
#define NSS_LOG std::log
#define NSS_LOG2 std::log2
#define NSS_POW std::pow
#define NSS_TANH std::tanh
#define NSS_MIN std::fmin
#define NSS_MAX std::fmax
#include "render_math.h"
#undef NSS_EXP
#undef NSS_EXP2
#undef NSS_LOG
#undef NSS_LOG2
#undef NSS_POW
#undef NSS_TANH
#undef NSS_MIN
#undef NSS_MAX
#undef NSS_PARAMS
namespace nss {
struct Prepared {
 std::array<float,40> values{};
 explicit Prepared(const Pipeline &pipe){
  const auto &d=pipe.density;const auto &o=pipe.positive;auto &p=values;
  p[0]=float(d.in_di);p[1]=float(d.out_trans);p[2]=float(d.clamp01);p[3]=d.dmin_code/d.full_code;
  p[4]=-1.f/(2.302585093f*d.dens_step*d.full_code);p[5]=std::exp2(d.ev_stops);
  p[6]=d.base_r;p[7]=d.base_g;p[8]=d.base_b;p[9]=d.blk_r;p[10]=d.blk_g;p[11]=d.blk_b;
  p[12]=float(pipe.stage);p[13]=float(o.out_mode);p[14]=float(o.contrast);p[15]=(o.white_code-95.f)*.002f;
  p[16]=o.gam_r;p[17]=o.gam_g;p[18]=o.gam_b;
  for(int c=0;c<3;c++)p[19+c]=std::exp(-2.302585f*p[15]/p[16+c]);
  float ar=0,ab=0; // Film Base loader already wrote visible bal_r/g/b.
  if(o.gray_on){
   float gray[]={o.gray_r,o.gray_g,o.gray_b},s[3];
   for(int c=0;c<3;c++){float v=2.302585f*((gray[c]-95.f)*.002f-p[15])/p[16+c];s[c]=std::fmax((std::exp(std::fmin(std::fmax(v,-30.f),20.f))-p[19+c])/(1.f-p[19+c]),.0001f);}
   ar=std::log2(s[1]/s[0]);ab=std::log2(s[1]/s[2]);
  }
  p[22]=std::exp2(o.expo+o.bal_r+ar);p[23]=std::exp2(o.expo+o.bal_g);p[24]=std::exp2(o.expo+o.bal_b+ab);
  p[25]=o.contrast==1?1.10f:(o.contrast==2?1.25f:1.40f);p[26]=o.contrast==1?.70f:(o.contrast==2?.55f:.45f);
  auto &m=pipe.neutralise?pipe.neutral:pipe.camera;bool identity=true;
  for(int c=0;c<9;c++){p[28+c]=m[c];if(m[c]!=(c%4==0?1.f:0.f))identity=false;}
  p[27]=float(pipe.cameraOn&&!identity&&pipe.cameraStrength!=0.f);p[37]=std::fmin(std::fmax(pipe.cameraStrength,0.f),1.f);
 }
 Vec3 pixel(Vec3 v) const {
  const float *p=values.data();
  if(p[0]!=0.f)v={nss_di_decode(v.x),nss_di_decode(v.y),nss_di_decode(v.z)};
  if(p[27]!=0.f)v={v.x+p[37]*(p[28]*v.x+p[29]*v.y+p[30]*v.z-v.x),v.y+p[37]*(p[31]*v.x+p[32]*v.y+p[33]*v.z-v.y),v.z+p[37]*(p[34]*v.x+p[35]*v.y+p[36]*v.z-v.z)};
  return {nss_channel(v.x,p,0),nss_channel(v.y,p,1),nss_channel(v.z,p,2)};
 }
 void rgba(const float *in,float *out,bool premult) const {
  float a=in[3];Vec3 v{in[0],in[1],in[2]};
  if(premult&&a!=0.f){v.x/=a;v.y/=a;v.z/=a;}
  v=pixel(v);if(premult){v.x*=a;v.y*=a;v.z*=a;}
  out[0]=v.x;out[1]=v.y;out[2]=v.z;out[3]=a;
 }
};
}
