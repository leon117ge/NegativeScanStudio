// Real Metal on macOS (CI: macos-14/macos-15 arm64 runners, paravirtual Metal device).
// Build: plugin + metal_render.mm with -DNSS_METAL, plus metal_test_support.mm. See macos-metal.yml.
// 1. Metal vs CPU parity of the production encode path (runtime-compiled embedded MSL, fast math off).
// 2. Pipeline cache: one build per device across many renders.
// 3. Forced failures (no device, compile, pipeline, encode) never poison the cache.
// 4. Plugin end to end through the mock host: Metal render on the host queue, and CPU fallback
//    plus withdrawal on every forced failure; GPU-private buffers report GPURenderFailed.
#define main nss_original_host_test
#include "host_test.cpp"
#undef main
#include <cstdio>
extern "C" {
void *nssTestQueue();void *nssTestBuffer(void *queue,size_t bytes,int shared);void *nssTestContents(void *buffer);void nssTestRelease(void *object);
const char *nssTestDeviceName(void *queue);int nssTestUnifiedMemory(void *queue);int nssTestWait(void *queue);
}
using mock::Bag;
static OfxPropertySetHandle bag(Bag &b){return reinterpret_cast<OfxPropertySetHandle>(&b);}
static constexpr double kParity=1e-4;
struct Frame {
 int W,H,padSrc,padDst;void *queue;void *src=nullptr,*dst=nullptr;float *in=nullptr,*out=nullptr;int bounds[4];
 Frame(void *q,int w,int h,bool shared=true,int ps=0,int pd=0,int x0=100,int y0=50):W(w),H(h),padSrc(ps),padDst(pd),queue(q),bounds{x0,y0,x0+w,y0+h}{
  src=nssTestBuffer(q,size_t(srcStride())*H*4,shared);dst=nssTestBuffer(q,size_t(dstStride())*H*4,shared);require(src&&dst);
  in=static_cast<float*>(nssTestContents(src));out=static_cast<float*>(nssTestContents(dst));
  if(in)for(int y=0;y<H;y++)for(int x=0;x<W;x++){float *p=in+size_t(y)*srcStride()+x*4;p[0]=float(x)/W*1.2f;p[1]=float(y)/H;p[2]=float((x*7+y*13)%W)/W;p[3]=.25f+.75f*float(x%5)/4;}
  clear();
 }
 ~Frame(){nssTestRelease(src);nssTestRelease(dst);}
 int srcStride() const {return W*4+padSrc;}int dstStride() const {return W*4+padDst;}
 void clear(){if(out)for(size_t i=0;i<size_t(dstStride())*H;i++)out[i]=-7.f;}
 double worst(const nss::Prepared &prep,const int *win,bool premult) const {
  double w=0;for(int y=bounds[1];y<bounds[3];y++)for(int x=bounds[0];x<bounds[2];x++){
   const float *i=in+size_t(y-bounds[1])*srcStride()+(x-bounds[0])*4;const float *o=out+size_t(y-bounds[1])*dstStride()+(x-bounds[0])*4;float e[4]={-7.f,-7.f,-7.f,-7.f};
   if(x>=win[0]&&x<win[2]&&y>=win[1]&&y<win[3])prep.rgba(i,e,premult);
   for(int c=0;c<4;c++){double d=std::fabs(double(o[c])-e[c]);if(!std::isfinite(o[c]))d=1e9;w=std::max(w,d);}}
  return w;
 }
};
static nss::MetalArgs argsFor(const Frame &f,const int *win,bool premult){return {f.bounds[0],f.bounds[1],f.bounds[2],f.bounds[3],f.srcStride(),f.bounds[0],f.bounds[1],f.bounds[2],f.bounds[3],f.dstStride(),win[0],win[1],win[2]-win[0],win[3]-win[1],int(premult)};}
static std::string lastRender(OfxImageEffectHandle e){std::lock_guard<std::mutex> lock(instance(e)->mutex);return instance(e)->lastRender;}
struct Plugin {
 mock::Effect fx;OfxImageEffectHandle e=reinterpret_cast<OfxImageEffectHandle>(&fx);OfxParamSetHandle s=nullptr;
 Plugin(){require(entry(kOfxActionDescribe,e,nullptr,nullptr)==kOfxStatOK);require(entry(kOfxImageEffectActionDescribeInContext,e,nullptr,nullptr)==kOfxStatOK);
  require(entry(kOfxActionCreateInstance,e,nullptr,nullptr)==kOfxStatOK);s=params(e);setI(s,"auto_metadata",0);setI(s,"camera_on",0);}
 ~Plugin(){entry(kOfxActionDestroyInstance,e,nullptr,nullptr);}
 std::string metalProp(){return str(properties(e),kOfxImageEffectPropMetalRenderSupported);}
 void attach(Frame &f){for(auto *b:{&fx.source.image,&fx.output.image}){auto p=bag(*b);mock::ss(p,kOfxImageEffectPropPixelDepth,0,kOfxBitDepthFloat);mock::ss(p,kOfxImageEffectPropComponents,0,kOfxImageComponentRGBA);mock::ss(p,kOfxImageEffectPropPreMultiplication,0,kOfxImageUnPreMultiplied);for(int c=0;c<4;c++)mock::si(p,kOfxImagePropBounds,c,f.bounds[c]);}
  mock::si(bag(fx.source.image),kOfxImagePropRowBytes,0,f.srcStride()*4);mock::si(bag(fx.output.image),kOfxImagePropRowBytes,0,f.dstStride()*4);
  mock::sp(bag(fx.source.image),kOfxImagePropData,0,f.src);mock::sp(bag(fx.output.image),kOfxImagePropData,0,f.dst);}
 Bag args(void *queue,const int *win){Bag a;mock::sd(bag(a),kOfxPropTime,0,0);for(int c=0;c<4;c++)mock::si(bag(a),kOfxImageEffectPropRenderWindow,c,win[c]);mock::si(bag(a),kOfxImageEffectPropMetalEnabled,0,1);mock::sp(bag(a),kOfxImageEffectPropMetalCommandQueue,0,queue);return a;}
};
int main(){
 auto &api=nss::MetalApi::get();
 std::printf("%s\n",api.availability.c_str());
 void *queue=api.available?nssTestQueue():nullptr;
 if(!queue){
  if(std::getenv("NSS_METAL_REQUIRE_DEVICE")){std::printf("FAIL: no Metal device but NSS_METAL_REQUIRE_DEVICE is set\n");return 1;}
  std::printf("SKIP: no Metal device\n");return 0;
 }
 std::printf("Metal device: %s, unified memory: %d\n",nssTestDeviceName(queue),nssTestUnifiedMemory(queue));
 auto shared=std::make_shared<nss::MetalStatus>();
 // 1. Parity of the production encode path on dense ramps: 5 pipeline variants, a full frame with
 // padded rows, and a window that is offset and partly outside the destination.
 const int W=1024,H=256;double overall=0;
 for(int variant=0;variant<5;variant++){
  nss::Pipeline pipe;if(variant==1)pipe.stage=0;if(variant==2){pipe.tone.shoulder_on=0;pipe.tone.toe_on=0;}if(variant==3)pipe.density.in_di=1;
  bool premult=variant==4;nss::Prepared prep(pipe);Frame f(queue,W,H,true,variant*4,variant*8);int win[4]={f.bounds[0],f.bounds[1],f.bounds[2],f.bounds[3]};
  if(variant==4){win[0]+=17;win[1]+=9;win[2]+=40;win[3]-=3;}
  std::string device,error;require(api.encode(queue,f.src,f.dst,prep.values.data(),prep.values.size(),argsFor(f,win,premult),shared,device,error));
  require(nssTestWait(queue)==1&&shared->failures.load()==0);
  int clipped[4]={win[0],win[1],std::min(win[2],f.bounds[2]),std::min(win[3],f.bounds[3])};double w=f.worst(prep,clipped,premult);
  std::printf("variant %d: worst abs %.3g (limit %.0e)\n",variant,w,kParity);require(w<=kParity);overall=std::max(overall,w);
 }
 int builds=api.pipelineBuilds.load();require(builds==1);
 // 2. Forced failures: each returns false with a reason, nothing is committed, cache stays good.
 {nss::Pipeline pipe;nss::Prepared prep(pipe);Frame f(queue,64,32);int win[4]={f.bounds[0],f.bounds[1],f.bounds[2],f.bounds[3]};
  const char *expect[]={"","no device","compile","pipeline","encode"};
  for(int fault=nss::kMetalFaultNoDevice;fault<=nss::kMetalFaultEncode;fault++){api.fault=fault;std::string device,error;bool ok=api.encode(queue,f.src,f.dst,prep.values.data(),prep.values.size(),argsFor(f,win,false),shared,device,error);api.fault=nss::kMetalFaultNone;
   std::printf("forced %s: %s\n",expect[fault],error.c_str());require(!ok&&error.find(expect[fault])!=std::string::npos);}
  std::string device,error;require(!api.encode(nullptr,f.src,f.dst,prep.values.data(),prep.values.size(),argsFor(f,win,false),shared,device,error)&&!error.empty());
  require(api.encode(queue,f.src,f.dst,prep.values.data(),prep.values.size(),argsFor(f,win,false),shared,device,error));require(nssTestWait(queue)==1&&f.worst(prep,win,false)<=kParity);
  require(api.pipelineBuilds.load()==builds);}
 // 3. Plugin end to end through the mock host.
 mock::init();OfxHost host{nullptr,mock::fetch};setHost(&host);check(entry(kOfxActionLoad,nullptr,nullptr,nullptr));
 // macOS build: Rendering controls visible, Render engine exactly Auto (Metal) | CPU (indices 0/1, default Auto).
 {Plugin p;auto &v=p.fx.params.values;auto visible=[&](const char *k){auto &b=v[k];auto a=b.ints.find(kOfxParamPropSecret);return v.count(k)&&(a==b.ints.end()||a->second.empty()||a->second[0]==0);};
  require(NSS_MAC_RENDER_UI==1&&visible("render")&&visible("render_device")&&visible("render_info"));
  require(v["render_device"].strings[kOfxParamPropChoiceOption]==std::vector<std::string>{"Auto (Metal)","CPU"}&&v["render_device"].ints[kOfxParamPropDefault][0]==0);
  require(v["render_info"].type==kOfxParamTypePushButton);std::printf("macOS UI: Render engine [Auto (Metal) | CPU] and Show render engine visible\n");}
 {Plugin p;require(p.metalProp()=="true");Frame f(queue,333,77,true,12,0);p.attach(f);int win[4]={f.bounds[0],f.bounds[1],f.bounds[2],f.bounds[3]};auto a=p.args(queue,win);
  for(int r=0;r<3;r++)require(entry(kOfxImageEffectActionRender,p.e,bag(a),nullptr)==kOfxStatOK);
  require(lastRender(p.e).rfind("Metal (",0)==0&&lastRender(p.e).find("), host queue buffers")!=std::string::npos);require(nssTestWait(queue)==1);
  double w=f.worst(nss::Prepared(readParams(p.s,0)),win,false);std::printf("plugin Metal render: worst abs %.3g\n",w);require(w<=kParity&&p.metalProp()=="true");overall=std::max(overall,w);
  require(api.pipelineBuilds.load()==builds);}
 for(int fault=nss::kMetalFaultNoDevice;fault<=nss::kMetalFaultEncode;fault++){
  Plugin p;Frame f(queue,129,33);p.attach(f);int win[4]={f.bounds[0],f.bounds[1],f.bounds[2],f.bounds[3]};auto a=p.args(queue,win);api.fault=fault;
  require(entry(kOfxImageEffectActionRender,p.e,bag(a),nullptr)==kOfxStatOK);api.fault=nss::kMetalFaultNone;
  auto text=lastRender(p.e);require(text.rfind("Metal failed",0)==0&&text.find("CPU fallback on shared Metal buffers")!=std::string::npos);
  require(f.worst(nss::Prepared(readParams(p.s,0)),win,false)<=1e-6);require(p.metalProp()=="false"&&instance(p.e)->metalWithdrawn);
 }
 {Plugin p;Frame f(queue,64,16,false);p.attach(f);int win[4]={f.bounds[0],f.bounds[1],f.bounds[2],f.bounds[3]};auto a=p.args(queue,win);api.fault=nss::kMetalFaultEncode;
  require(entry(kOfxImageEffectActionRender,p.e,bag(a),nullptr)==kOfxStatGPURenderFailed);api.fault=nss::kMetalFaultNone;require(p.metalProp()=="false");}
 nssTestRelease(queue);
 std::printf("PASS: real Metal parity worst abs %.3g (limit %.0e), %d pipeline build(s) per device, 4 forced failures fall back to CPU and withdraw Metal, private buffers report GPURenderFailed\n",overall,kParity,builds);
 return 0;
}
