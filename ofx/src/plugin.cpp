#include "ofxImageEffect.h"
#include "ofxProperty.h"
#include "metadata.hpp"
#include "wb_presets.hpp"
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
};
Instance *instance(OfxImageEffectHandle e){void *p=nullptr;check(prop->propGetPointer(properties(e),kOfxPropInstanceData,0,&p));if(!p)throw kOfxStatErrBadHandle;return static_cast<Instance*>(p);}
std::string sourcePath(OfxImageEffectHandle e,Instance *i,OfxPropertySetHandle args=nullptr,OfxPropertySetHandle image=nullptr) {
 // Resolve 20 extension, not standard OFX. Probe only host-provided properties.
 for(auto p:{args,image,properties(e)}){auto s=str(p,"OfxImageEffectPropSrcFilePath");if(!s.empty())return s;}
 OfxPropertySetHandle p=nullptr;effect->clipGetPropertySet(i->src,&p);return str(p,"OfxImageEffectPropSrcFilePath");
}
nss::Metadata load(Instance *i,const std::string &path) {
 if(path.empty())throw std::runtime_error("No source filename: select a TIFF/JSON and press Load from file");
 std::lock_guard<std::mutex> lock(i->mutex);
 auto stamp=std::filesystem::last_write_time(std::filesystem::u8path(path));
 if(i->path==path&&i->meta&&i->stamp==stamp)return *i->meta;
 auto m=nss::parseMetadata(nss::readDescription(path));i->path=path;i->stamp=stamp;i->meta=m;return m;
}
Pipeline readParams(OfxParamSetHandle set,double time) {
 Pipeline p;
#include "read_params.inc"
 p.stage=integer(set,"stage",time);p.cameraOn=integer(set,"camera_on",time);p.neutralise=integer(set,"neutralise",time);p.cameraStrength=number(set,"camera_strength",time);
 for(int a=0;a<9;a++){auto k="camera_"+std::to_string(a);p.camera[a]=number(set,k.c_str(),time);k="neutral_"+std::to_string(a);p.neutral[a]=number(set,k.c_str(),time);}
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
void boolDef(OfxParamSetHandle s,const char *key,const char *label,int d,const char *group="") {auto p=define(s,key,label,kOfxParamTypeBoolean,group);prop->propSetInt(p,kOfxParamPropDefault,0,d);}
OfxStatus describe(OfxImageEffectHandle e) {
 auto p=properties(e);prop->propSetString(p,kOfxPropLabel,0,"NegativeScan Studio");prop->propSetString(p,kOfxImageEffectPluginPropGrouping,0,"NegativeScan Studio");prop->propSetString(p,kOfxImageEffectPropSupportedContexts,0,kOfxImageEffectContextFilter);prop->propSetString(p,kOfxImageEffectPropSupportedPixelDepths,0,kOfxBitDepthFloat);prop->propSetString(p,kOfxImageEffectPluginRenderThreadSafety,0,kOfxImageEffectRenderFullySafe);prop->propSetInt(p,kOfxImageEffectPropSupportsTiles,0,1);prop->propSetInt(p,kOfxImageEffectPropSupportsMultiResolution,0,1);return kOfxStatOK;
}
OfxStatus describeContext(OfxImageEffectHandle e) {
 for(auto name:{kOfxImageEffectSimpleSourceClipName,kOfxImageEffectOutputClipName}) {
  OfxPropertySetHandle p;check(effect->clipDefine(e,name,&p));prop->propSetString(p,kOfxImageEffectPropSupportedComponents,0,kOfxImageComponentRGBA);prop->propSetInt(p,kOfxImageEffectPropSupportsTiles,0,1);
 }
 auto s=params(e);
 for(auto g:{"metadata","camera","density","cross","positive","balance"})define(s,g,g,kOfxParamTypeGroup);
 choiceDef(s,"stage","Pipeline output","Density In|Crosstalk|Positive Out",2);
 boolDef(s,"auto_metadata","Auto-read Resolve 20 source metadata",1,"metadata");
 auto p=define(s,"metadata_file","Scan TIFF or sidecar JSON",kOfxParamTypeString,"metadata");prop->propSetString(p,kOfxParamPropStringMode,0,kOfxParamStringIsFilePath);prop->propSetString(p,kOfxParamPropDefault,0,"");prop->propSetInt(p,kOfxParamPropAnimates,0,0);
 define(s,"load_metadata","Load from file (or Resolve source)",kOfxParamTypePushButton,"metadata");
 p=define(s,"metadata_status","Metadata status / light drive RGB",kOfxParamTypeString,"metadata");prop->propSetString(p,kOfxParamPropDefault,0,"Manual base until valid NSS metadata is available");prop->propSetInt(p,kOfxParamPropEnabled,0,0);prop->propSetInt(p,kOfxParamPropAnimates,0,0);prop->propSetInt(p,kOfxParamPropEvaluateOnChange,0,0);
 boolDef(s,"camera_on","Enable camera calibration",1,"camera");boolDef(s,"neutralise","Neutralise grey - bake white balance",0,"camera");numberDef(s,"camera_strength","Camera strength",1,0,1,"camera");
 for(int i=0;i<9;i++)for(auto prefix:{"camera_","neutral_"}) {auto k=std::string(prefix)+std::to_string(i);numberDef(s,k.c_str(),k.c_str(),i%4==0?1:0,-16,16,"camera");}
 choiceDef(s,"stock","Film stock gamma preset","Legacy defaults / manual|Vision3 500T 5219 Status M|Vision3 500T LED estimate|Vision3 250D 5207|UltraMax 400|Portra 400|Gold 200",0,"balance");
 choiceDef(s,"film_type","Film balance (overridable)","T - tungsten|D - daylight",0,"balance");choiceDef(s,"temperature","Scene illuminant (Planck approx.)","3200 K|3800 K|4400 K|5000 K|5600 K",0,"balance");
 define(s,"apply_wb","Apply T/D + temperature preset",kOfxParamTypePushButton,"balance");
 for(auto &d:defs) {
  if(d.kind==Kind::Number)numberDef(s,d.key,d.label,d.def,d.lo,d.hi,d.group);
  else if(d.kind==Kind::Choice)choiceDef(s,d.key,d.label,d.options,int(d.def),d.group);
  else boolDef(s,d.key,d.label,int(d.def),d.group);
 }
 auto page=define(s,"controls","Controls",kOfxParamTypePage);
 std::vector<std::string> children={"stage","auto_metadata","metadata_file","load_metadata","metadata_status","camera_on","neutralise","camera_strength"};
 for(int i=0;i<9;i++)for(auto prefix:{"camera_","neutral_"})children.push_back(std::string(prefix)+std::to_string(i));
 for(auto key:{"stock","film_type","temperature","apply_wb"})children.push_back(key);
 for(auto &d:defs)children.push_back(d.key);
 for(size_t i=0;i<children.size();i++)prop->propSetString(page,kOfxParamPropPageChild,int(i),children[i].c_str());
 return kOfxStatOK;
}
void applyWB(OfxParamSetHandle s,double t) {
 int film=integer(s,"film_type",t),k=integer(s,"temperature",t);if(film<0||film>1||k<0||k>4)throw kOfxStatErrValue;
 for(int c=0;c<3;c++){auto key=std::string("density_cast_")+"rgb"[c];setD(s,key.c_str(),nss::wbOffsets[film][k][c]);}
 setI(s,"density_cast_on",1);
 // Do not accidentally stack legacy tungsten Positive Out WB with new offsets.
 setI(s,"positive_film_wb",0);
}
void fillMetadata(OfxParamSetHandle s,const nss::Metadata &m) {
 Pipeline p;nss::applyMetadata(p,m);
 setD(s,"density_base_r",p.density.base_r);setD(s,"density_base_g",p.density.base_g);setD(s,"density_base_b",p.density.base_b);
 setI(s,"camera_on",p.cameraOn);setI(s,"neutralise",0);setD(s,"camera_strength",p.cameraStrength);
 if(m.camera)for(int a=0;a<9;a++){auto k="camera_"+std::to_string(a);setD(s,k.c_str(),p.camera[a]);}
 std::string text="Loaded measured film base; light drive RGB is provenance only";
 if(m.light)text+=": "+std::to_string((*m.light)[0])+" / "+std::to_string((*m.light)[1])+" / "+std::to_string((*m.light)[2]);
 setS(s,"metadata_status",text);
 // Loaded controls are now an editable snapshot. Re-enable Auto for per-file reads.
 setI(s,"auto_metadata",0);
}
OfxStatus changed(OfxImageEffectHandle e,OfxPropertySetHandle a) {
 if(str(a,kOfxPropChangeReason)!=kOfxChangeUserEdited)return kOfxStatReplyDefault;
 auto s=params(e);auto key=str(a,kOfxPropName);double time=0;prop->propGetDouble(a,kOfxPropTime,0,&time);
 if(key=="load_metadata") {
  try {auto i=instance(e);auto p=stringParam(s,"metadata_file");if(p.empty())p=sourcePath(e,i,a);auto m=load(i,p);Edit edit(s,"Load scan metadata");fillMetadata(s,m);}
  catch(const std::exception &x){setS(s,"metadata_status",x.what());if(message)message->message(e,kOfxMessageError,"nss.metadata","%s",x.what());return kOfxStatOK;}
 } else if(key=="apply_wb"||key=="film_type"||key=="temperature") {
  Edit edit(s,"Illuminant density compensation");applyWB(s,time);
 } else if(key=="stock") {
  int stock=integer(s,"stock",time);
  // Generated from official-curve readings (see docs/data/ofx_stock_gamma.md).
  static const float gammas[][3]={{.55f,.635f,.62f},{.4482f,.5266f,.5096f},{.45f,.54f,.53f},{.48148148f,.56018519f,.54166667f},
#include "still_gammas.inc"
  };
  if(stock<0||stock>6)throw kOfxStatErrValue;
  Edit edit(s,"Film stock gamma");
  for(int c=0;c<3;c++){auto k=std::string("positive_gam_")+"rgb"[c];setD(s,k.c_str(),gammas[stock][c]);}
  if(stock){setI(s,"film_type",stock<=2?0:1);applyWB(s,time);}
 }
 return kOfxStatOK;
}
struct Image {
 OfxPropertySetHandle p=nullptr;float *data=nullptr;int row=0,bounds[4]{};
 ~Image(){if(p)effect->clipReleaseImage(p);}
 void get(OfxImageClipHandle c,double t){check(effect->clipGetImage(c,t,nullptr,&p));
  if(str(p,kOfxImageEffectPropPixelDepth)!=kOfxBitDepthFloat||str(p,kOfxImageEffectPropComponents)!=kOfxImageComponentRGBA)throw kOfxStatErrFormat;
  check(prop->propGetPointer(p,kOfxImagePropData,0,reinterpret_cast<void**>(&data)));check(prop->propGetInt(p,kOfxImagePropRowBytes,0,&row));check(prop->propGetIntN(p,kOfxImagePropBounds,4,bounds));if(!data)throw kOfxStatFailed;
 }
 float *at(int x,int y){if(x<bounds[0]||x>=bounds[2]||y<bounds[1]||y>=bounds[3])return nullptr;return reinterpret_cast<float*>(reinterpret_cast<char*>(data)+ptrdiff_t(y-bounds[1])*row)+4*(x-bounds[0]);}
};
OfxStatus render(OfxImageEffectHandle e,OfxPropertySetHandle a) {
 double time=0;check(prop->propGetDouble(a,kOfxPropTime,0,&time));int win[4];check(prop->propGetIntN(a,kOfxImageEffectPropRenderWindow,4,win));
 auto i=instance(e);Image src,dst;src.get(i->src,time);dst.get(i->dst,time);auto s=params(e);auto p=readParams(s,time);
 if(integer(s,"auto_metadata",time)) {
  auto path=stringParam(s,"metadata_file");bool explicitFile=!path.empty();if(path.empty())path=sourcePath(e,i,a,src.p);
  if(!path.empty()) {try {nss::applyMetadata(p,load(i,path));}catch(const std::exception &x){if(explicitFile){if(message)message->message(e,kOfxMessageError,"nss.metadata","%s",x.what());return kOfxStatFailed;} /* Old/no-metadata source: retain manual values. */}}
 }
 bool premult=str(src.p,kOfxImageEffectPropPreMultiplication)==kOfxImagePreMultiplied;
 for(int y=win[1];y<win[3];y++){
  if(effect->abort(e))break;
  for(int x=win[0];x<win[2];x++){
   float *o=dst.at(x,y),*v=src.at(x,y);if(!o)continue;if(!v){for(int c=0;c<4;c++)o[c]=0;continue;}
   float alpha=v[3];nss::Vec3 rgb{v[0],v[1],v[2]};
   if(premult&&alpha!=0){rgb.x/=alpha;rgb.y/=alpha;rgb.z/=alpha;}
   rgb=p.pixel(rgb);if(premult){rgb.x*=alpha;rgb.y*=alpha;rgb.z*=alpha;}
   o[0]=rgb.x;o[1]=rgb.y;o[2]=rgb.z;o[3]=alpha;
  }
 }
 return kOfxStatOK;
}
void setHost(OfxHost *h){host=h;}
OfxStatus entry(const char *action,const void *handle_,OfxPropertySetHandle in,OfxPropertySetHandle) {
 auto e=const_cast<OfxImageEffectStruct*>(static_cast<const OfxImageEffectStruct*>(handle_));
 try {
  if(!std::strcmp(action,kOfxActionLoad)) {
   prop=static_cast<OfxPropertySuiteV1*>(const_cast<void*>(host->fetchSuite(host->host,kOfxPropertySuite,1)));
   effect=static_cast<OfxImageEffectSuiteV1*>(const_cast<void*>(host->fetchSuite(host->host,kOfxImageEffectSuite,1)));
   param=static_cast<OfxParameterSuiteV1*>(const_cast<void*>(host->fetchSuite(host->host,kOfxParameterSuite,1)));
   message=static_cast<OfxMessageSuiteV1*>(const_cast<void*>(host->fetchSuite(host->host,kOfxMessageSuite,1)));
   return prop&&effect&&param?kOfxStatOK:kOfxStatErrMissingHostFeature;
  }
  if(!std::strcmp(action,kOfxActionDescribe))return describe(e);
  if(!std::strcmp(action,kOfxImageEffectActionDescribeInContext))return describeContext(e);
  if(!std::strcmp(action,kOfxActionCreateInstance)) {
   auto i=std::make_unique<Instance>();check(effect->clipGetHandle(e,kOfxImageEffectSimpleSourceClipName,&i->src,nullptr));check(effect->clipGetHandle(e,kOfxImageEffectOutputClipName,&i->dst,nullptr));check(prop->propSetPointer(properties(e),kOfxPropInstanceData,0,i.get()));i.release();return kOfxStatOK;
  }
  if(!std::strcmp(action,kOfxActionDestroyInstance)){delete instance(e);prop->propSetPointer(properties(e),kOfxPropInstanceData,0,nullptr);return kOfxStatOK;}
  if(!std::strcmp(action,kOfxImageEffectActionRender))return render(e,in);
  if(!std::strcmp(action,kOfxActionInstanceChanged))return changed(e,in);
  if(!std::strcmp(action,kOfxActionUnload))return kOfxStatOK;
  return kOfxStatReplyDefault;
 }catch(OfxStatus s){return s;}catch(const std::bad_alloc &){return kOfxStatErrMemory;}catch(...){return kOfxStatFailed;}
}
OfxPlugin plugin={kOfxImageEffectPluginApi,1,"org.negativescanstudio.complete",0,90,setHost,entry};
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
