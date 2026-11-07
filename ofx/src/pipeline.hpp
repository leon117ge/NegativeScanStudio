#pragma once
#include "dctl_math.hpp"
#include <array>
namespace nss {
struct Pipeline {
 Density density; Cross cross; Positive positive;
 int cameraOn=1, neutralise=0,stage=2;
 float cameraStrength=1;
 std::array<float,9> camera{1,0,0,0,1,0,0,0,1},neutral{1,0,0,0,1,0,0,0,1};
 Vec3 cameraPixel(Vec3 v) const {
  if(!cameraOn)return v;
  auto &m=neutralise?neutral:camera;
  float k=std::fmin(std::fmax(cameraStrength,0.f),1.f);
  return {v.x+k*(m[0]*v.x+m[1]*v.y+m[2]*v.z-v.x),
   v.y+k*(m[3]*v.x+m[4]*v.y+m[5]*v.z-v.y),
   v.z+k*(m[6]*v.x+m[7]*v.y+m[8]*v.z-v.z)};
 }
 Vec3 pixel(Vec3 v) {
  // DCTL input DI decoding belongs before the LINEAR camera calibration.
  if(density.in_di && cameraOn) {
   auto lin=[](float x){return x>.02740668f?std::exp2(x/.07329248f-7.f)-.0075f:x/10.44426855f;};
   v={lin(v.x),lin(v.y),lin(v.z)};
   v=cameraPixel(v);
   auto d=density; d.in_di=0;v=d.transform(0,0,0,0,v.x,v.y,v.z);
  } else {v=cameraPixel(v);v=density.transform(0,0,0,0,v.x,v.y,v.z);}
  if(stage==0 || density.out_trans)return v;
  v=cross.transform(0,0,0,0,v.x,v.y,v.z);
  if(stage==1)return v;
  return positive.transform(0,0,0,0,v.x,v.y,v.z);
 }
};
}
