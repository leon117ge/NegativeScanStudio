// Metal render contract on any OS: mocked Metal API (host queue, host buffers, shared storage)
// plus the exact embedded MSL kernel compiled as host C++ (written by check_metal_source.py).
// Covers the Metal property, render routing, no-wait encode, CPU fallback and withdrawal on
// every forced failure, async command-buffer errors, private buffers and the picker.
// NOT Metal execution: metal_plugin_test.cpp runs the real Metal path on macOS CI.
// The Metal contract is a Metal build: Render engine reads 'Auto (Metal)|CPU' when the macOS UI is on
// (ofx/src/platform.hpp). Set here so the mock build needs no -DNSS_METAL (which would link metal_render.mm).
#ifndef NSS_RENDER_AUTO_METAL
#define NSS_RENDER_AUTO_METAL 1
#endif
#define main nss_original_host_test
#include "host_test.cpp"
#undef main
#include <metal_stdlib>
#define kernel
#define device
#define constant
#define thread
namespace emu {
#include "nss_render.metal"
}
#undef kernel
#undef device
#undef constant
#undef thread
static_assert(sizeof(emu::NssArgs)==sizeof(nss::MetalArgs),"argument block layout");
namespace fake {
struct Buffer {float *data;size_t floats;bool shared;};
int queue=0,encodes=0,drains=0,throws=0;bool throwNext=false;
std::vector<std::pair<unsigned,unsigned>> groups;
bool encode(void *q,void *s,void *d,const float *params,size_t count,const nss::MetalArgs &args,const std::shared_ptr<nss::MetalStatus>&,std::string &name,std::string &error){
 require(q==&queue&&count==56);
 if(throwNext){throwNext=false;++throws;throw std::runtime_error("mock Metal exception");}
 switch(nss::MetalApi::get().fault.load()){
  case nss::kMetalFaultNoDevice:error="Metal: no device";return false;
  case nss::kMetalFaultCompile:error="Metal compile failed: mock";return false;
  case nss::kMetalFaultPipeline:error="Metal pipeline failed: mock";return false;
  case nss::kMetalFaultEncode:error="Metal encode failed (forced by test)";return false;
 }
 ++encodes;name="Mock Metal";
 auto src=static_cast<Buffer*>(s),dst=static_cast<Buffer*>(d);emu::NssArgs a;std::memcpy(&a,&args,sizeof(a));
 // Whole threadgroups like metal_render.mm: the kernel must ignore threads outside the window.
 const unsigned tw=32,th=8,gx=(unsigned(args.width)+tw-1)/tw,gy=(unsigned(args.height)+th-1)/th;groups.push_back({gx,gy});
 for(unsigned y=0;y<gy*th;y++)for(unsigned x=0;x<gx*tw;x++)emu::nss_render_metal(src->data,dst->data,params,a,uint2{x,y});
 return true;
}
void *contents(void *b){auto buffer=static_cast<Buffer*>(b);return buffer&&buffer->shared?buffer->data:nullptr;}
bool drain(void *q){require(q==&queue);++drains;return true;}
void install(bool available=true){auto &api=nss::MetalApi::get();api.available=available;api.availability=available?"Metal: mock":"Metal: not built in";api.encode=encode;api.contents=contents;api.drain=drain;api.fault=nss::kMetalFaultNone;}
}
using mock::Bag;
// Captures the Show render engine message (the mock host has no message suite of its own).
static std::string shown;
static OfxStatus captureMessage(void*,const char*,const char*,const char *format,...){va_list ap;va_start(ap,format);char text[4096];std::vsnprintf(text,sizeof text,format,ap);va_end(ap);shown=text;return kOfxStatOK;}
static OfxMessageSuiteV1 messages{captureMessage};
static OfxPropertySetHandle bag(Bag &b){return reinterpret_cast<OfxPropertySetHandle>(&b);}
struct Frame {
 static constexpr int W=37,H=11; // odd sizes: partial threadgroups
 std::vector<float> in,out;fake::Buffer src,dst;int bounds[4]={100,50,100+W,50+H};
 Frame():in(size_t(W)*H*4),out(size_t(W)*H*4,-7.f),src{in.data(),in.size(),true},dst{out.data(),out.size(),true}{
  for(int y=0;y<H;y++)for(int x=0;x<W;x++){float *q=&in[(size_t(y)*W+x)*4];q[0]=.01f+.9f*x/W;q[1]=.02f+.8f*y/H;q[2]=float((x*7+y*13)%W)/W;q[3]=1.f;}
 }
};
static void attach(mock::Effect &fx,Frame &f,bool premult=false){
 for(auto *b:{&fx.source.image,&fx.output.image}){auto p=bag(*b);mock::ss(p,kOfxImageEffectPropPixelDepth,0,kOfxBitDepthFloat);mock::ss(p,kOfxImageEffectPropComponents,0,kOfxImageComponentRGBA);
  mock::ss(p,kOfxImageEffectPropPreMultiplication,0,premult?kOfxImagePreMultiplied:kOfxImageUnPreMultiplied);mock::si(p,kOfxImagePropRowBytes,0,Frame::W*16);for(int c=0;c<4;c++)mock::si(p,kOfxImagePropBounds,c,f.bounds[c]);}
 mock::sp(bag(fx.source.image),kOfxImagePropData,0,&f.src);mock::sp(bag(fx.output.image),kOfxImagePropData,0,&f.dst);
}
static Bag metalArgs(const int *window){Bag a;mock::sd(bag(a),kOfxPropTime,0,0);for(int c=0;c<4;c++)mock::si(bag(a),kOfxImageEffectPropRenderWindow,c,window[c]);
 mock::si(bag(a),kOfxImageEffectPropMetalEnabled,0,1);mock::sp(bag(a),kOfxImageEffectPropMetalCommandQueue,0,&fake::queue);return a;}
static double worst(OfxParamSetHandle s,const Frame &f,const int *window,bool premult=false){
 auto prepared=nss::Prepared(readParams(s,0));double w=0;
 for(int y=f.bounds[1];y<f.bounds[3];y++)for(int x=f.bounds[0];x<f.bounds[2];x++){size_t i=(size_t(y-f.bounds[1])*Frame::W+(x-f.bounds[0]))*4;
  bool inside=x>=window[0]&&x<window[2]&&y>=window[1]&&y<window[3];float expected[4];
  if(inside)prepared.rgba(&f.in[i],expected,premult);else for(int c=0;c<4;c++)expected[c]=-7.f; // outside the window: untouched
  for(int c=0;c<4;c++){double d=std::abs(double(f.out[i+c])-expected[c]);if(!(d==d))d=1e9;w=std::max(w,d);}}
 return w;
}
static std::string lastRender(OfxImageEffectHandle e){std::lock_guard<std::mutex> lock(instance(e)->mutex);return instance(e)->lastRender;}
struct Plugin {
 mock::Effect fx;OfxImageEffectHandle e=reinterpret_cast<OfxImageEffectHandle>(&fx);OfxParamSetHandle s=nullptr;
 Plugin(){require(entry(kOfxActionDescribe,e,nullptr,nullptr)==kOfxStatOK);require(entry(kOfxImageEffectActionDescribeInContext,e,nullptr,nullptr)==kOfxStatOK);
  require(entry(kOfxActionCreateInstance,e,nullptr,nullptr)==kOfxStatOK);s=params(e);setI(s,"auto_metadata",0);setI(s,"camera_on",0);}
 ~Plugin(){entry(kOfxActionDestroyInstance,e,nullptr,nullptr);}
 std::string metalProp(){return str(properties(e),kOfxImageEffectPropMetalRenderSupported);}
};
static void dense_parity(){
 // The exact embedded kernel (host C++ build) against the CPU reference on a dense ramp grid.
 const int W=512,H=128;std::vector<float> in(size_t(W)*H*4),out(in.size()),cpu(in.size());
 for(int y=0;y<H;y++)for(int x=0;x<W;x++){float *q=&in[(size_t(y)*W+x)*4];q[0]=float(x)/W*1.2f;q[1]=float(y)/H;q[2]=float((x*7+y*13)%W)/W;q[3]=.25f+.75f*float(x%5)/4;}
 double overall=0;
 for(int variant=0;variant<5;variant++){
  nss::Pipeline pipe;if(variant==1)pipe.stage=0;if(variant==2){pipe.tone.shoulder_on=0;pipe.tone.toe_on=0;}if(variant==3)pipe.density.in_di=1;
  bool premult=variant==4;nss::Prepared prep(pipe);
  for(size_t i=0;i<in.size();i+=4)prep.rgba(&in[i],&cpu[i],premult);
  emu::NssArgs a{0,0,W,H,W*4,0,0,W,H,W*4,0,0,W,H,int(premult)};
  for(unsigned y=0;y<unsigned(H);y++)for(unsigned x=0;x<unsigned(W);x++)emu::nss_render_metal(in.data(),out.data(),prep.values.data(),a,uint2{x,y});
  double w=0;for(size_t i=0;i<in.size();i++)w=std::max(w,std::abs(double(out[i])-cpu[i]));
  require(w<=1e-6);overall=std::max(overall,w);
 }
 std::cout<<"  embedded MSL kernel as host C++ vs CPU: worst abs "<<overall<<" over 5 variants x "<<W*H*4<<" values\n";
}
int main(){
 mock::init();OfxHost host{nullptr,mock::fetch};setHost(&host);check(entry(kOfxActionLoad,nullptr,nullptr,nullptr));
 dense_parity();
 // 1. No Metal (Windows/Linux/CPU-only Mac): declared false; describe/createInstance succeed.
 fake::install(false);{Plugin p;require(p.metalProp()=="false");}
 fake::install(true);
 // 2. Metal available: declared true, renders through the host queue without waiting.
 {Plugin p;require(p.metalProp()=="true");Frame f;attach(p.fx,f);int window[4]={101,51,135,60};auto a=metalArgs(window);
  for(int r=0;r<3;r++)require(entry(kOfxImageEffectActionRender,p.e,bag(a),nullptr)==kOfxStatOK);
  require(fake::encodes==3&&fake::drains==0);require(worst(p.s,f,window)<=1e-6);require(lastRender(p.e).find("Metal (Mock Metal), host queue buffers")==0);
  require(instance(p.e)->preparedCache.builds==1);require(p.metalProp()=="true");
  // Window larger than the source: pixels outside the source are written as zero, like the CPU path.
  int wide[4]={100,50,137,61};int clipped[4]={102,52,130,58};mock::Bag sb=p.fx.source.image;for(int c=0;c<4;c++)mock::si(bag(p.fx.source.image),kOfxImagePropBounds,c,clipped[c]);
  // Source buffer starts at its own origin: re-point at the clipped region.
  std::vector<float> sub(size_t(clipped[2]-clipped[0])*(clipped[3]-clipped[1])*4);for(int y=clipped[1];y<clipped[3];y++)for(int x=clipped[0];x<clipped[2];x++)for(int c=0;c<4;c++)sub[(size_t(y-clipped[1])*(clipped[2]-clipped[0])+(x-clipped[0]))*4+c]=f.in[(size_t(y-50)*Frame::W+(x-100))*4+c];
  fake::Buffer subBuffer{sub.data(),sub.size(),true};mock::sp(bag(p.fx.source.image),kOfxImagePropData,0,&subBuffer);mock::si(bag(p.fx.source.image),kOfxImagePropRowBytes,0,(clipped[2]-clipped[0])*16);
  auto wa=metalArgs(wide);require(entry(kOfxImageEffectActionRender,p.e,bag(wa),nullptr)==kOfxStatOK);
  auto prepared=nss::Prepared(readParams(p.s,0));double w=0;
  for(int y=50;y<61;y++)for(int x=100;x<137;x++){size_t i=(size_t(y-50)*Frame::W+(x-100))*4;float e[4]={0,0,0,0};if(x>=clipped[0]&&x<clipped[2]&&y>=clipped[1]&&y<clipped[3])prepared.rgba(&f.in[i],e,false);for(int c=0;c<4;c++)w=std::max(w,double(std::abs(f.out[i+c]-e[c])));}
  require(w<=1e-6);p.fx.source.image=sb;
 }
 // 3. Every forced failure: frame still renders (CPU on the shared buffers), Metal withdrawn for the instance.
 for(int fault:{int(nss::kMetalFaultNoDevice),int(nss::kMetalFaultCompile),int(nss::kMetalFaultPipeline),int(nss::kMetalFaultEncode),-1}){
  Plugin p;Frame f;attach(p.fx,f);int window[4]={100,50,137,61};auto a=metalArgs(window);int before=fake::encodes,drained=fake::drains;
  if(fault<0)fake::throwNext=true;else nss::MetalApi::get().fault=fault;
  require(entry(kOfxImageEffectActionRender,p.e,bag(a),nullptr)==kOfxStatOK);nss::MetalApi::get().fault=nss::kMetalFaultNone;
  require(fake::encodes==before&&fake::drains==drained+1);require(worst(p.s,f,window)<=1e-6);
  require(p.metalProp()=="false"&&instance(p.e)->metalWithdrawn);auto text=lastRender(p.e);require(text.find("Metal failed")==0&&text.find("CPU fallback on shared Metal buffers")!=std::string::npos);
  // Withdrawn: later Metal frames go straight to the CPU, no new encode attempt.
  std::fill(f.out.begin(),f.out.end(),-7.f);require(entry(kOfxImageEffectActionRender,p.e,bag(a),nullptr)==kOfxStatOK);require(fake::encodes==before&&worst(p.s,f,window)<=1e-6);
  // Switching Render engine does not re-offer Metal on a withdrawn instance.
  setI(p.s,"render_device",0);mock::Bag changed;mock::ss(bag(changed),kOfxPropName,0,"render_device");mock::ss(bag(changed),kOfxPropChangeReason,0,kOfxChangeUserEdited);check(entry(kOfxActionInstanceChanged,p.e,bag(changed),nullptr));require(p.metalProp()=="false");
 }
 require(fake::throws==1);
 // 4. Asynchronous command-buffer error reported after commit: the next frame falls back.
 {Plugin p;Frame f;attach(p.fx,f);int window[4]={100,50,137,61};auto a=metalArgs(window);require(entry(kOfxImageEffectActionRender,p.e,bag(a),nullptr)==kOfxStatOK);int before=fake::encodes;
  instance(p.e)->metal->fail("mock GPU timeout");std::fill(f.out.begin(),f.out.end(),-7.f);require(entry(kOfxImageEffectActionRender,p.e,bag(a),nullptr)==kOfxStatOK);
  require(fake::encodes==before&&worst(p.s,f,window)<=1e-6&&p.metalProp()=="false");require(lastRender(p.e).find("Metal command buffer failed: mock GPU timeout")==0);}
 // 5. GPU-private buffers cannot be reached from the CPU: report GPURenderFailed, never crash.
 {Plugin p;Frame f;f.src.shared=f.dst.shared=false;attach(p.fx,f);int window[4]={100,50,137,61};auto a=metalArgs(window);nss::MetalApi::get().fault=nss::kMetalFaultEncode;
  require(entry(kOfxImageEffectActionRender,p.e,bag(a),nullptr)==kOfxStatGPURenderFailed);nss::MetalApi::get().fault=nss::kMetalFaultNone;require(p.metalProp()=="false");}
 // 6. Render engine = CPU while the host still sends Metal buffers: CPU on shared buffers, Metal not withdrawn.
 {Plugin p;Frame f;attach(p.fx,f);int window[4]={100,50,137,61};auto a=metalArgs(window);setI(p.s,"render_device",1);mock::Bag changed;mock::ss(bag(changed),kOfxPropName,0,"render_device");mock::ss(bag(changed),kOfxPropChangeReason,0,kOfxChangeUserEdited);check(entry(kOfxActionInstanceChanged,p.e,bag(changed),nullptr));
  require(p.metalProp()=="false");int before=fake::encodes;require(entry(kOfxImageEffectActionRender,p.e,bag(a),nullptr)==kOfxStatOK);require(fake::encodes==before&&worst(p.s,f,window)<=1e-6&&!instance(p.e)->metalWithdrawn);
  setI(p.s,"render_device",0);check(entry(kOfxActionInstanceChanged,p.e,bag(changed),nullptr));require(p.metalProp()=="true");}
 // 7. Two GPU APIs at once is a host error.
 {Plugin p;Frame f;attach(p.fx,f);int window[4]={100,50,137,61};auto a=metalArgs(window);mock::si(bag(a),kOfxImageEffectPropCudaEnabled,0,1);require(entry(kOfxImageEffectActionRender,p.e,bag(a),nullptr)==kOfxStatGPURenderFailed);}
 // 8. Viewer gray picker with Metal: one queue drain to read the shared source, then the normal Metal render.
 {Plugin p;Frame f;for(size_t i=0;i<f.in.size();i+=4){f.in[i]=f.in[i+1]=f.in[i+2]=.3f;}attach(p.fx,f);int window[4]={100,50,137,61};auto a=metalArgs(window);setD(p.s,"tone_highlight_cream",0);
  setI(p.s,"pick_auto",1);mock::Bag changed;mock::ss(bag(changed),kOfxPropName,0,"pick_auto");mock::ss(bag(changed),kOfxPropChangeReason,0,kOfxChangeUserEdited);check(entry(kOfxActionInstanceChanged,p.e,bag(changed),nullptr));setD(p.s,"pick_radius",2);check(param->paramSetValue(handle(p.s,"pick_point"),110.5,55.5));
  int drained=fake::drains,before=fake::encodes;require(entry(kOfxImageEffectActionRender,p.e,bag(a),nullptr)==kOfxStatOK);require(fake::drains==drained+1&&fake::encodes==before+1);
  require(lastRender(p.e).find("pick unavailable")==std::string::npos);require(std::abs(f.out[0]-f.out[1])<2e-5f&&std::abs(f.out[1]-f.out[2])<2e-5f);}
 // 9. Rendering controls and Show render engine, per platform (this file is built both ways by test.sh).
 message=&messages;
 {Plugin p;auto device=p.fx.params.values["render_device"],info=p.fx.params.values["render_info"],group=p.fx.params.values["render"];
  auto secret=[](mock::Bag &b){auto a=b.ints.find(kOfxParamPropSecret);return a!=b.ints.end()&&!a->second.empty()&&a->second[0]==1;};
  require(device.ints[kOfxParamPropDefault][0]==0&&device.strings[kOfxParamPropChoiceOption].size()==2&&device.strings[kOfxParamPropChoiceOption][1]=="CPU");
  Frame f;attach(p.fx,f);int window[4]={100,50,137,61};auto a=metalArgs(window);require(entry(kOfxImageEffectActionRender,p.e,bag(a),nullptr)==kOfxStatOK);
  mock::Bag changed;mock::ss(bag(changed),kOfxPropName,0,"render_info");mock::ss(bag(changed),kOfxPropChangeReason,0,kOfxChangeUserEdited);shown.clear();require(entry(kOfxActionInstanceChanged,p.e,bag(changed),nullptr)==kOfxStatOK);
#if NSS_MAC_RENDER_UI
  require(!secret(device)&&!secret(info)&&!secret(group));require(device.strings[kOfxParamPropChoiceOption][0]=="Auto (Metal)");
  require(shown.rfind("Metal (Mock Metal)",0)==0&&shown.find("\nMetal: mock")!=std::string::npos&&shown.find("CUDA")==std::string::npos&&shown.find("OpenCL")==std::string::npos);
  // A/B: Render engine = CPU, the host then sends CPU images; Show render engine starts with CPU.
  setI(p.s,"render_device",1);mock::Bag dev;mock::ss(bag(dev),kOfxPropName,0,"render_device");mock::ss(bag(dev),kOfxPropChangeReason,0,kOfxChangeUserEdited);check(entry(kOfxActionInstanceChanged,p.e,bag(dev),nullptr));require(p.metalProp()=="false");
  Bag cpuArgs;mock::sd(bag(cpuArgs),kOfxPropTime,0,0);for(int c=0;c<4;c++)mock::si(bag(cpuArgs),kOfxImageEffectPropRenderWindow,c,window[c]);mock::sp(bag(p.fx.source.image),kOfxImagePropData,0,f.in.data());mock::sp(bag(p.fx.output.image),kOfxImagePropData,0,f.out.data());
  require(entry(kOfxImageEffectActionRender,p.e,bag(cpuArgs),nullptr)==kOfxStatOK);require(entry(kOfxActionInstanceChanged,p.e,bag(changed),nullptr)==kOfxStatOK);require(shown.rfind("CPU ",0)==0);
  std::cout<<"  macOS UI: Rendering group visible, Render engine Auto (Metal)|CPU, Show render engine reports Metal (device) / CPU\n";
#else
  require(secret(device)&&secret(info)&&secret(group));require(device.strings[kOfxParamPropChoiceOption][0]=="Auto (CUDA/OpenCL if offered)");
  require(shown.rfind("Metal (Mock Metal)",0)==0&&shown.find("OpenCL runtime: ")!=std::string::npos&&shown.find("\nMetal: ")==std::string::npos);
  std::cout<<"  Windows/Linux UI: Rendering controls hidden as in 0.90.14, Show render engine text lines unchanged\n";
#endif
 }
 // Withdrawn state is named in the Show render engine text on macOS.
 {Plugin p;auto i=instance(p.e);i->metalWithdrawn=true;mock::Bag changed;mock::ss(bag(changed),kOfxPropName,0,"render_info");mock::ss(bag(changed),kOfxPropChangeReason,0,kOfxChangeUserEdited);require(entry(kOfxActionInstanceChanged,p.e,bag(changed),nullptr)==kOfxStatOK);
  require(!NSS_MAC_RENDER_UI||shown.find("withdrawn for this node")!=std::string::npos);}
 message=nullptr;
 std::cout<<"PASS: Metal contract (mock API + embedded MSL kernel as host C++): property declare/withdraw, host-queue encode without drain, "<<fake::encodes<<" encodes, CPU fallback on 5 forced failures, async error, private buffers, CPU engine, picker. NOT Metal execution.\n";
}
