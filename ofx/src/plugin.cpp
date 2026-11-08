#include "ofxImageEffect.h"
#include "ofxProperty.h"
#include "ofxInteract.h"
#include "overlay_gl.hpp"
#include "metadata.hpp"
#include "wb_presets.hpp"
#include "ofxGPURender.h"
#include "opencl.hpp"
#include "cuda.hpp"
#include "prepared_cache.hpp"
#include <chrono>
#include "cpu_render.hpp"
#include "gray_pick.hpp"
#include <cstdio>
#include <cstring>
#include <map>
#include <memory>
#include <mutex>
#include <sstream>

namespace {
OfxHost *host=nullptr;
OfxPropertySuiteV1 *prop=nullptr;
OfxImageEffectSuiteV1 *effect=nullptr;
OfxParameterSuiteV1 *param=nullptr;
OfxMessageSuiteV1 *message=nullptr;
OfxMultiThreadSuiteV1 *multithread=nullptr;
OfxInteractSuiteV1 *interact=nullptr;
OfxStatus overlayEntry(const char*,const void*,OfxPropertySetHandle,OfxPropertySetHandle);
using nss::Pipeline;
enum class Kind {Number,Boolean,Choice};
struct Def {const char *key,*label;Kind kind;double def,lo,hi,step;const char *options,*group;};
const Def defs[]={
#include "parameter_defs.inc"
};
void check(OfxStatus s){if(s!=kOfxStatOK)throw s;}
std::string str(OfxPropertySetHandle p,const char *key,int i=0){char *s=nullptr;return p&&prop->propGetString(p,key,i,&s)==kOfxStatOK&&s?s:"";}
OfxPropertySetHandle properties(OfxImageEffectHandle e){OfxPropertySetHandle p;check(effect->getPropertySet(e,&p));return p;}
OfxParamSetHandle params(OfxImageEffectHandle e){OfxParamSetHandle p;check(effect->getParamSet(e,&p));return p;}
OfxParamHandle handle(OfxParamSetHandle s,const char *k){OfxParamHandle h;check(param->paramGetHandle(s,k,&h,nullptr));return h;}
int integer(OfxParamSetHandle s,const char *k,double t){int v;check(param->paramGetValueAtTime(handle(s,k),t,&v));return v;}
double number(OfxParamSetHandle s,const char *k,double t){double v;check(param->paramGetValueAtTime(handle(s,k),t,&v));return v;}
std::string stringParam(OfxParamSetHandle s,const char *k){char *v=nullptr;check(param->paramGetValue(handle(s,k),&v));return v?v:"";}
void setD(OfxParamSetHandle s,const char *k,double v){check(param->paramSetValue(handle(s,k),v));}
void setI(OfxParamSetHandle s,const char *k,int v){check(param->paramSetValue(handle(s,k),v));}
void setS(OfxParamSetHandle s,const char *k,const std::string &v){check(param->paramSetValue(handle(s,k),v.c_str()));}
struct Edit {
 OfxParamSetHandle set;
 Edit(OfxParamSetHandle s,const char *name):set(s){check(param->paramEditBegin(set,name));}
 ~Edit(){param->paramEditEnd(set);}
};
struct Instance {
 OfxImageClipHandle src=nullptr,dst=nullptr;
 std::mutex mutex;
 // One bounded cache entry; no stale metadata reused for another clip.
 std::string path;std::filesystem::file_time_type stamp{};std::optional<nss::Metadata> meta;
 std::string metadataError,lastRender="No render yet";nss::CLRender opencl;nss::CudaRender cuda;nss::PreparedCache preparedCache;
 std::chrono::steady_clock::time_point nextMetadataCheck{};uint64_t metadataReads=0,metadataProbes=0;
};
Instance *instance(OfxImageEffectHandle e){void *p=nullptr;check(prop->propGetPointer(properties(e),kOfxPropInstanceData,0,&p));if(!p)throw kOfxStatErrBadHandle;return static_cast<Instance*>(p);}
std::string sourcePath(OfxImageEffectHandle e,Instance *i,OfxPropertySetHandle args=nullptr,OfxPropertySetHandle image=nullptr) {
 // Resolve 20 extension, not standard OFX. Probe only host-provided properties.
 for(auto p:{args,image,properties(e)}){auto s=str(p,"OfxImageEffectPropSrcFilePath");if(!s.empty())return s;}
 OfxPropertySetHandle p=nullptr;effect->clipGetPropertySet(i->src,&p);return str(p,"OfxImageEffectPropSrcFilePath");
}
nss::Metadata load(Instance *i,const std::string &input,bool force=false) {
 auto path=nss::normalizePath(input);if(path.empty())throw std::runtime_error("Choose TIFF or JSON");
 std::lock_guard<std::mutex> lock(i->mutex);std::error_code error;
 auto now=std::chrono::steady_clock::now();
 if(!force&&path==i->path&&now<i->nextMetadataCheck){if(i->meta)return *i->meta;if(!i->metadataError.empty())throw std::runtime_error(i->metadataError);}
 i->nextMetadataCheck=now+std::chrono::milliseconds(250);++i->metadataProbes;
 auto stamp=std::filesystem::last_write_time(std::filesystem::u8path(path),error);
 if(!force&&!error&&i->path==path&&i->stamp==stamp){if(i->meta)return *i->meta;if(!i->metadataError.empty())throw std::runtime_error(i->metadataError);}
 i->path=path;i->stamp=stamp;i->meta.reset();i->metadataError.clear();
 try{++i->metadataReads;auto m=nss::parseMetadata(nss::readDescription(path));i->meta=m;return m;}
 catch(const std::exception &x){i->metadataError=x.what();throw;}
}
std::string shortError(const std::exception &x){std::string text=x.what();return text.size()<=40?text:"Invalid NSS info";}
void setLastRender(Instance *i,const std::string &text){std::lock_guard<std::mutex> lock(i->mutex);i->lastRender=text;}
Pipeline readParams(OfxParamSetHandle set,double time) {
 Pipeline p;
#include "read_params.inc"
 p.positive.film_wb=0; // Dropdown is a loader, never an extra hidden render gain.
 p.stage=integer(set,"stage",time);p.cameraOn=integer(set,"camera_on",time);p.neutralise=integer(set,"neutralise",time);p.cameraStrength=number(set,"camera_strength",time);
 for(int a=0;a<9;a++){auto k="camera_"+std::to_string(a);p.camera[a]=number(set,k.c_str(),time);k="neutral_"+std::to_string(a);p.neutral[a]=number(set,k.c_str(),time);}
 float gammaScale=number(set,"gamma_scale",time);if(!std::isfinite(gammaScale)||gammaScale<.25f||gammaScale>2.f)throw kOfxStatErrValue;p.positive.gam_r*=gammaScale;p.positive.gam_g*=gammaScale;p.positive.gam_b*=gammaScale;
 return p;
}
void choices(OfxPropertySetHandle p,const char *s) {std::istringstream in(s);std::string v;int i=0;while(std::getline(in,v,'|'))prop->propSetString(p,kOfxParamPropChoiceOption,i++,v.c_str());}
OfxPropertySetHandle define(OfxParamSetHandle s,const char *key,const char *label,const char *type,const char *group="") {
 OfxPropertySetHandle p;check(param->paramDefine(s,type,key,&p));prop->propSetString(p,kOfxPropLabel,0,label);if(*group)prop->propSetString(p,kOfxParamPropParent,0,group);return p;
}
void numberDef(OfxParamSetHandle s,const char *key,const char *label,double d,double lo,double hi,const char *group) {
 auto p=define(s,key,label,kOfxParamTypeDouble,group);prop->propSetDouble(p,kOfxParamPropDefault,0,d);prop->propSetDouble(p,kOfxParamPropMin,0,lo);prop->propSetDouble(p,kOfxParamPropMax,0,hi);prop->propSetDouble(p,kOfxParamPropDisplayMin,0,lo);prop->propSetDouble(p,kOfxParamPropDisplayMax,0,hi);prop->propSetDouble(p,kOfxParamPropIncrement,0,.0001);prop->propSetInt(p,kOfxParamPropDigits,0,6);
}
void choiceDef(OfxParamSetHandle s,const char *key,const char *label,const char *items,int d,const char *group="") {
 auto p=define(s,key,label,kOfxParamTypeChoice,group);choices(p,items);prop->propSetInt(p,kOfxParamPropDefault,0,d);prop->propSetInt(p,kOfxParamPropAnimates,0,0);
}
OfxPropertySetHandle boolDef(OfxParamSetHandle s,const char *key,const char *label,int d,const char *group="") {auto p=define(s,key,label,kOfxParamTypeBoolean,group);prop->propSetInt(p,kOfxParamPropDefault,0,d);return p;}
OfxStatus describe(OfxImageEffectHandle e) {
 auto p=properties(e);if(interact){OfxPluginEntryPoint *callback=overlayEntry;void *address=nullptr;static_assert(sizeof(callback)==sizeof(address));std::memcpy(&address,&callback,sizeof(address));prop->propSetPointer(p,kOfxImageEffectPluginPropOverlayInteractV1,0,address);}
 prop->propSetString(p,kOfxPropLabel,0,"NegativeScan Studio OFX 0.90.6");prop->propSetString(p,kOfxImageEffectPluginPropGrouping,0,"NegativeScan Studio");prop->propSetString(p,kOfxImageEffectPropSupportedContexts,0,kOfxImageEffectContextFilter);prop->propSetString(p,kOfxImageEffectPropSupportedPixelDepths,0,kOfxBitDepthFloat);prop->propSetString(p,kOfxImageEffectPluginRenderThreadSafety,0,kOfxImageEffectRenderFullySafe);prop->propSetInt(p,kOfxImageEffectPropSupportsTiles,0,1);prop->propSetInt(p,kOfxImageEffectPropSupportsMultiResolution,0,1);prop->propSetInt(p,kOfxImageEffectPluginPropHostFrameThreading,0,0);
 prop->propSetString(p,kOfxImageEffectPropOpenCLRenderSupported,0,nss::CLApi::get().available?"true":"false");
 prop->propSetString(p,kOfxImageEffectPropOpenCLSupported,0,"false");
 prop->propSetString(p,kOfxImageEffectPropCudaRenderSupported,0,nss::CudaApi::get().available?"true":"false");
 prop->propSetString(p,kOfxImageEffectPropCudaStreamSupported,0,"true");return kOfxStatOK;
}
OfxStatus describeContext(OfxImageEffectHandle e) {
 for(auto name:{kOfxImageEffectSimpleSourceClipName,kOfxImageEffectOutputClipName}) {
  OfxPropertySetHandle p;check(effect->clipDefine(e,name,&p));prop->propSetString(p,kOfxImageEffectPropSupportedComponents,0,kOfxImageComponentRGBA);prop->propSetInt(p,kOfxImageEffectPropSupportsTiles,0,1);
 }
 auto s=params(e);
 const char *groups[][2]={{"graypick","Viewer gray / white pick"},{"balance","Film base and balance"},{"metadata","Scan metadata"},{"camera","Camera calibration"},{"density","Density In"},{"positive","Positive Out"},{"render","Rendering"}};
 for(auto &g:groups){auto gp=define(s,g[0],g[1],kOfxParamTypeGroup);if(std::strcmp(g[0],"graypick")==0)prop->propSetInt(gp,kOfxParamPropGroupOpen,0,1);}
 auto armProps=boolDef(s,"pick_on_viewer","Pick gray on viewer",0,"graypick");prop->propSetInt(armProps,kOfxParamPropAnimates,0,0);prop->propSetInt(armProps,kOfxParamPropEvaluateOnChange,0,0);prop->propSetString(armProps,kOfxParamPropHint,0,"Select Open FX Overlay in the viewer, then click/drag a neutral patch. Click enables Auto gray and resets visible Base balances once.");
 boolDef(s,"pick_auto","Auto gray from point",0,"graypick");
 auto point=define(s,"pick_point","Gray / white point",kOfxParamTypeDouble2D,"graypick");
 prop->propSetString(point,kOfxParamPropDoubleType,0,kOfxParamDoubleTypeXYAbsolute);
 prop->propSetString(point,kOfxParamPropDefaultCoordinateSystem,0,kOfxParamCoordinatesNormalised);
 prop->propSetInt(point,kOfxParamPropUseHostOverlayHandle,0,1);
 for(int c=0;c<2;c++)prop->propSetDouble(point,kOfxParamPropDefault,c,.5);
 numberDef(s,"pick_radius","Sample radius (pixels)",3,0,32,"graypick");
 choiceDef(s,"stage","Pipeline output","Density In|Positive Out",1);
 choiceDef(s,"render_device","Render engine","Auto (CUDA/OpenCL if offered)|CPU",0,"render");
 define(s,"render_info","Show render engine",kOfxParamTypePushButton,"render");
 boolDef(s,"auto_metadata","Auto-read Resolve 20 source metadata",1,"metadata");
 auto p=define(s,"metadata_file","Scan TIFF or sidecar JSON",kOfxParamTypeString,"metadata");prop->propSetString(p,kOfxParamPropStringMode,0,kOfxParamStringIsFilePath);prop->propSetString(p,kOfxParamPropDefault,0,"");prop->propSetInt(p,kOfxParamPropAnimates,0,0);
 define(s,"load_metadata","Load from file (or Resolve source)",kOfxParamTypePushButton,"metadata");
 p=define(s,"metadata_status","File status",kOfxParamTypeString,"metadata");prop->propSetString(p,kOfxParamPropDefault,0,"Choose TIFF or JSON");prop->propSetInt(p,kOfxParamPropEnabled,0,0);prop->propSetInt(p,kOfxParamPropAnimates,0,0);prop->propSetInt(p,kOfxParamPropEvaluateOnChange,0,0);
 boolDef(s,"camera_on","Enable camera calibration",1,"camera");boolDef(s,"neutralise","Neutralise grey - bake white balance",0,"camera");numberDef(s,"camera_strength","Camera strength",1,0,1,"camera");
 for(int i=0;i<9;i++)for(auto prefix:{"camera_","neutral_"}) {auto k=std::string(prefix)+std::to_string(i);numberDef(s,k.c_str(),k.c_str(),i%4==0?1:0,-16,16,"camera");}
 choiceDef(s,"stock","Film stock gamma preset","Legacy defaults / manual|Vision3 500T 5219 Status M|Vision3 500T LED estimate|Vision3 250D 5207|UltraMax 400|Portra 400|Gold 200|Ektar 100",0,"balance");
 choiceDef(s,"film_type","Film balance (overridable)","T - tungsten|D - daylight",0,"balance");choiceDef(s,"temperature","Scene illuminant (Planck approx.)","3200 K|3800 K|4400 K|5000 K|5600 K",0,"balance");
 for(auto key:{"reference_base_r","reference_base_g","reference_base_b"}){
  auto hidden=define(s,key,key,kOfxParamTypeDouble,"balance");prop->propSetDouble(hidden,kOfxParamPropDefault,0,.5);prop->propSetInt(hidden,kOfxParamPropSecret,0,1);prop->propSetInt(hidden,kOfxParamPropAnimates,0,0);
 }
 for(auto &d:defs) {
  if(d.kind==Kind::Number)numberDef(s,d.key,d.label,d.def,d.lo,d.hi,d.group);
  else if(d.kind==Kind::Choice)choiceDef(s,d.key,d.label,d.options,int(d.def),d.group);
  else boolDef(s,d.key,d.label,int(d.def),d.group);
 }
 auto page=define(s,"controls","Controls",kOfxParamTypePage);
 std::vector<std::string> children={"pick_on_viewer","pick_auto","pick_point","pick_radius","stage","render_device","render_info","auto_metadata","metadata_file","load_metadata","metadata_status","camera_on","neutralise","camera_strength"};
 for(int i=0;i<9;i++)for(auto prefix:{"camera_","neutral_"})children.push_back(std::string(prefix)+std::to_string(i));
 for(auto key:{"stock","film_type","temperature"})children.push_back(key);
 for(auto &d:defs)children.push_back(d.key);
 for(size_t i=0;i<children.size();i++)prop->propSetString(page,kOfxParamPropPageChild,int(i),children[i].c_str());
 return kOfxStatOK;
}
nss::Vec3 referenceBase(OfxParamSetHandle s,double t){return {float(number(s,"reference_base_r",t)),float(number(s,"reference_base_g",t)),float(number(s,"reference_base_b",t))};}
void writeBase(OfxParamSetHandle s,nss::Vec3 b){
 for(float v:{b.x,b.y,b.z})if(!std::isfinite(v)||v<.0001f||v>16.f)throw std::runtime_error("WB base out of range");
 setD(s,"density_base_r",b.x);setD(s,"density_base_g",b.y);setD(s,"density_base_b",b.z);
}
void updateBase(OfxParamSetHandle s,double t){writeBase(s,nss::balancedBase(referenceBase(s,t),integer(s,"film_type",t),integer(s,"temperature",t)));}
void applyWB(OfxParamSetHandle s,double t) {
 int film=integer(s,"film_type",t),k=integer(s,"temperature",t);if(film<0||film>1||k<0||k>4)throw kOfxStatErrValue;
 updateBase(s,t);setI(s,"auto_metadata",0);
 // Film-base, gamma and visible balance loaders are independent; no hidden stack.
}
void fillMetadata(OfxParamSetHandle s,const nss::Metadata &m,double t) {
 Pipeline p;nss::applyMetadata(p,m);
 setD(s,"reference_base_r",p.density.base_r);setD(s,"reference_base_g",p.density.base_g);setD(s,"reference_base_b",p.density.base_b);
 updateBase(s,t);setI(s,"camera_on",p.cameraOn);setI(s,"neutralise",0);setD(s,"camera_strength",p.cameraStrength);
 if(m.camera)for(int a=0;a<9;a++){auto k="camera_"+std::to_string(a);setD(s,k.c_str(),p.camera[a]);}
 auto b=nss::balancedBase({p.density.base_r,p.density.base_g,p.density.base_b},integer(s,"film_type",t),integer(s,"temperature",t));
 char text[80];std::snprintf(text,sizeof(text),"Loaded: %.2f/%.2f/%.2f",b.x,b.y,b.z);setS(s,"metadata_status",text);
 setI(s,"auto_metadata",0);
}
// A preset can start from newly available source metadata, then remains editable.
void syncReference(OfxImageEffectHandle e,OfxParamSetHandle s,double t){
 if(!integer(s,"auto_metadata",t))return;
 try{auto i=instance(e);auto path=stringParam(s,"metadata_file");if(path.empty())path=sourcePath(e,i);if(path.empty())return;Pipeline p;nss::applyMetadata(p,load(i,path));setD(s,"reference_base_r",p.density.base_r);setD(s,"reference_base_g",p.density.base_g);setD(s,"reference_base_b",p.density.base_b);setI(s,"camera_on",p.cameraOn);setD(s,"camera_strength",p.cameraStrength);setI(s,"neutralise",0);if(p.cameraOn)for(int a=0;a<9;a++){auto k="camera_"+std::to_string(a);setD(s,k.c_str(),p.camera[a]);}}
 catch(const std::exception &x){setS(s,"metadata_status",shortError(x));}
}
void clearPickedBalance(OfxParamSetHandle s){setI(s,"positive_film_wb",0);setD(s,"positive_bal_r",0);setD(s,"positive_bal_g",0);setD(s,"positive_bal_b",0);}
OfxStatus changed(OfxImageEffectHandle e,OfxPropertySetHandle a) {
 if(str(a,kOfxPropChangeReason)!=kOfxChangeUserEdited)return kOfxStatReplyDefault;
 auto s=params(e);auto key=str(a,kOfxPropName);double time=0;prop->propGetDouble(a,kOfxPropTime,0,&time);
 if(key=="load_metadata") {
  try{auto i=instance(e);auto path=stringParam(s,"metadata_file");if(path.empty())path=sourcePath(e,i,a);auto m=load(i,path,true);Edit edit(s,"Load scan metadata");fillMetadata(s,m,time);}
  catch(const std::exception &x){setS(s,"metadata_status",shortError(x));return kOfxStatOK;}
  catch(...){setS(s,"metadata_status","Cannot read file path");return kOfxStatOK;}
 }else if(key=="positive_film_wb"){
  int selected=integer(s,"positive_film_wb",time);
  if(selected<0||selected>=int(sizeof(nss::filmBases)/sizeof(nss::filmBases[0])))throw kOfxStatErrValue;
  auto base=nss::filmBaseGains(selected);Edit edit(s,"Load film Base balance");
  setD(s,"positive_bal_r",base.r);setD(s,"positive_bal_g",base.g);setD(s,"positive_bal_b",base.b);
 }else if(key=="pick_point"&&integer(s,"pick_on_viewer",time)&&!integer(s,"pick_auto",time)){
  Edit edit(s,"Pick gray with host handle");clearPickedBalance(s);setI(s,"pick_auto",1);
 }else if(key=="pick_auto"&&integer(s,"pick_auto",time)){
  // A fresh neutral pick replaces Base balance, visibly; subsequent manual tweaks remain.
  Edit edit(s,"Start picked gray balance");clearPickedBalance(s);
 }else if(key=="film_type"||key=="temperature"){
  Edit edit(s,"Film-base WB preset");syncReference(e,s,time);applyWB(s,time);
 }else if(key=="stock"){
  int stock=integer(s,"stock",time);
  static const float gammas[][3]={{.55f,.635f,.62f},{.4482f,.5266f,.5096f},{.45f,.54f,.53f},{.48148148f,.56018519f,.54166667f},
#include "still_gammas.inc"
  };
  if(stock<0||stock>=int(sizeof(gammas)/sizeof(gammas[0])))throw kOfxStatErrValue;
  Edit edit(s,"Film stock gamma");
  for(int c=0;c<3;c++){auto k=std::string("positive_gam_")+"rgb"[c];setD(s,k.c_str(),gammas[stock][c]);}
  if(stock){syncReference(e,s,time);setI(s,"film_type",stock<=2?0:1);applyWB(s,time);}
 }else if(key=="density_base_r"||key=="density_base_g"||key=="density_base_b"){
  setI(s,"auto_metadata",0);
 }else if(key=="render_device"){
  bool automatic=integer(s,"render_device",time)==0;
  prop->propSetString(properties(e),kOfxImageEffectPropOpenCLRenderSupported,0,automatic&&nss::CLApi::get().available?"true":"false");
  prop->propSetString(properties(e),kOfxImageEffectPropCudaRenderSupported,0,automatic&&nss::CudaApi::get().available?"true":"false");
 }else if(key=="render_info"){
  auto i=instance(e);std::string text;{std::lock_guard<std::mutex> lock(i->mutex);text=i->lastRender;}
  text+="\n"+nss::CudaApi::get().availability+"\nOpenCL runtime: "+(nss::CLApi::get().available?"available":"unavailable");
  if(message)message->message(e,kOfxMessageMessage,"nss.render","%s",text.c_str());
 }
 return kOfxStatOK;
}
#include "overlay.hpp"
struct Image {
 OfxPropertySetHandle p=nullptr;float *data=nullptr;int row=0,bounds[4]{};
 ~Image(){if(p)effect->clipReleaseImage(p);}
 void get(OfxImageClipHandle c,double t,const OfxRectD *region=nullptr){check(effect->clipGetImage(c,t,region,&p));
  if(str(p,kOfxImageEffectPropPixelDepth)!=kOfxBitDepthFloat||str(p,kOfxImageEffectPropComponents)!=kOfxImageComponentRGBA)throw kOfxStatErrFormat;
  check(prop->propGetPointer(p,kOfxImagePropData,0,reinterpret_cast<void**>(&data)));check(prop->propGetInt(p,kOfxImagePropRowBytes,0,&row));check(prop->propGetIntN(p,kOfxImagePropBounds,4,bounds));if(!data)throw kOfxStatFailed;
 }
 float *at(int x,int y){if(x<bounds[0]||x>=bounds[2]||y<bounds[1]||y>=bounds[3])return nullptr;return reinterpret_cast<float*>(reinterpret_cast<char*>(data)+ptrdiff_t(y-bounds[1])*row)+4*(x-bounds[0]);}
};
OfxRectD fetchRegion(OfxImageClipHandle clip,OfxParamSetHandle s,double time,const int *win,OfxPropertySetHandle args,bool includePick){
 double sx=1,sy=1,aspect=1;prop->propGetDouble(args,kOfxImageEffectPropRenderScale,0,&sx);prop->propGetDouble(args,kOfxImageEffectPropRenderScale,1,&sy);
 if(!(sx>0))sx=1;
 if(!(sy>0))sy=1;
 OfxPropertySetHandle props=nullptr;
 if(effect->clipGetPropertySet(clip,&props)==kOfxStatOK)prop->propGetDouble(props,kOfxImagePropPixelAspectRatio,0,&aspect);
 if(!(aspect>0))aspect=1;
 OfxRectD roi{win[0]*aspect/sx,win[1]/sy,win[2]*aspect/sx,win[3]/sy};
 if(includePick){double x=0,y=0;check(param->paramGetValueAtTime(handle(s,"pick_point"),time,&x,&y));double r=std::clamp(number(s,"pick_radius",time),0.,32.)+2;
  if(std::isfinite(x)&&std::isfinite(y)){roi.x1=std::min(roi.x1,x-r*aspect/sx);roi.x2=std::max(roi.x2,x+r*aspect/sx);roi.y1=std::min(roi.y1,y-r/sy);roi.y2=std::max(roi.y2,y+r/sy);}}
 return roi;
}
void cpuThread(unsigned int index,unsigned int count,void *arg){static_cast<nss::CpuJob*>(arg)->rows(index,count);}
OfxStatus render(OfxImageEffectHandle e,OfxPropertySetHandle a) {
 double time=0;check(prop->propGetDouble(a,kOfxPropTime,0,&time));int win[4];check(prop->propGetIntN(a,kOfxImageEffectPropRenderWindow,4,win));
 int gpu=0,cuda=0;prop->propGetInt(a,kOfxImageEffectPropOpenCLEnabled,0,&gpu);prop->propGetInt(a,kOfxImageEffectPropCudaEnabled,0,&cuda);
 if(gpu&&cuda)return kOfxStatGPURenderFailed;
 auto i=instance(e);auto s=params(e);bool pick=integer(s,"pick_auto",time)!=0;auto p=readParams(s,time);
 auto sourceRegion=fetchRegion(i->src,s,time,win,a,pick),destRegion=fetchRegion(i->dst,s,time,win,a,false);
 Image src,dst;src.get(i->src,time,&sourceRegion);dst.get(i->dst,time,&destRegion);
 if(integer(s,"auto_metadata",time)){
  auto path=stringParam(s,"metadata_file");if(path.empty())path=sourcePath(e,i,a,src.p);
  if(!path.empty())try{nss::applyMetadata(p,load(i,path));auto b=nss::balancedBase({p.density.base_r,p.density.base_g,p.density.base_b},integer(s,"film_type",time),integer(s,"temperature",time));p.density.base_r=b.x;p.density.base_g=b.y;p.density.base_b=b.z;}catch(const std::exception &){/* Old/no-info scans retain manual controls. Load reports the reason. */}
 }
 bool premult=str(src.p,kOfxImageEffectPropPreMultiplication)==kOfxImagePreMultiplied;
 void *queue=nullptr;if(gpu)prop->propGetPointer(a,kOfxImageEffectPropOpenCLCommandQueue,0,&queue);
 if((gpu||cuda)&&integer(s,"render_device",time)!=0)return kOfxStatGPURenderFailed;
 void *cudaStream=nullptr;bool cudaStreamProvided=cuda&&prop->propGetPointer(a,kOfxImageEffectPropCudaStream,0,&cudaStream)==kOfxStatOK;
 bool pickUnavailable=false;
 if(pick){
  double x=0,y=0,scaleX=1,scaleY=1,aspect=1;
  check(param->paramGetValueAtTime(handle(s,"pick_point"),time,&x,&y));
  prop->propGetDouble(a,kOfxImageEffectPropRenderScale,0,&scaleX);prop->propGetDouble(a,kOfxImageEffectPropRenderScale,1,&scaleY);
  prop->propGetDouble(src.p,kOfxImagePropPixelAspectRatio,0,&aspect);if(!(aspect>0))aspect=1;
  int rect[4],radius=int(std::clamp(number(s,"pick_radius",time),0.,32.));
  double px=std::floor(x*scaleX/aspect),py=std::floor(y*scaleY);
  if(!std::isfinite(px)||!std::isfinite(py)||px<INT32_MIN+64.||px>INT32_MAX-64.||py<INT32_MIN+64.||py>INT32_MAX-64.||!nss::pickRect(src.bounds,int(px),int(py),radius,rect))pickUnavailable=true;
  else if(gpu||cuda){
   try{auto patch=cuda?i->cuda.readPatch(cudaStream,src.data,src.bounds,src.row,rect):i->opencl.readPatch(queue,src.data,src.bounds,src.row,rect);nss::FrameView view{patch.data(),{rect[0],rect[1],rect[2],rect[3]},(rect[2]-rect[0])*4*int(sizeof(float))};pickUnavailable=!nss::grayFromPatch(p,view,rect,premult);}
   catch(const std::exception &x){setLastRender(i,x.what());return kOfxStatGPURenderFailed;}
  }else{nss::FrameView view{src.data,{src.bounds[0],src.bounds[1],src.bounds[2],src.bounds[3]},src.row};pickUnavailable=!nss::grayFromPatch(p,view,rect,premult);}
 }
 auto prepared=i->preparedCache.get(p);
 if(cuda){
  try{i->cuda.enqueue(cudaStream,cudaStreamProvided,src.data,dst.data,src.bounds,dst.bounds,src.row,dst.row,win,premult,prepared);setLastRender(i,std::string(cudaStreamProvided?"CUDA PTX (host stream)":"CUDA PTX (default stream waited)")+(pickUnavailable?"; pick unavailable":""));return kOfxStatOK;}
  catch(const std::exception &x){setLastRender(i,std::string("CUDA failed; select CPU. ")+x.what());return kOfxStatGPURenderFailed;}
 }
 if(gpu){
  try{if(integer(s,"render_device",time)!=0)throw std::runtime_error("CPU requested");i->opencl.enqueue(queue,src.data,dst.data,src.bounds,dst.bounds,src.row,dst.row,win,premult,prepared);setLastRender(i,std::string("OpenCL buffers (host queue)")+(pickUnavailable?"; pick unavailable":""));return kOfxStatOK;}
  catch(const std::exception &x){setLastRender(i,std::string("OpenCL failed; select CPU. ")+x.what());return kOfxStatGPURenderFailed;}
 }
 nss::FrameView source{src.data,{src.bounds[0],src.bounds[1],src.bounds[2],src.bounds[3]},src.row};
 nss::FrameView dest{dst.data,{dst.bounds[0],dst.bounds[1],dst.bounds[2],dst.bounds[3]},dst.row};
 nss::CpuJob job{prepared,source,dest,win,premult,[](void *e){return effect->abort(static_cast<OfxImageEffectHandle>(e));},e};
 unsigned count=nss::cpuThreads(win[3]-win[1]);bool done=false;
 if(multithread){unsigned available=1;if(multithread->multiThreadNumCPUs(&available)==kOfxStatOK)count=std::min(count,std::max(available,1u));done=multithread->multiThread(cpuThread,count,&job)==kOfxStatOK;}
 if(!done)nss::parallelCpu(job,count);
 setLastRender(i,"CPU "+std::to_string(count)+" threads"+(pickUnavailable?"; pick unavailable":""));return kOfxStatOK;
}
OfxStatus regionsOfInterest(OfxImageEffectHandle e,OfxPropertySetHandle in,OfxPropertySetHandle out){
 double time=0;prop->propGetDouble(in,kOfxPropTime,0,&time);auto s=params(e);
 if(!integer(s,"pick_auto",time))return kOfxStatReplyDefault;
 double roi[4]{},x=0,y=0,sx=1,sy=1;for(int c=0;c<4;c++)check(prop->propGetDouble(in,kOfxImageEffectPropRegionOfInterest,c,roi+c));
 check(param->paramGetValueAtTime(handle(s,"pick_point"),time,&x,&y));
 prop->propGetDouble(in,kOfxImageEffectPropRenderScale,0,&sx);prop->propGetDouble(in,kOfxImageEffectPropRenderScale,1,&sy);
 if(!std::isfinite(x)||!std::isfinite(y)||!(sx>0)||!(sy>0))return kOfxStatReplyDefault;
 double radius=std::clamp(number(s,"pick_radius",time),0.,32.)+2,aspect=1;
 OfxPropertySetHandle clip=nullptr;if(effect->clipGetPropertySet(instance(e)->src,&clip)==kOfxStatOK)prop->propGetDouble(clip,kOfxImagePropPixelAspectRatio,0,&aspect);
 if(!(aspect>0))aspect=1;
 double rx=radius*aspect/sx,ry=radius/sy;
 roi[0]=std::min(roi[0],x-rx);roi[1]=std::min(roi[1],y-ry);roi[2]=std::max(roi[2],x+rx);roi[3]=std::max(roi[3],y+ry);
 for(int c=0;c<4;c++)prop->propSetDouble(out,"OfxImageClipPropRoI_Source",c,roi[c]);
 return kOfxStatOK;
}
void setHost(OfxHost *h){host=h;}
OfxStatus entry(const char *action,const void *handle_,OfxPropertySetHandle in,OfxPropertySetHandle out) {
 auto e=const_cast<OfxImageEffectStruct*>(static_cast<const OfxImageEffectStruct*>(handle_));
 try {
  if(!std::strcmp(action,kOfxActionLoad)) {
   prop=static_cast<OfxPropertySuiteV1*>(const_cast<void*>(host->fetchSuite(host->host,kOfxPropertySuite,1)));
   effect=static_cast<OfxImageEffectSuiteV1*>(const_cast<void*>(host->fetchSuite(host->host,kOfxImageEffectSuite,1)));
   param=static_cast<OfxParameterSuiteV1*>(const_cast<void*>(host->fetchSuite(host->host,kOfxParameterSuite,1)));
   interact=static_cast<OfxInteractSuiteV1*>(const_cast<void*>(host->fetchSuite(host->host,kOfxInteractSuite,1)));
   multithread=static_cast<OfxMultiThreadSuiteV1*>(const_cast<void*>(host->fetchSuite(host->host,kOfxMultiThreadSuite,1)));
   message=static_cast<OfxMessageSuiteV1*>(const_cast<void*>(host->fetchSuite(host->host,kOfxMessageSuite,1)));
   return prop&&effect&&param?kOfxStatOK:kOfxStatErrMissingHostFeature;
  }
  if(!std::strcmp(action,kOfxActionDescribe))return describe(e);
  if(!std::strcmp(action,kOfxImageEffectActionDescribeInContext))return describeContext(e);
  if(!std::strcmp(action,kOfxActionCreateInstance)) {
   auto i=std::make_unique<Instance>();check(effect->clipGetHandle(e,kOfxImageEffectSimpleSourceClipName,&i->src,nullptr));check(effect->clipGetHandle(e,kOfxImageEffectOutputClipName,&i->dst,nullptr));check(prop->propSetPointer(properties(e),kOfxPropInstanceData,0,i.get()));i.release();return kOfxStatOK;
  }
  if(!std::strcmp(action,kOfxActionDestroyInstance)){delete instance(e);prop->propSetPointer(properties(e),kOfxPropInstanceData,0,nullptr);return kOfxStatOK;}
  if(!std::strcmp(action,kOfxImageEffectActionGetRegionsOfInterest))return regionsOfInterest(e,in,out);
  if(!std::strcmp(action,kOfxImageEffectActionRender))return render(e,in);
  if(!std::strcmp(action,kOfxActionInstanceChanged))return changed(e,in);
  if(!std::strcmp(action,kOfxActionUnload))return kOfxStatOK;
  return kOfxStatReplyDefault;
 }catch(OfxStatus s){return s;}catch(const std::bad_alloc &){return kOfxStatErrMemory;}catch(...){return kOfxStatFailed;}
}
OfxPlugin plugin={kOfxImageEffectPluginApi,1,"org.negativescanstudio.complete",0,9006,setHost,entry};
}
extern "C" {
#ifdef _WIN32
__declspec(dllexport)
#endif
int OfxGetNumberOfPlugins(){return 1;}
#ifdef _WIN32
__declspec(dllexport)
#endif
OfxPlugin *OfxGetPlugin(int i){return i==0?&plugin:nullptr;}
}
