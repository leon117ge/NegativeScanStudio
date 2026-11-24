// Tone stage (0.90.12) math tests: off parity, reference agreement, monotonic ramps, hue, no clipping, C1 joins.
#include "../src/prepared.hpp"
#include <cstring>
#include <iostream>
#include <random>
using namespace nss;
static int failures=0;
static void expect(bool ok,const char *what){if(!ok){std::cerr<<"FAIL: "<<what<<"\n";++failures;}}
static float hue(float r,float g,float b){float mx=std::fmax(r,std::fmax(g,b)),mn=std::fmin(r,std::fmin(g,b)),d=mx-mn;if(d<=0)return 0;
 float h=mx==r?std::fmod((g-b)/d+6.f,6.f):(mx==g?(b-r)/d+2.f:(r-g)/d+4.f);return h*60.f;}
static float hueDiff(float a,float b){float d=std::fabs(a-b);return std::fmin(d,360.f-d);}
int main(){
 std::mt19937 rng(9012);std::uniform_real_distribution<float> u(0.f,1.f);
 const Tone off=tonePresets[kTonePresetCount-1].tone;
 // 1. Off (and strength 0) is bit-identical to the 0.90.11 per-channel path for every output and contrast.
 for(int mode=0;mode<5;mode++)for(int contrast=0;contrast<4;contrast++)for(int variant=0;variant<2;variant++){
  Pipeline p;p.cameraOn=0;p.positive.out_mode=mode;p.positive.contrast=contrast;p.tone=off;
  if(variant){p.tone=tonePresets[0].tone;p.tone.shoulder_strength=0;p.tone.toe_strength=0;}
  Prepared q(p);expect(!nss_tone_active(q.values.data()),"tone inactive when off");
  for(int i=0;i<4000;i++){float v[3]={u(rng)*u(rng),u(rng)*u(rng),u(rng)*u(rng)};auto o=q.pixel({v[0],v[1],v[2]});
   float e[3]={nss_channel(v[0],q.values.data(),0),nss_channel(v[1],q.values.data(),1),nss_channel(v[2],q.values.data(),2)};
   expect(std::memcmp(&o.x,&e[0],4)==0&&std::memcmp(&o.y,&e[1],4)==0&&std::memcmp(&o.z,&e[2],4)==0,"off bitwise");
   auto r=p.pixel({v[0],v[1],v[2]});expect(std::fabs(r.x-o.x)<=1e-6f*std::fmax(1.f,std::fabs(r.x))&&std::fabs(r.y-o.y)<=1e-6f*std::fmax(1.f,std::fabs(r.y))&&std::fabs(r.z-o.z)<=1e-6f*std::fmax(1.f,std::fabs(r.z)),"off vs DCTL reference");}
 }
 // Density stage and transmittance output are never toned.
 {Pipeline p;p.stage=0;Prepared q(p);expect(!nss_tone_active(q.values.data()),"density stage untoned");
  Pipeline t;t.density.out_trans=1;Prepared w(t);expect(!nss_tone_active(w.values.data()),"transmittance untoned");}
 // 2. Every preset and output: kernel math agrees with the independent stops-based reference.
 float worst=0;
 for(int pr=0;pr<kTonePresetCount;pr++)for(int mode=0;mode<5;mode++)for(int contrast:{0,2}){
  Pipeline p;p.cameraOn=0;p.positive.out_mode=mode;p.positive.contrast=contrast;p.tone=tonePresets[pr].tone;Prepared q(p);
  for(int i=0;i<4000;i++){float v[3]={u(rng)*u(rng),u(rng)*u(rng),u(rng)*u(rng)};auto o=q.pixel({v[0],v[1],v[2]});auto r=p.pixel({v[0],v[1],v[2]});
   for(auto d:{std::fabs(o.x-r.x)/std::fmax(1.f,std::fabs(r.x)),std::fabs(o.y-r.y)/std::fmax(1.f,std::fabs(r.y)),std::fabs(o.z-r.z)/std::fmax(1.f,std::fabs(r.z))})worst=std::fmax(worst,d);}
 }
 expect(worst<2e-6f,"tone vs reference");
 // 3. 16-bit ramp. Monotonic tone curve on any fixed R:G:B (hue line), and monotonic mean on the real input ramp.
 for(int pr=0;pr<kTonePresetCount;pr++){
  Pipeline p;p.cameraOn=0;p.tone=tonePresets[pr].tone;p.positive.out_mode=4;Prepared q(p);const float *k=q.values.data();
  const float ratios[][3]={{1,1,1},{1,.2f,.05f},{.05f,1,.3f},{.2f,.3f,1},{1,.9f,.1f}};
  for(auto &c:ratios){float prev[3]={-1,-1,-1};
   for(int i=0;i<=65535;i++){float n0=64.f*std::pow(i/65535.f,4.f),m=(c[0]+c[1]+c[2])/3.f,l[3]={c[0]/m*n0,c[1]/m*n0,c[2]/m*n0};
    float g=nss_shoulder_gain(nss_tone_norm(l[0],l[1],l[2]),k);for(auto &x:l)x*=g;float t=nss_toe_offset(nss_tone_min(l[0],l[1],l[2]),k);
    for(int ch=0;ch<3;ch++){float y=l[ch]+t;expect(y>=prev[ch]*(1.f-2e-6f),"hue-line ramp monotonic (float rounding only)");expect(y>=0.f&&std::isfinite(y),"finite non-negative");prev[ch]=y;}}}
  for(int mode=0;mode<5;mode++){Pipeline r=p;r.positive.out_mode=mode;Prepared w(r);float prevMean=1e30f;
   for(int i=0;i<=65535;i++){float v=i/65535.f;auto o=w.pixel({v,v,v});if(mode==4){float m=(o.x+o.y+o.z)/3.f;expect(m<=prevMean,"input ramp mean monotonic");prevMean=m;}
    expect(std::isfinite(o.x)&&std::isfinite(o.y)&&std::isfinite(o.z),"ramp finite");
    if(mode==2&&tonePresets[pr].tone.shoulder_on)expect(o.x<1.f&&o.y<1.f&&o.z<1.f,"shoulder keeps Cineon below 1023");}}
 }
 // Per-channel reversal on the real 16-bit input ramp (channel ratios drift with the per-channel stock gammas) stays small.
 for(int pr=0;pr<kTonePresetCount;pr++){Pipeline p;p.cameraOn=0;p.tone=tonePresets[pr].tone;p.positive.out_mode=4;Prepared q(p);
  float peak[3]={0,0,0},drop=0;for(int i=65535;i>=1;i--){float v=i/65535.f;auto o=q.pixel({v,v,v});float a[3]={o.x,o.y,o.z};
   for(int c=0;c<3;c++){peak[c]=std::fmax(peak[c],a[c]);if(a[c]>0)drop=std::fmax(drop,std::log2(peak[c]/a[c]));}}
  std::cout<<"  "<<tonePresets[pr].name<<": max per-channel reversal on default 16-bit ramp "<<drop<<" stops\n";expect(drop<.25f,"per-channel reversal bounded");}
 // 4. Hue: the shoulder scales R,G,B together and the toe adds the same amount to each, so HSV hue is unchanged in scene linear.
 float hueWorst=0;
 for(int pr=0;pr<kTonePresetCount-1;pr++){Pipeline p;p.cameraOn=0;p.tone=tonePresets[pr].tone;p.positive.out_mode=4;Prepared q(p);Pipeline o=p;o.tone=off;Prepared z(o);
  for(int i=0;i<20000;i++){float v[3]={.002f+.5f*u(rng),.002f+.5f*u(rng),.002f+.5f*u(rng)};auto a=q.pixel({v[0],v[1],v[2]}),b=z.pixel({v[0],v[1],v[2]});
   float mx=std::fmax(b.x,std::fmax(b.y,b.z)),mn=std::fmin(b.x,std::fmin(b.y,b.z));if(mx<=0||(mx-mn)/mx<.3f||mx-mn<1e-3f)continue;
   hueWorst=std::fmax(hueWorst,hueDiff(hue(a.x,a.y,a.z),hue(b.x,b.y,b.z)));}}
 std::cout<<"  max HSV hue drift on saturated patches (scene linear): "<<hueWorst<<" deg\n";expect(hueWorst<.05f,"hue drift");
 // 5. C1 joins: slope in stops is 1 on both sides of each pivot.
 for(int pr=0;pr<kTonePresetCount-1;pr++){Pipeline p;p.tone=tonePresets[pr].tone;Prepared q(p);const float *k=q.values.data();auto c=toneConstants(p.tone);
  auto curve=[&](float x){float n=.18f*std::exp2(x);float g=nss_shoulder_gain(n,k);n*=g;n+=nss_toe_offset(n,k);return std::log2(n/.18f);};
  for(float x0:{c.xs,c.xt}){float e=1e-3f;float lo=(curve(x0)-curve(x0-e))/e,hi=(curve(x0+e)-curve(x0))/e;expect(std::fabs(lo-hi)<.01f,"C1 at pivot");}
  expect(c.xt<=c.xs&&c.h>0&&c.r>0,"pivot order");}
 // Black floor: pure black lifts to strength*0.18*2^black; never below 0.
 {Pipeline p;p.cameraOn=0;p.positive.out_mode=4;Prepared q(p);auto o=q.pixel({1,1,1});auto c=toneConstants(p.tone);float floor=c.ts*.18f*std::exp2(c.xt-c.r);
  expect(std::fabs(o.x-floor)<1e-6f&&std::fabs(o.y-floor)<1e-6f&&std::fabs(o.z-floor)<1e-6f,"black floor");}
 if(failures){std::cerr<<failures<<" tone failures\n";return 1;}
 std::cout<<"PASS: tone off bit-identical to 0.90.11 path, kernel vs reference (worst "<<worst<<"), 16-bit ramps, hue, no Cineon clip, C1 pivots, black floor\n";return 0;
}
