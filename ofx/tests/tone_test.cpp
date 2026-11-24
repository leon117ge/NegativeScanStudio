// Tone stage (0.90.12 shoulder/toe, 0.90.13 desaturation/cream) math tests: off parity, reference agreement,
// monotonic ramps, hue, no clipping, C1 joins, region limits.
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
static float luma(float r,float g,float b){return .2126f*r+.7152f*g+.0722f*b;}
static Tone noColor(Tone t){t.highlight_desat=0;t.highlight_cream=0;t.shadow_desat=0;return t;}
static bool same(Vec3 a,Vec3 b){return std::memcmp(&a.x,&b.x,4)==0&&std::memcmp(&a.y,&b.y,4)==0&&std::memcmp(&a.z,&b.z,4)==0;}
// 0.90.12 tone path written out (shoulder then toe, no color stage).
static Vec3 tone0912(const Prepared &q,Vec3 v){const float *p=q.values.data();
 if(!nss_tone_active(p))return {nss_channel(v.x,p,0),nss_channel(v.y,p,1),nss_channel(v.z,p,2)};
 float r=nss_linear(nss_code(v.x,p,0),p,0),g=nss_linear(nss_code(v.y,p,1),p,1),b=nss_linear(nss_code(v.z,p,2),p,2);
 float k=nss_shoulder_gain(nss_tone_norm(r,g,b),p);r*=k;g*=k;b*=k;float t=nss_toe_offset(nss_tone_min(r,g,b),p);
 return {nss_encode(r+t,p),nss_encode(g+t,p),nss_encode(b+t,p)};}
int main(){
 std::mt19937 rng(9013);std::uniform_real_distribution<float> u(0.f,1.f);
 const Tone off=tonePresets[kTonePresetCount-1].tone;
 // 1. Off (and strength 0) is bit-identical to the 0.90.11 per-channel path for every output and contrast.
 for(int mode=0;mode<5;mode++)for(int contrast=0;contrast<4;contrast++)for(int variant=0;variant<2;variant++){
  Pipeline p;p.cameraOn=0;p.positive.out_mode=mode;p.positive.contrast=contrast;p.tone=off;
  if(variant){p.tone=noColor(tonePresets[0].tone);p.tone.shoulder_strength=0;p.tone.toe_strength=0;}
  Prepared q(p);expect(!nss_tone_active(q.values.data()),"tone inactive when off");
  for(int i=0;i<4000;i++){float v[3]={u(rng)*u(rng),u(rng)*u(rng),u(rng)*u(rng)};auto o=q.pixel({v[0],v[1],v[2]});
   float e[3]={nss_channel(v[0],q.values.data(),0),nss_channel(v[1],q.values.data(),1),nss_channel(v[2],q.values.data(),2)};
   expect(std::memcmp(&o.x,&e[0],4)==0&&std::memcmp(&o.y,&e[1],4)==0&&std::memcmp(&o.z,&e[2],4)==0,"off bitwise");
   auto r=p.pixel({v[0],v[1],v[2]});expect(std::fabs(r.x-o.x)<=1e-6f*std::fmax(1.f,std::fabs(r.x))&&std::fabs(r.y-o.y)<=1e-6f*std::fmax(1.f,std::fabs(r.y))&&std::fabs(r.z-o.z)<=1e-6f*std::fmax(1.f,std::fabs(r.z)),"off vs DCTL reference");}
 }
 // 1b. Desaturation and cream at zero: bit-identical to the 0.90.12 shoulder/toe path, for every preset and output.
 for(int pr=0;pr<kTonePresetCount;pr++)for(int mode=0;mode<5;mode++){Pipeline p;p.cameraOn=0;p.positive.out_mode=mode;p.tone=noColor(tonePresets[pr].tone);Prepared q(p);
  expect(q.values[48]==0.f,"color inactive at zero");
  for(int i=0;i<4000;i++){Vec3 v{u(rng)*u(rng),u(rng)*u(rng),u(rng)*u(rng)};expect(same(q.pixel(v),tone0912(q,v)),"color zero == 0.90.12");}}
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
  const float ratios[][3]={{1,1,1},{1,.2f,.05f},{.05f,1,.3f},{.2f,.3f,1},{1,.9f,.1f}};float prevY[5]={-1,-1,-1,-1,-1};
  for(auto &c:ratios){float prev[3]={-1,-1,-1};
   for(int i=0;i<=65535;i++){float n0=64.f*std::pow(i/65535.f,4.f),m=(c[0]+c[1]+c[2])/3.f,l[3]={c[0]/m*n0,c[1]/m*n0,c[2]/m*n0};
    float g=nss_shoulder_gain(nss_tone_norm(l[0],l[1],l[2]),k);for(auto &x:l)x*=g;float t=nss_toe_offset(nss_tone_min(l[0],l[1],l[2]),k);
    for(int ch=0;ch<3;ch++){float y=l[ch]+t;expect(y>=prev[ch]*(1.f-2e-6f),"hue-line ramp monotonic (float rounding only)");expect(y>=0.f&&std::isfinite(y),"finite non-negative");prev[ch]=y;}
    // With the 0.90.13 color stage: luminance unchanged by it, so luminance stays monotonic; values stay finite and non-negative.
    float r=l[0]+t,gg=l[1]+t,b=l[2]+t,y0=luma(r,gg,b);if(k[48]!=0.f)nss_tone_color(&r,&gg,&b,k);float y1=luma(r,gg,b);
    expect(std::fabs(y1-y0)<=2e-6f*std::fmax(y0,1e-6f)+1e-12f,"color keeps luminance");expect(y1>=prevY[&c-ratios]*(1.f-2e-6f),"luminance ramp monotonic");prevY[&c-ratios]=y1;
    expect(r>=0.f&&gg>=0.f&&b>=0.f&&std::isfinite(r)&&std::isfinite(gg)&&std::isfinite(b),"color finite non-negative");}}
  for(int mode=0;mode<5;mode++){Pipeline r=p;r.positive.out_mode=mode;Prepared w(r);Pipeline r0=r;r0.tone=noColor(r.tone);Prepared w0(r0);float prevMean=1e30f;
   for(int i=0;i<=65535;i++){float v=i/65535.f;auto o=w.pixel({v,v,v});if(mode==4){auto o0=w0.pixel({v,v,v});float m=(o0.x+o0.y+o0.z)/3.f;expect(m<=prevMean*(1.f+2e-6f),"input ramp mean monotonic (shoulder/toe)");prevMean=m;}
    expect(std::isfinite(o.x)&&std::isfinite(o.y)&&std::isfinite(o.z),"ramp finite");
    if(mode==2&&tonePresets[pr].tone.shoulder_on)expect(o.x<1.f&&o.y<1.f&&o.z<1.f,"shoulder keeps Cineon below 1023");}}
 }
 // Luminance on the real 16-bit input ramp: the color stage keeps Y, so its dips equal the 0.90.12 ones (channel drift, < 0.01 stop).
 for(int pr=0;pr<kTonePresetCount;pr++){Pipeline p;p.cameraOn=0;p.tone=tonePresets[pr].tone;p.positive.out_mode=4;Prepared q(p);Pipeline n0=p;n0.tone=noColor(p.tone);Prepared w(n0);
  float pk=0,pk0=0,dip=0,dip0=0;for(int i=65535;i>=0;i--){float v=i/65535.f;auto o=q.pixel({v,v,v}),o0=w.pixel({v,v,v});float y=luma(o.x,o.y,o.z),y0=luma(o0.x,o0.y,o0.z);
   pk=std::fmax(pk,y);pk0=std::fmax(pk0,y0);if(y>0)dip=std::fmax(dip,std::log2(pk/y));if(y0>0)dip0=std::fmax(dip0,std::log2(pk0/y0));}
  std::cout<<"  "<<tonePresets[pr].name<<": luminance dip on default 16-bit ramp "<<dip<<" stops (without color stage "<<dip0<<")\n";expect(dip<.03f&&dip<=dip0+1e-4f,"ramp luminance dip bounded");}
 // Per-channel reversal on the real 16-bit input ramp (channel ratios drift with the per-channel stock gammas) stays small.
 for(int pr=0;pr<kTonePresetCount;pr++){Pipeline p;p.cameraOn=0;p.tone=tonePresets[pr].tone;p.positive.out_mode=4;Prepared q(p);
  float peak[3]={0,0,0},drop=0;for(int i=65535;i>=1;i--){float v=i/65535.f;auto o=q.pixel({v,v,v});float a[3]={o.x,o.y,o.z};
   for(int c=0;c<3;c++){peak[c]=std::fmax(peak[c],a[c]);if(a[c]>0)drop=std::fmax(drop,std::log2(peak[c]/a[c]));}}
  std::cout<<"  "<<tonePresets[pr].name<<": max per-channel reversal on default 16-bit ramp "<<drop<<" stops\n";expect(drop<.35f,"per-channel reversal bounded");}
 // 4. Hue: the shoulder scales R,G,B together and the toe adds the same amount to each, so HSV hue is unchanged in scene linear.
 float hueWorst=0;
 for(int pr=0;pr<kTonePresetCount-1;pr++){Pipeline p;p.cameraOn=0;p.tone=tonePresets[pr].tone;p.tone.highlight_cream=0;p.positive.out_mode=4;Prepared q(p);Pipeline o=p;o.tone=off;Prepared z(o);
  for(int i=0;i<20000;i++){float v[3]={.002f+.5f*u(rng),.002f+.5f*u(rng),.002f+.5f*u(rng)};auto a=q.pixel({v[0],v[1],v[2]}),b=z.pixel({v[0],v[1],v[2]});
   float mx=std::fmax(b.x,std::fmax(b.y,b.z)),mn=std::fmin(b.x,std::fmin(b.y,b.z));if(mx<=0||(mx-mn)/mx<.3f||mx-mn<1e-3f)continue;
   hueWorst=std::fmax(hueWorst,hueDiff(hue(a.x,a.y,a.z),hue(b.x,b.y,b.z)));}}
 std::cout<<"  max HSV hue drift on saturated patches (scene linear, cream 0): "<<hueWorst<<" deg\n";expect(hueWorst<.05f,"hue drift");
 // 4b. Cream: neutral highlights turn warm (hue 0..60), saturated hues move a bounded amount and never flip,
 // nothing changes below Highlight color start, and desaturation only acts in the two tails.
 {float creamShift=0;int warm=0,neutralChecked=0;
  for(int pr=0;pr<kTonePresetCount-1;pr++){Pipeline p;p.cameraOn=0;p.tone=tonePresets[pr].tone;p.positive.out_mode=4;Prepared q(p);Pipeline nc=p;nc.tone.highlight_cream=0;Prepared z(nc);
   Pipeline n0=p;n0.tone=noColor(p.tone);Prepared w(n0);auto c=toneConstants(p.tone);
   for(int i=0;i<40000;i++){float s=u(rng)*u(rng);Vec3 v{s*(.5f+u(rng)),s*(.5f+u(rng)),s*(.5f+u(rng))};if(i%4==0)v={s,s,s};
    auto a=q.pixel(v),b=z.pixel(v),base=w.pixel(v);float n=(base.x+base.y+base.z)/3.f,x=std::log2(std::fmax(n,1e-30f)/.18f);
    if(x<=c.hs)expect(same(a,b),"cream only above highlight start");
    if(x<c.hs&&x>c.ss2)expect(same(a,base),"color stage only in tails");
    float mx=std::fmax(b.x,std::fmax(b.y,b.z)),mn=std::fmin(b.x,std::fmin(b.y,b.z));
    if(mx>0&&(mx-mn)/mx>.3f)creamShift=std::fmax(creamShift,hueDiff(hue(a.x,a.y,a.z),hue(b.x,b.y,b.z)));}
   const float *k=q.values.data();for(int i=0;i<=200;i++){float n=.18f*std::exp2(c.hs+.05f+i*.03f),r=n,g=n,b=n;nss_tone_color(&r,&g,&b,k);
    ++neutralChecked;float h=hue(r,g,b);if(h>0.f&&h<60.f&&r>g&&g>b&&std::fabs(luma(r,g,b)-n)<=2e-6f*n)++warm;}}
  std::cout<<"  cream: "<<warm<<"/"<<neutralChecked<<" neutral highlights warm; max hue move on saturated patches "<<creamShift<<" deg\n";
  expect(neutralChecked>0&&warm==neutralChecked,"neutral highlights turn warm");expect(creamShift<20.f,"cream hue move bounded (saturation > 0.3)");}
 // 5. C1 joins: slope in stops is 1 on both sides of each pivot.
 for(int pr=0;pr<kTonePresetCount-1;pr++){Pipeline p;p.tone=tonePresets[pr].tone;Prepared q(p);const float *k=q.values.data();auto c=toneConstants(p.tone);
  auto curve=[&](float x){float n=.18f*std::exp2(x);float g=nss_shoulder_gain(n,k);n*=g;n+=nss_toe_offset(n,k);return std::log2(n/.18f);};
  for(float x0:{c.xs,c.xt}){float e=1e-3f;float lo=(curve(x0)-curve(x0-e))/e,hi=(curve(x0+e)-curve(x0))/e;expect(std::fabs(lo-hi)<.01f,"C1 at pivot");}
  expect(c.xt<=c.xs&&c.h>0&&c.r>0,"pivot order");}
 // Black floor: pure black lifts to strength*0.18*2^black; never below 0.
 {Pipeline p;p.cameraOn=0;p.positive.out_mode=4;Prepared q(p);auto o=q.pixel({1,1,1});auto c=toneConstants(p.tone);float floor=c.ts*.18f*std::exp2(c.xt-c.r);
  expect(std::fabs(o.x-floor)<1e-6f&&std::fabs(o.y-floor)<1e-6f&&std::fabs(o.z-floor)<1e-6f,"black floor");}
 if(failures){std::cerr<<failures<<" tone failures\n";return 1;}
 std::cout<<"PASS: tone off bit-identical to 0.90.11 path, color zero bit-identical to 0.90.12, kernel vs reference (worst "<<worst<<"), 16-bit ramps, luminance, hue, cream/desat regions, no Cineon clip, C1 pivots, black floor\n";return 0;
}
