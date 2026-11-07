#pragma once
#include "cpu_render.hpp"
namespace nss {
inline bool pickRect(const int *bounds,int x,int y,int radius,int *rect){
 if(x<bounds[0]||x>=bounds[2]||y<bounds[1]||y>=bounds[3])return false;
 rect[0]=std::max(bounds[0],x-radius);rect[1]=std::max(bounds[1],y-radius);
 rect[2]=std::min(bounds[2],x+radius+1);rect[3]=std::min(bounds[3],y+radius+1);return true;
}
inline bool grayFromPatch(Pipeline &p,const FrameView &source,const int *rect,bool premult){
 Pipeline density=p;density.stage=0;density.density.out_trans=0;
 double sums[3]{};size_t count=0;
 for(int y=rect[1];y<rect[3];y++)for(int x=rect[0];x<rect[2];x++){
  const float *v=source.at(x,y);if(!v||!std::isfinite(v[3])||v[3]<=1e-8f)continue;
  float k=premult?1.f/v[3]:1.f;Vec3 rgb{v[0]*k,v[1]*k,v[2]*k};
  if(!std::isfinite(rgb.x)||!std::isfinite(rgb.y)||!std::isfinite(rgb.z))continue;
  auto code=density.pixel(rgb);sums[0]+=code.x;sums[1]+=code.y;sums[2]+=code.z;++count;
 }
 if(!count)return false;
 p.positive.gray_on=1;p.positive.gray_r=float(sums[0]*1023/count);p.positive.gray_g=float(sums[1]*1023/count);p.positive.gray_b=float(sums[2]*1023/count);return true;
}
}
