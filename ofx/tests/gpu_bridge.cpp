#define main nss_mock_test_main
#include "host_test.cpp"
#undef main
namespace {
mock::Effect gpuFx;bool gpuInit=false;std::string gpuError;
void setGpuParam(OfxParamSetHandle s,const Def &d,const nss::JSON &j){std::string key=d.key;size_t split=key.find('_');if(split==std::string::npos)return;auto group=key.substr(0,split),name=key.substr(split+1);if(j.contains(group)&&j.at(group).contains(name)){if(d.kind==Kind::Number)setD(s,d.key,j.at(group).at(name).get<double>());else setI(s,d.key,j.at(group).at(name).get<int>());}}
}
int testRender(void *queue,void *input,void *output,int width,int height,int premult,const char *json,int gpu){
 try{
  auto e=reinterpret_cast<OfxImageEffectHandle>(&gpuFx);auto bag=[](mock::Bag &b){return reinterpret_cast<OfxPropertySetHandle>(&b);};
  if(!gpuInit){mock::init();static OfxHost h{nullptr,mock::fetch};setHost(&h);check(entry(kOfxActionLoad,nullptr,nullptr,nullptr));check(describe(e));check(describeContext(e));check(entry(kOfxActionCreateInstance,e,nullptr,nullptr));gpuInit=true;}
  else{gpuFx.params.values.clear();check(describeContext(e));}
  auto s=params(e);setI(s,"auto_metadata",0);auto j=nss::JSON::parse(json);
  for(auto &d:defs)setGpuParam(s,d,j);setD(s,"gamma_scale",j.value("gamma_scale",1.));
  if(j.contains("pick")){setI(s,"pick_auto",1);mock::Bag change;mock::ss(bag(change),kOfxPropName,0,"pick_auto");mock::ss(bag(change),kOfxPropChangeReason,0,kOfxChangeUserEdited);check(changed(e,bag(change)));setD(s,"pick_radius",j.at("pick").at("radius").get<double>());check(param->paramSetValue(handle(s,"pick_point"),j.at("pick").at("x").get<double>(),j.at("pick").at("y").get<double>()));}
  setI(s,"stage",j.value("stage",1));setI(s,"camera_on",j.value("camera_on",0));setD(s,"camera_strength",j.value("camera_strength",1.));
  if(j.contains("camera"))for(int i=0;i<9;i++){auto key="camera_"+std::to_string(i);setD(s,key.c_str(),j.at("camera").at(i).get<double>());}
  for(auto *b:{&gpuFx.source.image,&gpuFx.output.image}){auto p=bag(*b);mock::ss(p,kOfxImageEffectPropPixelDepth,0,kOfxBitDepthFloat);mock::ss(p,kOfxImageEffectPropComponents,0,kOfxImageComponentRGBA);mock::ss(p,kOfxImageEffectPropPreMultiplication,0,premult?kOfxImagePreMultiplied:kOfxImageUnPreMultiplied);mock::si(p,kOfxImagePropRowBytes,0,width*16);int bounds[]={10,20,10+width,20+height};for(int c=0;c<4;c++)mock::si(p,kOfxImagePropBounds,c,bounds[c]);}
  mock::sp(bag(gpuFx.source.image),kOfxImagePropData,0,input);mock::sp(bag(gpuFx.output.image),kOfxImagePropData,0,output);
  mock::Bag args;mock::sd(bag(args),kOfxPropTime,0,0);mock::si(bag(args),kOfxImageEffectPropOpenCLEnabled,0,gpu);mock::sp(bag(args),kOfxImageEffectPropOpenCLCommandQueue,0,queue);int window[]={10,20,10+width,20+height};for(int c=0;c<4;c++)mock::si(bag(args),kOfxImageEffectPropRenderWindow,c,window[c]);
  auto status=entry(kOfxImageEffectActionRender,e,bag(args),nullptr);gpuError=instance(e)->lastRender;return status;
 }catch(const std::exception &e){gpuError=e.what();return -1;}catch(...){gpuError="Bridge exception";return -2;}
}
extern "C" const char *nss_test_gpu_status(){return gpuError.c_str();}
extern "C" void nss_test_gpu_close(){if(gpuInit){auto e=reinterpret_cast<OfxImageEffectHandle>(&gpuFx);entry(kOfxActionDestroyInstance,e,nullptr,nullptr);gpuInit=false;}}
extern "C" int nss_test_gpu(void *q,void *i,void *o,int w,int h,int p,const char *j){return testRender(q,i,o,w,h,p,j,1);}
extern "C" int nss_test_cpu(void *i,void *o,int w,int h,int p,const char *j){return testRender(nullptr,i,o,w,h,p,j,0);}
extern "C" unsigned long long nss_test_cl_programs(){auto e=reinterpret_cast<OfxImageEffectHandle>(&gpuFx);return instance(e)->opencl.programBuilds;}
extern "C" unsigned long long nss_test_cl_kernels(){auto e=reinterpret_cast<OfxImageEffectHandle>(&gpuFx);return instance(e)->opencl.kernelBuilds;}
