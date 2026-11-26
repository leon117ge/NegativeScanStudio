#include "ofxImageEffect.h"
#include "ofxProperty.h"
#include "ofxInteract.h"
#include "overlay_gl.hpp"
#include "metadata.hpp"
#include "wb_presets.hpp"
#include "balance_presets.hpp"
#include "ofxGPURender.h"
#include "opencl.hpp"
#include "cuda.hpp"
#include "metal.hpp"
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

// macOS shows the Rendering group (Render engine + Show render engine) so Metal and CPU can be
// compared in the host. Windows/Linux keep the 0.90.14 hidden controls and labels exactly.
// Choice indices are shared by every platform: 0 = Auto, 1 = CPU (saved projects keep meaning).
#ifndef NSS_MAC_RENDER_UI
#if defined(__APPLE__)
#define NSS_MAC_RENDER_UI 1
#else
#define NSS_MAC_RENDER_UI 0
#endif
#endif
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
// Default X-T3 trichrome matrix (convert/scan.go DefaultXT3Trichrome) and
// MV-CS240-90UM neutral-preserving chart matrix (SoftLoaf No. 000004 chart_hik2, mean dE ~2.16, row sums = 1).
constexpr std::array<double,9> kXT3Matrix={
 0.86426, 0.10467,-0.01969,
-0.05068, 0.98176, 0.01644,
 0.11287,-0.14623, 1.04095
};
constexpr std::array<double,9> kMonoMatrix={
 0.942661, 0.195781,-0.138442,
-0.056435, 1.036487, 0.019948,
 0.014258,-0.089212, 1.074954
};
inline OfxPropertySetHandle hideParam(OfxPropertySetHandle p){prop->propSetInt(p,kOfxParamPropSecret,0,1);return p;}
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
 // Metal: async command-buffer status, and the per-instance withdrawal after any Metal failure.
 std::shared_ptr<nss::MetalStatus> metal=std::make_shared<nss::MetalStatus>();std::atomic<bool> metalWithdrawn{false};
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
// Never lets a Metal probe fail describe, createInstance or instanceChanged.
bool metalAvailable(){try{return nss::MetalApi::get().available;}catch(...){return false;}}
// Stop offering Metal for this instance after a failure; later frames come as CPU buffers.
void withdrawMetal(OfxImageEffectHandle e,Instance *i){i->metalWithdrawn=true;OfxPropertySetHandle p=nullptr;if(effect->getPropertySet(e,&p)==kOfxStatOK&&p)prop->propSetString(p,kOfxImageEffectPropMetalRenderSupported,0,"false");}
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
OfxPropertySetHandle numberDef(OfxParamSetHandle s,const char *key,const char *label,double d,double lo,double hi,const char *group) {
 auto p=define(s,key,label,kOfxParamTypeDouble,group);prop->propSetDouble(p,kOfxParamPropDefault,0,d);prop->propSetDouble(p,kOfxParamPropMin,0,lo);prop->propSetDouble(p,kOfxParamPropMax,0,hi);prop->propSetDouble(p,kOfxParamPropDisplayMin,0,lo);prop->propSetDouble(p,kOfxParamPropDisplayMax,0,hi);prop->propSetDouble(p,kOfxParamPropIncrement,0,.0001);prop->propSetInt(p,kOfxParamPropDigits,0,6);return p;
}
OfxPropertySetHandle choiceDef(OfxParamSetHandle s,const char *key,const char *label,const char *items,int d,const char *group="") {
 auto p=define(s,key,label,kOfxParamTypeChoice,group);choices(p,items);prop->propSetInt(p,kOfxParamPropDefault,0,d);prop->propSetInt(p,kOfxParamPropAnimates,0,0);return p;
}
OfxPropertySetHandle boolDef(OfxParamSetHandle s,const char *key,const char *label,int d,const char *group="") {auto p=define(s,key,label,kOfxParamTypeBoolean,group);prop->propSetInt(p,kOfxParamPropDefault,0,d);return p;}
const char *toneHint(const char *key){
 std::string k=key;
 if(k=="tone_shoulder_on")return "Soft highlight roll-off on the positive. Applied to the RGB mean in log2 stops around 18% gray with one gain for R, G and B, so hue and channel ratios stay unchanged. Off renders highlights as 0.90.11.";
 if(k=="tone_shoulder_strength")return "0 = no shoulder, 1 = full shoulder curve.";
 if(k=="tone_shoulder_pivot")return "Stops above 18% gray where the shoulder starts. Below this point nothing changes (slope 1, no kink).";
 if(k=="tone_shoulder_white")return "Stops above 18% gray that very bright highlights approach but never reach. Must be above Shoulder start (minimum gap 0.05).";
 if(k=="tone_toe_on")return "Soft shadow toe on the positive, driven by the darkest of R, G and B. Adds the same small amount to all three, so hue is kept, deep shadows desaturate slightly and blacks lift to a soft neutral floor. Off renders shadows as 0.90.11.";
 if(k=="tone_toe_strength")return "0 = no toe, 1 = full toe curve.";
 if(k=="tone_toe_pivot")return "Stops below 18% gray (darkest channel) where the toe starts. Above this point nothing changes. Never above Shoulder start.";
 if(k=="tone_toe_black")return "Stops below 18% gray of the lifted black floor. Pure black lands here at full strength.";
 if(k=="tone_highlight_desat_on")return "Switch highlight desaturation on or off. Off renders exactly like amount 0.";
 if(k=="tone_highlight_cream_on")return "Switch the warm cream tint in the highlights on or off. Off renders exactly like amount 0.";
 if(k=="tone_shadow_desat_on")return "Switch shadow desaturation on or off. Off renders exactly like amount 0.";
 if(k=="tone_highlight_desat")return "Reduces color in bright areas, mixing toward each pixel's own luminance, so brightness and hue stay the same.";
 if(k=="tone_highlight_start")return "Stops above 18% gray where highlight desaturation and cream begin. They fade in smoothly above this point; nothing changes below it.";
 if(k=="tone_highlight_cream")return "Warm cream tint in the highlights only (more red, less blue), with luminance kept the same. Applied after highlight desaturation, so desaturated whites turn cream. Near-neutral highlights only; saturated colors keep their hue.";
 if(k=="tone_shadow_desat")return "Reduces color in deep shadows, mixing toward each pixel's own luminance.";
 if(k=="tone_shadow_start")return "Stops below 18% gray where shadow desaturation begins. It fades in smoothly below this point. Never above Highlight color start.";
 return "";
}
OfxStatus describe(OfxImageEffectHandle e) {
 auto p=properties(e);if(interact){OfxPluginEntryPoint *callback=overlayEntry;void *address=nullptr;static_assert(sizeof(callback)==sizeof(address));std::memcpy(&address,&callback,sizeof(address));prop->propSetPointer(p,kOfxImageEffectPluginPropOverlayInteractV1,0,address);}
 prop->propSetString(p,kOfxPropLabel,0,"NegativeScan Studio OFX 0.90.14");prop->propSetString(p,kOfxImageEffectPluginPropGrouping,0,"NegativeScan Studio");prop->propSetString(p,kOfxImageEffectPropSupportedContexts,0,kOfxImageEffectContextFilter);prop->propSetString(p,kOfxImageEffectPropSupportedPixelDepths,0,kOfxBitDepthFloat);prop->propSetString(p,kOfxImageEffectPluginRenderThreadSafety,0,kOfxImageEffectRenderFullySafe);prop->propSetInt(p,kOfxImageEffectPropSupportsTiles,0,1);prop->propSetInt(p,kOfxImageEffectPropSupportsMultiResolution,0,1);prop->propSetInt(p,kOfxImageEffectPluginPropHostFrameThreading,0,0);
 prop->propSetString(p,kOfxImageEffectPropOpenCLRenderSupported,0,nss::CLApi::get().available?"true":"false");
 prop->propSetString(p,kOfxImageEffectPropOpenCLSupported,0,"false");
 prop->propSetString(p,kOfxImageEffectPropCudaRenderSupported,0,nss::CudaApi::get().available?"true":"false");
 prop->propSetString(p,kOfxImageEffectPropCudaStreamSupported,0,"true");
 prop->propSetString(p,kOfxImageEffectPropMetalRenderSupported,0,metalAvailable()?"true":"false");return kOfxStatOK;
}
OfxStatus describeContext(OfxImageEffectHandle e) {
 for(auto name:{kOfxImageEffectSimpleSourceClipName,kOfxImageEffectOutputClipName}) {
  OfxPropertySetHandle p;check(effect->clipDefine(e,name,&p));prop->propSetString(p,kOfxImageEffectPropSupportedComponents,0,kOfxImageComponentRGBA);prop->propSetInt(p,kOfxImageEffectPropSupportsTiles,0,1);
 }
 auto s=params(e);
 const char *groups[][2]={
  {"camera","Camera calibration"},
  {"balance","Film base"},
  {"positive","Film & Gamma"},
  {"color_balance","Color balance"},
  {"tone","Tone"},
  {"tone_color","Tone Color"},
  {"graypick","Viewer gray / white pick"},
  {"density","Density In"},
  {"render","Rendering"}
 };
 for(auto &g:groups){
  auto gp=define(s,g[0],g[1],kOfxParamTypeGroup);
  if(std::strcmp(g[0],"camera")==0||std::strcmp(g[0],"balance")==0||std::strcmp(g[0],"positive")==0||std::strcmp(g[0],"color_balance")==0||std::strcmp(g[0],"tone")==0||std::strcmp(g[0],"tone_color")==0||std::strcmp(g[0],"graypick")==0)prop->propSetInt(gp,kOfxParamPropGroupOpen,0,1);
  if(std::strcmp(g[0],"graypick")==0||std::strcmp(g[0],"density")==0||(std::strcmp(g[0],"render")==0&&!NSS_MAC_RENDER_UI))hideParam(gp);
 }
 // 1. Camera calibration (visible selector; underlying 3x3 matrix & controls preserved as secret params)
 auto camMode=choiceDef(s,"camera_mode","Camera calibration","X-T3|Monochrome (MV-CS240-90UM)|Off",2,"camera");
 prop->propSetString(camMode,kOfxParamPropHint,0,"X-T3 uses embedded scan metadata matrix or default X-T3 trichrome matrix. Monochrome applies the MV-CS240-90UM chart matrix (SoftLoaf No. 000004 positive target; not a universal negative dye matrix). Off bypasses camera calibration.");
 // 2. Film base (default 0.5/0.5/0.5; optional read from TIFF/JSON metadata)
 auto p=define(s,"metadata_file","TIFF or JSON path",kOfxParamTypeString,"balance");
 prop->propSetString(p,kOfxParamPropStringMode,0,kOfxParamStringIsFilePath);prop->propSetString(p,kOfxParamPropDefault,0,"");prop->propSetInt(p,kOfxParamPropAnimates,0,0);
 auto loadBtn=define(s,"load_metadata","Load film base from metadata",kOfxParamTypePushButton,"balance");
 prop->propSetString(loadBtn,kOfxParamPropHint,0,"Read film_base (and any pending camera matrix) from TIFF ImageDescription or sidecar JSON. Default film base is 0.50 / 0.50 / 0.50.");
 p=define(s,"metadata_status","Film base status",kOfxParamTypeString,"balance");
 prop->propSetString(p,kOfxParamPropDefault,0,"Default 0.50/0.50/0.50");prop->propSetInt(p,kOfxParamPropEnabled,0,0);prop->propSetInt(p,kOfxParamPropAnimates,0,0);prop->propSetInt(p,kOfxParamPropEvaluateOnChange,0,0);
 // 3. Film & Gamma dropdowns (Exposure, Contrast and Output retained visible)
 choiceDef(s,"stock","Film stock","Legacy defaults / manual|Vision3 500T 5219 Status M|Vision3 500T LED estimate|Vision3 250D 5207|UltraMax 400|Portra 400|Gold 200|Ektar 100",0,"positive");
 choiceDef(s,"film_type","Film balance","T - tungsten|D - daylight",0,"positive");
 choiceDef(s,"temperature","Scene illuminant","3200 K|3800 K|4400 K|5000 K|5600 K",0,"positive");
 // Secret technical parameters (preserved with identical IDs & defaults for saved project compatibility)
 auto armProps=boolDef(s,"pick_on_viewer","Pick gray on viewer",0,"graypick");prop->propSetInt(armProps,kOfxParamPropAnimates,0,0);prop->propSetInt(armProps,kOfxParamPropEvaluateOnChange,0,0);prop->propSetString(armProps,kOfxParamPropHint,0,"Select Open FX Overlay in the viewer, then click/drag a neutral patch. Click enables Auto gray and resets visible Base balances once.");hideParam(armProps);
 hideParam(boolDef(s,"pick_auto","Auto gray from point",0,"graypick"));
 auto point=define(s,"pick_point","Gray / white point",kOfxParamTypeDouble2D,"graypick");
 prop->propSetString(point,kOfxParamPropDoubleType,0,kOfxParamDoubleTypeXYAbsolute);
 prop->propSetString(point,kOfxParamPropDefaultCoordinateSystem,0,kOfxParamCoordinatesNormalised);
 prop->propSetInt(point,kOfxParamPropUseHostOverlayHandle,0,1);
 for(int c=0;c<2;c++)prop->propSetDouble(point,kOfxParamPropDefault,c,.5);
 hideParam(point);
 hideParam(numberDef(s,"pick_radius","Sample radius (pixels)",3,0,32,"graypick"));
 hideParam(choiceDef(s,"stage","Pipeline output","Density In|Positive Out",1));
 if(NSS_MAC_RENDER_UI){
  auto device=choiceDef(s,"render_device","Render engine","Auto (Metal)|CPU",0,"render");
  prop->propSetString(device,kOfxParamPropHint,0,"Auto renders with Metal when the host offers it, otherwise on the CPU. CPU always renders on the CPU (for Metal vs CPU checks).");
  auto info=define(s,"render_info","Show render engine",kOfxParamTypePushButton,"render");
  prop->propSetString(info,kOfxParamPropHint,0,"Shows the engine used by the last render (Metal with the GPU name, or CPU) and why.");
 }else{
  hideParam(choiceDef(s,"render_device","Render engine","Auto (CUDA/OpenCL if offered)|CPU",0,"render"));
  hideParam(define(s,"render_info","Show render engine",kOfxParamTypePushButton,"render"));
 }
 hideParam(boolDef(s,"auto_metadata","Auto-read Resolve 20 source metadata",1,"balance"));
 hideParam(boolDef(s,"camera_on","Enable camera calibration",1,"camera"));
 hideParam(boolDef(s,"neutralise","Neutralise grey - bake white balance",0,"camera"));
 hideParam(numberDef(s,"camera_strength","Camera strength",1,0,1,"camera"));
 for(int i=0;i<9;i++)for(auto prefix:{"camera_","neutral_"}) {auto k=std::string(prefix)+std::to_string(i);hideParam(numberDef(s,k.c_str(),k.c_str(),i%4==0?1:0,-16,16,"camera"));}
 for(auto key:{"reference_base_r","reference_base_g","reference_base_b"}){
  auto hidden=define(s,key,key,kOfxParamTypeDouble,"balance");prop->propSetDouble(hidden,kOfxParamPropDefault,0,.5);hideParam(hidden);prop->propSetInt(hidden,kOfxParamPropAnimates,0,0);
 }
 auto isVisibleDef=[](const char *key){
  return std::strcmp(key,"positive_out_mode")==0||std::strcmp(key,"positive_contrast")==0||std::strcmp(key,"positive_expo")==0||std::strcmp(key,"gamma_scale")==0||std::strncmp(key,"positive_bal_",13)==0||std::strncmp(key,"tone_",5)==0;
 };
 // Tone and Tone Color (0.90.14): switches first, then the group's preset menu, then the adjustment sliders.
 // Each preset menu only loads its own group and never renders by itself.
 auto definePresetMenu=[&](const char *key,const char *label,const char *menu,const char *group,const char *hint){
  auto m=choiceDef(s,key,label,menu,0,group);prop->propSetInt(m,kOfxParamPropEvaluateOnChange,0,0);prop->propSetString(m,kOfxParamPropHint,0,hint);};
 bool tonePresetDone=false,toneColorPresetDone=false;
 for(auto &d:defs) {
  const char *grp=std::strncmp(d.key,"positive_bal_",13)==0?"color_balance":d.group;
  if(d.kind!=Kind::Boolean&&std::strcmp(d.group,"tone")==0&&!tonePresetDone){tonePresetDone=true;
   definePresetMenu("tone_preset","Tone preset",nss::kTonePresetMenu,"tone","Load highlight shoulder and shadow toe settings (this group only). Soft cinematic is the default. Off / Classic 0.90.11 switches both off. Editing a switch or slider in this group shows Custom.");}
  if(d.kind!=Kind::Boolean&&std::strcmp(d.group,"tone_color")==0&&!toneColorPresetDone){toneColorPresetDone=true;
   definePresetMenu("tone_color_preset","Tone color preset",nss::kToneColorPresetMenu,"tone_color","Load highlight desaturation, highlight cream and shadow desaturation settings (this group only). Soft cinematic is the default. Off switches all three off. Editing a switch or slider in this group shows Custom.");}
  OfxPropertySetHandle dp=nullptr;
  if(d.kind==Kind::Number)dp=numberDef(s,d.key,d.label,d.def,d.lo,d.hi,grp);
  else if(d.kind==Kind::Choice)dp=choiceDef(s,d.key,d.label,d.options,int(d.def),grp);
  else dp=boolDef(s,d.key,d.label,int(d.def),grp);
  if(!isVisibleDef(d.key))hideParam(dp);
  if(std::strncmp(d.key,"tone_",5)==0){
   if(d.kind==Kind::Number){prop->propSetDouble(dp,kOfxParamPropIncrement,0,d.step);prop->propSetInt(dp,kOfxParamPropDigits,0,2);}
   prop->propSetString(dp,kOfxParamPropHint,0,toneHint(d.key));
  }
 }
 auto balPreset=choiceDef(s,"balance_preset","Balance preset","None|Slot 1|Slot 2|Slot 3|Slot 4|Slot 5|Slot 6|Slot 7|Slot 8",0,"color_balance");
 prop->propSetInt(balPreset,kOfxParamPropEvaluateOnChange,0,0);prop->propSetInt(balPreset,kOfxParamPropPersistant,0,0);
 prop->propSetString(balPreset,kOfxParamPropHint,0,"Apply a saved Red/Green/Blue balance into the sliders. If the host does not refresh this menu, names still appear in Preset status.");
 auto balName=define(s,"balance_preset_name","Preset name",kOfxParamTypeString,"color_balance");
 prop->propSetString(balName,kOfxParamPropDefault,0,"");prop->propSetInt(balName,kOfxParamPropAnimates,0,0);prop->propSetInt(balName,kOfxParamPropEvaluateOnChange,0,0);prop->propSetInt(balName,kOfxParamPropPersistant,0,0);
 prop->propSetString(balName,kOfxParamPropHint,0,"English (ASCII) name used by Save. Saving the same name overwrites that preset.");
 auto saveBal=define(s,"save_balance_preset","Save",kOfxParamTypePushButton,"color_balance");
 prop->propSetInt(saveBal,kOfxParamPropEvaluateOnChange,0,0);
 prop->propSetString(saveBal,kOfxParamPropHint,0,"Save the current Red, Green and Blue balance values under Preset name. Same name overwrites. Shared across all projects.");
 auto delBal=define(s,"delete_balance_preset","Delete",kOfxParamTypePushButton,"color_balance");
 prop->propSetInt(delBal,kOfxParamPropEvaluateOnChange,0,0);
 prop->propSetString(delBal,kOfxParamPropHint,0,"Delete the preset currently selected in Balance preset.");
 auto balStatus=define(s,"balance_preset_status","Preset status",kOfxParamTypeString,"color_balance");
 prop->propSetString(balStatus,kOfxParamPropDefault,0,"No saved presets");prop->propSetInt(balStatus,kOfxParamPropEnabled,0,0);prop->propSetInt(balStatus,kOfxParamPropAnimates,0,0);prop->propSetInt(balStatus,kOfxParamPropEvaluateOnChange,0,0);prop->propSetInt(balStatus,kOfxParamPropPersistant,0,0);
 auto page=define(s,"controls","Controls",kOfxParamTypePage);
 std::vector<std::string> children={
  "camera_mode",
  "metadata_file","load_metadata","metadata_status",
  "stock","film_type","temperature","positive_expo","gamma_scale","positive_contrast","positive_out_mode",
  "positive_bal_r","positive_bal_g","positive_bal_b",
  "tone_shoulder_on","tone_toe_on","tone_preset","tone_shoulder_strength","tone_shoulder_pivot","tone_shoulder_white","tone_toe_strength","tone_toe_pivot","tone_toe_black",
  "tone_highlight_desat_on","tone_highlight_cream_on","tone_shadow_desat_on","tone_color_preset","tone_highlight_desat","tone_highlight_start","tone_highlight_cream","tone_shadow_desat","tone_shadow_start",
  "balance_preset","balance_preset_name","save_balance_preset","delete_balance_preset","balance_preset_status"
 };
 for(auto key:{"pick_on_viewer","pick_auto","pick_point","pick_radius","stage","render_device","render_info","auto_metadata","camera_on","neutralise","camera_strength"})children.push_back(key);
 for(int i=0;i<9;i++)for(auto prefix:{"camera_","neutral_"})children.push_back(std::string(prefix)+std::to_string(i));
 for(auto &d:defs)if(!isVisibleDef(d.key))children.push_back(d.key);
 for(size_t i=0;i<children.size();i++)prop->propSetString(page,kOfxParamPropPageChild,int(i),children[i].c_str());
 return kOfxStatOK;
}
// Tone field helpers driven by the generated field table (section 0 = Tone, 1 = Tone Color, -1 = both).
void applyTone(OfxParamSetHandle s,const nss::Tone &t,int section=-1){
 for(int i=0;i<nss::kToneFieldCount;i++){auto &f=nss::toneFields[i];if(section>=0&&f.section!=section)continue;
  if(f.toggle)setI(s,f.key,int(nss::toneGet(t,i)));else setD(s,f.key,nss::toneGet(t,i));}
}
nss::Tone readTone(OfxParamSetHandle s,double time){
 nss::Tone t;for(int i=0;i<nss::kToneFieldCount;i++){auto &f=nss::toneFields[i];nss::toneSet(t,i,f.toggle?double(integer(s,f.key,time)):number(s,f.key,time));}return t;
}
bool sameTone(const nss::Tone &a,const nss::Tone &b,int section=-1){
 for(int i=0;i<nss::kToneFieldCount;i++){if(section>=0&&nss::toneFields[i].section!=section)continue;if(std::abs(nss::toneGet(a,i)-nss::toneGet(b,i))>=1e-4)return false;}return true;
}
int toneSection(const std::string &key){for(auto &f:nss::toneFields)if(key==f.key)return f.section;return -1;}
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
 updateBase(s,t);
 int camMode=integer(s,"camera_mode",t);
 if(m.cameraApplied){
  setI(s,"camera_on",0);setI(s,"neutralise",0);setI(s,"camera_mode",2);
 }else if(m.camera){
  setI(s,"camera_on",1);setI(s,"neutralise",0);setD(s,"camera_strength",p.cameraStrength);
  for(int a=0;a<9;a++){auto k="camera_"+std::to_string(a);setD(s,k.c_str(),p.camera[a]);}
  setI(s,"camera_mode",0);
 }else if(camMode==1){
  setI(s,"camera_on",1);setI(s,"neutralise",0);setD(s,"camera_strength",1.0);
  for(int a=0;a<9;a++){auto k="camera_"+std::to_string(a);setD(s,k.c_str(),kMonoMatrix[a]);}
 }else{
  setI(s,"camera_on",0);setI(s,"neutralise",0);setI(s,"camera_mode",2);
 }
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
OfxPropertySetHandle paramProps(OfxParamSetHandle s,const char *k){OfxParamHandle h;OfxPropertySetHandle p=nullptr;check(param->paramGetHandle(s,k,&h,&p));return p;}
void refreshBalancePresetUi(OfxParamSetHandle s,const nss::BalancePresetsFile &file,const std::string &status){
 auto p=paramProps(s,"balance_preset");
 prop->propSetString(p,kOfxParamPropChoiceOption,0,"None");
 for(int i=0;i<nss::kBalancePresetSlots;i++)prop->propSetString(p,kOfxParamPropChoiceOption,i+1,nss::slotLabel(i+1,file.slots[i]).c_str());
 setS(s,"balance_preset_status",status);
}
void showBalancePresets(OfxParamSetHandle s,const nss::BalancePresetsFile &file,const std::string &status=""){
 refreshBalancePresetUi(s,file,status.empty()?nss::presetsListStatus(file):status);
}
void applyBalancePreset(OfxParamSetHandle s,double time){
 auto file=nss::loadBalancePresets();
 int selected=integer(s,"balance_preset",time);
 if(selected<=0){showBalancePresets(s,file);return;}
 if(selected>nss::kBalancePresetSlots)throw kOfxStatErrValue;
 auto &preset=file.slots[selected-1];
 if(preset.empty()){showBalancePresets(s,file,"Slot "+std::to_string(selected)+" is empty");return;}
 Edit edit(s,"Load color balance preset");
 setD(s,"positive_bal_r",preset.r);setD(s,"positive_bal_g",preset.g);setD(s,"positive_bal_b",preset.b);
 setS(s,"balance_preset_name",preset.name);
 showBalancePresets(s,file,std::string("Applied '")+preset.name+"'");
}
void saveBalancePreset(OfxParamSetHandle s,double time){
 auto name=nss::trimAscii(stringParam(s,"balance_preset_name"));
 std::string why;
 if(!nss::validPresetName(name,&why)){auto file=nss::loadBalancePresets();showBalancePresets(s,file,why);return;}
 auto file=nss::loadBalancePresets();
 int existing=nss::findPresetName(file,name);
 int selected=integer(s,"balance_preset",time)-1;
 int slot=existing;
 if(slot<0){
  if(selected>=0&&selected<nss::kBalancePresetSlots&&file.slots[selected].empty())slot=selected;
  else slot=nss::firstEmptySlot(file);
 }
 if(slot<0){showBalancePresets(s,file,"All 8 slots are full");return;}
 file.slots[slot]={name,number(s,"positive_bal_r",time),number(s,"positive_bal_g",time),number(s,"positive_bal_b",time)};
 file.valid=true;
 if(!nss::saveBalancePresets(file)){showBalancePresets(s,file,"Could not save presets file");return;}
 setI(s,"balance_preset",slot+1);
 showBalancePresets(s,file,std::string("Saved '")+name+"' in slot "+std::to_string(slot+1));
}
void deleteBalancePreset(OfxParamSetHandle s,double time){
 auto file=nss::loadBalancePresets();
 int selected=integer(s,"balance_preset",time)-1;
 if(selected<0||selected>=nss::kBalancePresetSlots||file.slots[selected].empty()){showBalancePresets(s,file,"Select a saved preset to delete");return;}
 auto name=file.slots[selected].name;
 file.slots[selected]={};
 file.valid=true;
 if(!nss::saveBalancePresets(file)){showBalancePresets(s,file,"Could not save presets file");return;}
 setI(s,"balance_preset",0);
 showBalancePresets(s,file,std::string("Deleted '")+name+"'");
}
OfxStatus changed(OfxImageEffectHandle e,OfxPropertySetHandle a) {
 if(str(a,kOfxPropChangeReason)!=kOfxChangeUserEdited)return kOfxStatReplyDefault;
 auto s=params(e);auto key=str(a,kOfxPropName);double time=0;prop->propGetDouble(a,kOfxPropTime,0,&time);
 if(key=="load_metadata") {
  try{auto i=instance(e);auto path=stringParam(s,"metadata_file");if(path.empty())path=sourcePath(e,i,a);auto m=load(i,path,true);Edit edit(s,"Load scan metadata");fillMetadata(s,m,time);}
  catch(const std::exception &x){setS(s,"metadata_status",shortError(x));return kOfxStatOK;}
  catch(...){setS(s,"metadata_status","Cannot read file path");return kOfxStatOK;}
 }else if(key=="tone_preset"){
  int selected=integer(s,"tone_preset",time);
  if(selected>=0&&selected<nss::kTonePresetCount){Edit edit(s,"Load tone preset");applyTone(s,nss::tonePresets[selected].tone,0);}
 }else if(key=="tone_color_preset"){
  int selected=integer(s,"tone_color_preset",time);
  if(selected>=0&&selected<nss::kToneColorPresetCount){Edit edit(s,"Load tone color preset");applyTone(s,nss::toneColorPresets[selected].tone,1);}
 }else if(toneSection(key)>=0){
  // A manual edit no longer matches the group's named preset; the other group's menu is untouched.
  bool color=toneSection(key)==1;const char *menu=color?"tone_color_preset":"tone_preset";
  int count=color?nss::kToneColorPresetCount:nss::kTonePresetCount;const nss::TonePreset *rows=color?nss::toneColorPresets:nss::tonePresets;
  int selected=integer(s,menu,time);
  if(selected<0||selected>=count||!sameTone(rows[selected].tone,readTone(s,time),color?1:0))setI(s,menu,count);
 }else if(key=="balance_preset"){
  applyBalancePreset(s,time);
 }else if(key=="save_balance_preset"){
  saveBalancePreset(s,time);
 }else if(key=="delete_balance_preset"){
  deleteBalancePreset(s,time);
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
 }else if(key=="camera_mode"){
  int mode=integer(s,"camera_mode",time);
  if(mode<0||mode>2)throw kOfxStatErrValue;
  Edit edit(s,"Camera calibration");
  if(integer(s,"auto_metadata",time)){syncReference(e,s,time);updateBase(s,time);}
  setI(s,"auto_metadata",0);
  setI(s,"neutralise",0);
  if(mode==2){
   setI(s,"camera_on",0);
  }else{
   std::array<double,9> m=(mode==1)?kMonoMatrix:kXT3Matrix;
   double strength=1.0;
   if(mode==0){
    auto i=instance(e);
    auto path=stringParam(s,"metadata_file");if(path.empty())path=sourcePath(e,i,a);
    auto norm=nss::normalizePath(path);
    std::lock_guard<std::mutex> lock(i->mutex);
    if(!norm.empty()&&i->path==norm&&i->meta&&i->meta->camera&&!i->meta->cameraApplied){
     for(int c=0;c<9;c++)m[c]=(*i->meta->camera)[c];
     strength=i->meta->strength;
    }
   }
   setI(s,"camera_on",1);
   setD(s,"camera_strength",strength);
   for(int c=0;c<9;c++){auto k="camera_"+std::to_string(c);setD(s,k.c_str(),m[c]);}
  }
 }else if(key=="density_base_r"||key=="density_base_g"||key=="density_base_b"){
  setI(s,"auto_metadata",0);
 }else if(key=="render_device"){
  bool automatic=integer(s,"render_device",time)==0;
  prop->propSetString(properties(e),kOfxImageEffectPropOpenCLRenderSupported,0,automatic&&nss::CLApi::get().available?"true":"false");
  prop->propSetString(properties(e),kOfxImageEffectPropCudaRenderSupported,0,automatic&&nss::CudaApi::get().available?"true":"false");
  prop->propSetString(properties(e),kOfxImageEffectPropMetalRenderSupported,0,automatic&&metalAvailable()&&!instance(e)->metalWithdrawn?"true":"false");
 }else if(key=="render_info"){
  auto i=instance(e);std::string text;{std::lock_guard<std::mutex> lock(i->mutex);text=i->lastRender;}
  // macOS: the last render plus the Metal state. Windows/Linux: the 0.90.14 text (CUDA, OpenCL).
  if(NSS_MAC_RENDER_UI)text+="\n"+nss::MetalApi::get().availability+(i->metalWithdrawn?" (withdrawn for this node after a failure)":"");
  else text+="\n"+nss::CudaApi::get().availability+"\nOpenCL runtime: "+(nss::CLApi::get().available?"available":"unavailable");
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
 int gpu=0,cuda=0,metal=0;prop->propGetInt(a,kOfxImageEffectPropOpenCLEnabled,0,&gpu);prop->propGetInt(a,kOfxImageEffectPropCudaEnabled,0,&cuda);prop->propGetInt(a,kOfxImageEffectPropMetalEnabled,0,&metal);
 if((gpu!=0)+(cuda!=0)+(metal!=0)>1)return kOfxStatGPURenderFailed;
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
 void *metalQueue=nullptr;if(metal)prop->propGetPointer(a,kOfxImageEffectPropMetalCommandQueue,0,&metalQueue);
 // Metal on unified memory: after the host queue has drained, the host MTLBuffers are readable and
 // writable from the CPU (zero copy). Only the picker and the CPU fallback use this; the normal
 // Metal render never waits.
 float *metalGpuSource=src.data,*metalGpuDest=dst.data;bool metalMapped=false,metalMapTried=false;
 auto mapMetal=[&]{
  if(metalMapTried)return metalMapped;
  metalMapTried=true;auto &api=nss::MetalApi::get();
  if(!api.contents||!api.drain)return false;
  void *sp=api.contents(metalGpuSource),*dp=api.contents(metalGpuDest);
  if(!sp||!dp||!api.drain(metalQueue))return false;
  src.data=static_cast<float*>(sp);dst.data=static_cast<float*>(dp);return metalMapped=true;
 };
 bool pickUnavailable=false;
 if(pick){
  double x=0,y=0,scaleX=1,scaleY=1,aspect=1;
  check(param->paramGetValueAtTime(handle(s,"pick_point"),time,&x,&y));
  prop->propGetDouble(a,kOfxImageEffectPropRenderScale,0,&scaleX);prop->propGetDouble(a,kOfxImageEffectPropRenderScale,1,&scaleY);
  prop->propGetDouble(src.p,kOfxImagePropPixelAspectRatio,0,&aspect);if(!(aspect>0))aspect=1;
  int rect[4],radius=int(std::clamp(number(s,"pick_radius",time),0.,32.));
  double px=std::floor(x*scaleX/aspect),py=std::floor(y*scaleY);
  if(!std::isfinite(px)||!std::isfinite(py)||px<INT32_MIN+64.||px>INT32_MAX-64.||py<INT32_MIN+64.||py>INT32_MAX-64.||!nss::pickRect(src.bounds,int(px),int(py),radius,rect))pickUnavailable=true;
  else if(metal&&!mapMetal())pickUnavailable=true;
  else if(!metal&&(gpu||cuda)){
   try{auto patch=cuda?i->cuda.readPatch(cudaStream,src.data,src.bounds,src.row,rect):i->opencl.readPatch(queue,src.data,src.bounds,src.row,rect);nss::FrameView view{patch.data(),{rect[0],rect[1],rect[2],rect[3]},(rect[2]-rect[0])*4*int(sizeof(float))};pickUnavailable=!nss::grayFromPatch(p,view,rect,premult);}
   catch(const std::exception &x){setLastRender(i,x.what());return kOfxStatGPURenderFailed;}
  }else{nss::FrameView view{src.data,{src.bounds[0],src.bounds[1],src.bounds[2],src.bounds[3]},src.row};pickUnavailable=!nss::grayFromPatch(p,view,rect,premult);}
 }
 auto prepared=i->preparedCache.get(p);
 std::string metalNote;
 if(metal){
  auto &api=nss::MetalApi::get();std::string why;bool cpuRequested=integer(s,"render_device",time)!=0;
  if(cpuRequested)why="CPU requested";
  else if(!api.available||!api.encode)why=api.availability;
  else if(i->metalWithdrawn)why="Metal withdrawn after an earlier failure";
  else if(i->metal->failures.load())why="Metal command buffer failed: "+i->metal->error();
  else{
   nss::MetalArgs args{src.bounds[0],src.bounds[1],src.bounds[2],src.bounds[3],src.row/int(sizeof(float)),dst.bounds[0],dst.bounds[1],dst.bounds[2],dst.bounds[3],dst.row/int(sizeof(float)),win[0],win[1],win[2]-win[0],win[3]-win[1],int(premult)};
   std::string device,error;bool ok=false;
   if(src.row<=0||dst.row<=0||src.row%int(sizeof(float))||dst.row%int(sizeof(float)))error="Unsupported Metal buffer layout";
   else try{ok=api.encode(metalQueue,metalGpuSource,metalGpuDest,prepared.values.data(),prepared.values.size(),args,i->metal,device,error);}
   catch(const std::exception &x){error=x.what();}catch(...){error="unknown Metal error";}
   if(ok){setLastRender(i,"Metal ("+device+"), host queue buffers"+(pickUnavailable?"; pick unavailable":""));return kOfxStatOK;}
   why="Metal failed: "+error;
  }
  // Any Metal failure: withdraw Metal for this instance and render this frame on the CPU,
  // in place on the shared buffers. GPU-private buffers cannot be reached from the CPU.
  if(!cpuRequested)withdrawMetal(e,i);
  if(!mapMetal()){setLastRender(i,why+"; CPU fallback needs shared (unified memory) buffers");return kOfxStatGPURenderFailed;}
  metalNote=why+"; CPU fallback on shared Metal buffers, ";
 }
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
 setLastRender(i,metalNote+"CPU "+std::to_string(count)+" threads"+(pickUnavailable?"; pick unavailable":""));return kOfxStatOK;
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
   auto i=std::make_unique<Instance>();check(effect->clipGetHandle(e,kOfxImageEffectSimpleSourceClipName,&i->src,nullptr));check(effect->clipGetHandle(e,kOfxImageEffectOutputClipName,&i->dst,nullptr));check(prop->propSetPointer(properties(e),kOfxPropInstanceData,0,i.get()));i.release();
   try{auto s=params(e);showBalancePresets(s,nss::loadBalancePresets());}catch(...){}
   return kOfxStatOK;
  }
  if(!std::strcmp(action,kOfxActionDestroyInstance)){delete instance(e);prop->propSetPointer(properties(e),kOfxPropInstanceData,0,nullptr);return kOfxStatOK;}
  if(!std::strcmp(action,kOfxImageEffectActionGetRegionsOfInterest))return regionsOfInterest(e,in,out);
  if(!std::strcmp(action,kOfxImageEffectActionRender))return render(e,in);
  if(!std::strcmp(action,kOfxActionInstanceChanged))return changed(e,in);
  if(!std::strcmp(action,kOfxActionUnload))return kOfxStatOK;
  return kOfxStatReplyDefault;
 }catch(OfxStatus s){return s;}catch(const std::bad_alloc &){return kOfxStatErrMemory;}catch(...){return kOfxStatFailed;}
}
OfxPlugin plugin={kOfxImageEffectPluginApi,1,"org.negativescanstudio.complete",0,9014,setHost,entry};
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
