#pragma once
#include "dctl_math.hpp"
#include <array>
#include "wb_presets.hpp"
namespace nss {
struct Pipeline {
 Density density; Positive positive;
 int cameraOn=1, neutralise=0,stage=1;
 float cameraStrength=1;
 std::array<float,9> camera{1,0,0,0,1,0,0,0,1},neutral{1,0,0,0,1,0,0,0,1};
 Pipeline(){density.base_r=density.base_g=density.base_b=.5f;}
 Vec3 cameraPixel(Vec3 v) const {
  if(!cameraOn)return v;
  auto &m=neutralise?neutral:camera;
  float k=std::fmin(std::fmax(cameraStrength,0.f),1.f);
  return {v.x+k*(m[0]*v.x+m[1]*v.y+m[2]*v.z-v.x),
   v.y+k*(m[3]*v.x+m[4]*v.y+m[5]*v.z-v.y),
   v.z+k*(m[6]*v.x+m[7]*v.y+m[8]*v.z-v.z)};
 }
 // Direct DCTL reference, retained for tests, not used in the render loop.
 Vec3 pixel(Vec3 v) {
  auto d=density;
  if(d.in_di) {auto lin=[](float x){return x>.02740668f?std::exp2(x/.07329248f-7.f)-.0075f:x/10.44426855f;};v={lin(v.x),lin(v.y),lin(v.z)};d.in_di=0;}
  v=cameraPixel(v);v=d.transform(0,0,0,0,v.x,v.y,v.z);
  if(stage==0||d.out_trans)return v;
  return positive.transform(0,0,0,0,v.x,v.y,v.z);
 }
};
inline Vec3 balancedBase(Vec3 baseline,int film,int temperature){
 if(film<0||film>1||temperature<0||temperature>4)return baseline;
 // D=-log10(pixel/base). Adding cast is equivalent to base*=10^(+cast).
 auto &offset=wbOffsets[film][temperature];
 return {baseline.x*std::pow(10.f,offset[0]),baseline.y*std::pow(10.f,offset[1]),baseline.z*std::pow(10.f,offset[2])};
}
}
