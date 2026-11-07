// No GPU emulator claim: mock Driver ABI + native compilation of CUDA source.
// PTX execution is NOT tested here. Production code still calls the real CUDA driver.
#define main nss_original_host_test
#include "host_test.cpp"
#undef main
#include <cstdlib>
struct Dim {unsigned x=0,y=0,z=0;};static Dim threadIdx,blockIdx,blockDim;
#define __global__
#include "../src/render_cuda.cu"
#undef __global__
namespace driver {
void *current=nullptr,*context=reinterpret_cast<void*>(0x3333),*lastStream=nullptr;std::vector<void*> stack;
float *source=nullptr,*dest=nullptr;int loads=0,unloads=0,launches=0,syncs=0,copies=0,allocations=0;
int get(void **p){*p=current;return 0;}int push(void *p){stack.push_back(current);current=p;return 0;}
int pop(void **p){*p=current;current=stack.back();stack.pop_back();return 0;}
int attribute(void *p,int type,uint64_t v){if(type!=1||(v!=0x100000&&v!=0x200000))return 1;*static_cast<void**>(p)=context;return 0;}
int load(void **p,const void *ptx){require(std::strstr(static_cast<const char*>(ptx),".entry nss_render_cuda")!=nullptr);*p=reinterpret_cast<void*>(0x4444);++loads;return 0;}
int function(void **p,void*,const char *name){require(std::string(name)=="nss_render_cuda");*p=reinterpret_cast<void*>(0x5555);return 0;}
int unload(void*){++unloads;return 0;}int synchronize(void *s){lastStream=s;++syncs;return 0;}
int alloc(void **p,size_t n,unsigned){*p=std::malloc(n);++allocations;return *p?0:2;}int free(void *p){std::free(p);return 0;}
int error(int,const char **p){*p="mock failure";return 0;}
int copy(const nss::CudaCopy2D *r,void *s){require(current==context&&r->srcDevice==0x100000&&r->srcType==2&&r->dstType==1);lastStream=s;++copies;
 for(size_t y=0;y<r->height;y++)std::memcpy(static_cast<char*>(r->dstHost)+y*r->dstPitch,reinterpret_cast<char*>(source)+(y+r->srcY)*r->srcPitch+r->srcX,r->width);return 0;}
int launch(void*,unsigned gx,unsigned gy,unsigned,unsigned tx,unsigned ty,unsigned,unsigned,void *stream,void **args,void**){
 require(current==context&&*static_cast<uint64_t*>(args[0])==0x100000&&*static_cast<uint64_t*>(args[1])==0x200000);lastStream=stream;++launches;
 NssBlock constants;std::memcpy(&constants,args[2],sizeof(constants));int a[15];for(int i=0;i<15;i++)a[i]=*static_cast<int*>(args[i+3]);
 blockDim={tx,ty,1};for(unsigned by=0;by<gy;by++)for(unsigned bx=0;bx<gx;bx++)for(unsigned y=0;y<ty;y++)for(unsigned x=0;x<tx;x++){
  blockIdx={bx,by,0};threadIdx={x,y,0};nss_render_cuda(source,dest,constants,a[0],a[1],a[2],a[3],a[4],a[5],a[6],a[7],a[8],a[9],a[10],a[11],a[12],a[13],a[14]);}return 0;
}
void install(){auto &a=nss::CudaApi::get();a.available=true;a.availability="mock CUDA Driver ABI test";a.getCurrent=get;a.pushCurrent=push;a.popCurrent=pop;a.pointerAttribute=attribute;a.loadModule=load;a.getFunction=function;a.unloadModule=unload;a.launch=launch;a.synchronize=synchronize;a.hostAlloc=alloc;a.hostFree=free;a.copy2DAsync=copy;a.errorString=error;}
}
int main(){driver::install();mock::init();OfxHost host{nullptr,mock::fetch};setHost(&host);check(entry(kOfxActionLoad,nullptr,nullptr,nullptr));mock::Effect fx;auto e=reinterpret_cast<OfxImageEffectHandle>(&fx);check(describe(e));check(describeContext(e));check(entry(kOfxActionCreateInstance,e,nullptr,nullptr));auto s=params(e);
 auto bag=[](mock::Bag &b){return reinterpret_cast<OfxPropertySetHandle>(&b);};setI(s,"auto_metadata",0);setI(s,"camera_on",0);
 require(str(properties(e),kOfxImageEffectPropCudaRenderSupported)=="true");require(fx.props.ints[kOfxImageEffectPluginPropHostFrameThreading][0]==0);
 float input[32],output[32]{};for(int i=0;i<8;i++){input[4*i]=.02f+.03f*i;input[4*i+1]=.03f+.02f*i;input[4*i+2]=.04f+.01f*i;input[4*i+3]=1;}
 driver::source=input;driver::dest=output;
 int bounds[]={10,20,14,22};for(auto *b:{&fx.source.image,&fx.output.image}){auto p=bag(*b);mock::ss(p,kOfxImageEffectPropPixelDepth,0,kOfxBitDepthFloat);mock::ss(p,kOfxImageEffectPropComponents,0,kOfxImageComponentRGBA);mock::ss(p,kOfxImageEffectPropPreMultiplication,0,kOfxImageUnPreMultiplied);mock::si(p,kOfxImagePropRowBytes,0,64);for(int c=0;c<4;c++)mock::si(p,kOfxImagePropBounds,c,bounds[c]);}
 mock::sp(bag(fx.source.image),kOfxImagePropData,0,reinterpret_cast<void*>(0x100000));mock::sp(bag(fx.output.image),kOfxImagePropData,0,reinterpret_cast<void*>(0x200000));
 mock::Bag args;mock::sd(bag(args),kOfxPropTime,0,0);for(int c=0;c<4;c++)mock::si(bag(args),kOfxImageEffectPropRenderWindow,c,bounds[c]);mock::si(bag(args),kOfxImageEffectPropCudaEnabled,0,1);auto stream=reinterpret_cast<void*>(0x7777);mock::sp(bag(args),kOfxImageEffectPropCudaStream,0,stream);
 for(int repeat=0;repeat<3;repeat++){require(entry(kOfxImageEffectActionRender,e,bag(args),nullptr)==kOfxStatOK);require(driver::current==nullptr&&driver::lastStream==stream);}
 require(driver::loads==1&&driver::launches==3&&driver::syncs==0&&driver::copies==0&&driver::allocations==0);require(instance(e)->preparedCache.builds==1);
 auto prepared=nss::Prepared(readParams(s,0));for(int i=0;i<8;i++){float expected[4];prepared.rgba(input+4*i,expected,false);for(int c=0;c<4;c++)require(std::abs(output[4*i+c]-expected[c])<1e-6f);}
 require(fx.source.requestedRegion&&fx.source.requested.x1==10&&fx.source.requested.y1==20&&fx.source.requested.x2==14&&fx.source.requested.y2==22);
 // Changing a visible value invalidates the exact prepared cache.
 setD(s,"positive_bal_r",.25);check(entry(kOfxImageEffectActionRender,e,bag(args),nullptr));require(instance(e)->preparedCache.builds==2);
 // Signed row stride is preserved by the CUDA-source indexing.
 driver::source=input+16;driver::dest=output+16;mock::si(bag(fx.source.image),kOfxImagePropRowBytes,0,-64);mock::si(bag(fx.output.image),kOfxImagePropRowBytes,0,-64);
 check(entry(kOfxImageEffectActionRender,e,bag(args),nullptr));auto negativeReference=nss::Prepared(readParams(s,0));
 for(int j=0;j<8;j++){float expected[4];negativeReference.rgba(input+4*j,expected,false);for(int c=0;c<4;c++)require(std::abs(output[4*j+c]-expected[c])<1e-6f);}
 driver::source=input;driver::dest=output;mock::si(bag(fx.source.image),kOfxImagePropRowBytes,0,64);mock::si(bag(fx.output.image),kOfxImagePropRowBytes,0,64);
 // A new host context gets a module; returning to the old one reuses its module.
 driver::context=reinterpret_cast<void*>(0x9999);check(entry(kOfxImageEffectActionRender,e,bag(args),nullptr));driver::context=reinterpret_cast<void*>(0x3333);check(entry(kOfxImageEffectActionRender,e,bag(args),nullptr));require(driver::loads==2);
 // No stream property requires completion of default stream, never device sync.
 args.pointers.erase(kOfxImageEffectPropCudaStream);check(entry(kOfxImageEffectActionRender,e,bag(args),nullptr));require(driver::syncs==1&&driver::lastStream==nullptr);mock::sp(bag(args),kOfxImageEffectPropCudaStream,0,stream);
 // Gray picker is the only normal source download/sync, and is source based.
 setI(s,"pick_auto",1);mock::Bag changed;mock::ss(bag(changed),kOfxPropName,0,"pick_auto");mock::ss(bag(changed),kOfxPropChangeReason,0,kOfxChangeUserEdited);check(entry(kOfxActionInstanceChanged,e,bag(changed),nullptr));setD(s,"pick_radius",0);check(param->paramSetValue(handle(s,"pick_point"),11.5,20.5));check(entry(kOfxImageEffectActionRender,e,bag(args),nullptr));require(driver::copies==1&&driver::allocations==1&&driver::syncs==2);require(std::abs(output[4]-output[5])<2e-5f&&std::abs(output[5]-output[6])<2e-5f);
 setI(s,"pick_auto",0);setI(s,"render_device",1);require(entry(kOfxImageEffectActionRender,e,bag(args),nullptr)==kOfxStatGPURenderFailed);
 check(entry(kOfxActionDestroyInstance,e,nullptr,nullptr));require(driver::unloads==2&&driver::current==nullptr);
 std::cout<<"PASS: mocked CUDA Driver ABI/context/streams/device pointers, native CUDA-source parity, module/cache reuse, scoped ROI and optional picker. NOT PTX execution.\n";
}
