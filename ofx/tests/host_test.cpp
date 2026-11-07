// Minimal suite host: exercises real plugin entry points, params, render and metadata.
#include "../src/plugin.cpp"
#include <cstdarg>
#include <cassert>
#include <iostream>
static void require(bool value){if(!value){std::cerr<<"Host test assertion failed\n";std::abort();}}
namespace mock {
struct Bag { std::map<std::string,std::vector<int>> ints;std::map<std::string,std::vector<double>> doubles;std::map<std::string,std::vector<std::string>> strings;std::map<std::string,std::vector<void*>> pointers;std::string type; };
struct Set {std::map<std::string,Bag> values;};
struct Clip {Bag props,image;};
struct Effect {Bag props;Set params;Clip source,output;};
template<class T,class H>T *ptr(H h){return reinterpret_cast<T*>(h);}
template<class T>OfxStatus put(std::map<std::string,std::vector<T>>&m,const char *k,int i,T v){if(i<0)return kOfxStatErrBadIndex;auto &a=m[k];if(a.size()<=size_t(i))a.resize(i+1);a[i]=v;return kOfxStatOK;}
template<class T>OfxStatus get(std::map<std::string,std::vector<T>>&m,const char *k,int i,T *v){auto a=m.find(k);if(a==m.end()||i<0||a->second.size()<=size_t(i))return kOfxStatErrUnknown;*v=a->second[i];return kOfxStatOK;}
OfxStatus si(OfxPropertySetHandle p,const char*k,int i,int v){return put(ptr<Bag>(p)->ints,k,i,v);}
OfxStatus sd(OfxPropertySetHandle p,const char*k,int i,double v){return put(ptr<Bag>(p)->doubles,k,i,v);}
OfxStatus ss(OfxPropertySetHandle p,const char*k,int i,const char*v){return put(ptr<Bag>(p)->strings,k,i,std::string(v));}
OfxStatus sp(OfxPropertySetHandle p,const char*k,int i,void*v){return put(ptr<Bag>(p)->pointers,k,i,v);}
OfxStatus gi(OfxPropertySetHandle p,const char*k,int i,int*v){return get(ptr<Bag>(p)->ints,k,i,v);}
OfxStatus gd(OfxPropertySetHandle p,const char*k,int i,double*v){return get(ptr<Bag>(p)->doubles,k,i,v);}
OfxStatus gs(OfxPropertySetHandle p,const char*k,int i,char**v){auto &m=ptr<Bag>(p)->strings;auto a=m.find(k);if(a==m.end()||a->second.size()<=size_t(i))return kOfxStatErrUnknown;*v=a->second[i].data();return kOfxStatOK;}
OfxStatus gp(OfxPropertySetHandle p,const char*k,int i,void**v){return get(ptr<Bag>(p)->pointers,k,i,v);}
OfxStatus gin(OfxPropertySetHandle p,const char*k,int n,int*v){for(int i=0;i<n;i++){auto r=gi(p,k,i,v+i);if(r)return r;}return kOfxStatOK;}
OfxStatus ep(OfxImageEffectHandle e,OfxPropertySetHandle *p){*p=reinterpret_cast<OfxPropertySetHandle>(&ptr<Effect>(e)->props);return kOfxStatOK;}
OfxStatus es(OfxImageEffectHandle e,OfxParamSetHandle *p){*p=reinterpret_cast<OfxParamSetHandle>(&ptr<Effect>(e)->params);return kOfxStatOK;}
OfxStatus cd(OfxImageEffectHandle e,const char*k,OfxPropertySetHandle *p){auto f=ptr<Effect>(e);*p=reinterpret_cast<OfxPropertySetHandle>(std::string(k)=="Source"?&f->source.props:&f->output.props);return kOfxStatOK;}
OfxStatus cg(OfxImageEffectHandle e,const char*k,OfxImageClipHandle *c,OfxPropertySetHandle *p){auto f=ptr<Effect>(e);auto cl=std::string(k)=="Source"?&f->source:&f->output;*c=reinterpret_cast<OfxImageClipHandle>(cl);if(p)*p=reinterpret_cast<OfxPropertySetHandle>(&cl->props);return kOfxStatOK;}
OfxStatus cp(OfxImageClipHandle c,OfxPropertySetHandle *p){*p=reinterpret_cast<OfxPropertySetHandle>(&ptr<Clip>(c)->props);return kOfxStatOK;}
OfxStatus image(OfxImageClipHandle c,OfxTime,const OfxRectD*,OfxPropertySetHandle *p){*p=reinterpret_cast<OfxPropertySetHandle>(&ptr<Clip>(c)->image);return kOfxStatOK;}
OfxStatus release(OfxPropertySetHandle){return kOfxStatOK;}
int abort_(OfxImageEffectHandle){return 0;}
OfxStatus pd(OfxParamSetHandle set,const char*t,const char*k,OfxPropertySetHandle *p){auto &b=ptr<Set>(set)->values[k];b.type=t;*p=reinterpret_cast<OfxPropertySetHandle>(&b);return kOfxStatOK;}
OfxStatus ph(OfxParamSetHandle set,const char*k,OfxParamHandle *h,OfxPropertySetHandle *p){auto &m=ptr<Set>(set)->values;auto a=m.find(k);if(a==m.end())return kOfxStatErrUnknown;*h=reinterpret_cast<OfxParamHandle>(&a->second);if(p)*p=reinterpret_cast<OfxPropertySetHandle>(&a->second);return kOfxStatOK;}
OfxStatus value(OfxParamHandle h,va_list ap){auto b=ptr<Bag>(h);if(b->type==kOfxParamTypeDouble)return get(b->doubles,kOfxParamPropDefault,0,va_arg(ap,double*));if(b->type==kOfxParamTypeString){auto p=va_arg(ap,char**);*p=b->strings[kOfxParamPropDefault][0].data();return kOfxStatOK;}return get(b->ints,kOfxParamPropDefault,0,va_arg(ap,int*));}
OfxStatus pv(OfxParamHandle h,...){va_list ap;va_start(ap,h);auto r=value(h,ap);va_end(ap);return r;}
OfxStatus pvt(OfxParamHandle h,OfxTime t,...){va_list ap;va_start(ap,t);auto r=value(h,ap);va_end(ap);return r;}
OfxStatus ps(OfxParamHandle h,...){va_list ap;va_start(ap,h);auto b=ptr<Bag>(h);if(b->type==kOfxParamTypeDouble)put(b->doubles,kOfxParamPropDefault,0,va_arg(ap,double));else if(b->type==kOfxParamTypeString)put(b->strings,kOfxParamPropDefault,0,std::string(va_arg(ap,const char*)));else put(b->ints,kOfxParamPropDefault,0,va_arg(ap,int));va_end(ap);return kOfxStatOK;}
OfxStatus edit(OfxParamSetHandle,const char*){return kOfxStatOK;}
OfxStatus end(OfxParamSetHandle){return kOfxStatOK;}
OfxPropertySuiteV1 props{};OfxImageEffectSuiteV1 effects{};OfxParameterSuiteV1 params_{};
const void *fetch(OfxPropertySetHandle,const char *name,int version){if(version!=1)return nullptr;if(std::string(name)==kOfxPropertySuite)return &props;if(std::string(name)==kOfxImageEffectSuite)return &effects;if(std::string(name)==kOfxParameterSuite)return &params_;return nullptr;}
void init(){props.propSetInt=si;props.propSetDouble=sd;props.propSetString=ss;props.propSetPointer=sp;props.propGetInt=gi;props.propGetDouble=gd;props.propGetString=gs;props.propGetPointer=gp;props.propGetIntN=gin;effects.getPropertySet=ep;effects.getParamSet=es;effects.clipDefine=cd;effects.clipGetHandle=cg;effects.clipGetPropertySet=cp;effects.clipGetImage=image;effects.clipReleaseImage=release;effects.abort=abort_;params_.paramDefine=pd;params_.paramGetHandle=ph;params_.paramGetValue=pv;params_.paramGetValueAtTime=pvt;params_.paramSetValue=ps;params_.paramEditBegin=edit;params_.paramEditEnd=end;}
}
int main(int argc,char **argv){require(argc==2);mock::init();OfxHost h{nullptr,mock::fetch};OfxGetPlugin(0)->setHost(&h);require(OfxGetNumberOfPlugins()==1&&OfxGetPlugin(1)==nullptr);auto call=OfxGetPlugin(0)->mainEntry;mock::Effect fx;auto e=reinterpret_cast<OfxImageEffectHandle>(&fx);auto bag=[](mock::Bag &b){return reinterpret_cast<OfxPropertySetHandle>(&b);};
require(call(kOfxActionLoad,nullptr,nullptr,nullptr)==kOfxStatOK);require(call(kOfxActionDescribe,e,nullptr,nullptr)==kOfxStatOK);require(call(kOfxImageEffectActionDescribeInContext,e,nullptr,nullptr)==kOfxStatOK);require(fx.params.values.size()>=80);require(call(kOfxActionCreateInstance,e,nullptr,nullptr)==kOfxStatOK);
auto s=reinterpret_cast<OfxParamSetHandle>(&fx.params);setI(s,"auto_metadata",0);require(number(s,"positive_gam_r",0)==.55);require(integer(s,"positive_film_wb",0)==1);
float in[16]={.03,.04,.05,1,.1,.2,.3,1,.4,.3,.2,1,.5,.5,.5,1},out[16]{};
for(auto *b:{&fx.source.image,&fx.output.image}){auto p=bag(*b);mock::ss(p,kOfxImageEffectPropPixelDepth,0,kOfxBitDepthFloat);mock::ss(p,kOfxImageEffectPropComponents,0,kOfxImageComponentRGBA);mock::ss(p,kOfxImageEffectPropPreMultiplication,0,kOfxImageUnPreMultiplied);mock::si(p,kOfxImagePropRowBytes,0,-32);for(int j=0;j<4;j++)mock::si(p,kOfxImagePropBounds,j,(int[]){10,20,12,22}[j]);}
mock::sp(bag(fx.source.image),kOfxImagePropData,0,in+8);mock::sp(bag(fx.output.image),kOfxImagePropData,0,out+8);
mock::Bag args;mock::sd(bag(args),kOfxPropTime,0,0);for(int j=0;j<4;j++)mock::si(bag(args),kOfxImageEffectPropRenderWindow,j,(int[]){10,20,12,22}[j]);
require(call(kOfxImageEffectActionRender,e,bag(args),nullptr)==kOfxStatOK);auto p=readParams(s,0);for(int j=0;j<4;j++){auto v=p.pixel({in[j*4],in[j*4+1],in[j*4+2]});require(std::abs(out[j*4]-v.x)<1e-6f);require(std::abs(out[j*4+1]-v.y)<1e-6f);require(std::abs(out[j*4+2]-v.z)<1e-6f);require(out[j*4+3]==1);}
// Premultiplication must be undone before nonlinear negative conversion.
mock::ss(bag(fx.source.image),kOfxImageEffectPropPreMultiplication,0,kOfxImagePreMultiplied);
for(int j=0;j<4;j++){for(int c=0;c<3;c++)in[4*j+c]*=.5f;in[4*j+3]=.5f;}
require(call(kOfxImageEffectActionRender,e,bag(args),nullptr)==kOfxStatOK);
for(int j=0;j<4;j++){auto v=p.pixel({in[4*j]*2,in[4*j+1]*2,in[4*j+2]*2});require(std::abs(out[4*j]-v.x*.5f)<1e-6f);require(out[4*j+3]==.5f);}
mock::ss(bag(fx.source.image),kOfxImageEffectPropPreMultiplication,0,kOfxImageUnPreMultiplied);
for(int j=0;j<4;j++){for(int c=0;c<3;c++)in[4*j+c]*=2;in[4*j+3]=1;}
// Resolve-specific source property automatically reads TIFF, no user path required.
setI(s,"auto_metadata",1);mock::ss(bag(fx.props),"OfxImageEffectPropSrcFilePath",0,argv[1]);require(call(kOfxImageEffectActionRender,e,bag(args),nullptr)==kOfxStatOK);auto m=nss::parseMetadata(nss::readDescription(argv[1]));nss::applyMetadata(p,m);auto expected=p.pixel({in[0],in[1],in[2]});require(std::abs(out[0]-expected.x)<1e-6f);
// Button snapshots the values and preserves manual override thereafter.
mock::Bag changed;mock::ss(bag(changed),kOfxPropName,0,"load_metadata");mock::ss(bag(changed),kOfxPropChangeReason,0,kOfxChangeUserEdited);require(call(kOfxActionInstanceChanged,e,bag(changed),nullptr)==kOfxStatOK);require(integer(s,"auto_metadata",0)==0);require(std::abs(number(s,"density_base_r",0)-.479)<1e-6);setD(s,"density_base_r",.7);require(number(s,"density_base_r",0)==.7);
setI(s,"stock",6);mock::ss(bag(changed),kOfxPropName,0,"stock");require(call(kOfxActionInstanceChanged,e,bag(changed),nullptr)==kOfxStatOK);require(integer(s,"film_type",0)==1);require(std::abs(number(s,"positive_gam_r",0)-.550724)<1e-5);require(integer(s,"positive_film_wb",0)==0);
setI(s,"film_type",0);mock::ss(bag(changed),kOfxPropName,0,"film_type");require(call(kOfxActionInstanceChanged,e,bag(changed),nullptr)==kOfxStatOK);require(integer(s,"film_type",0)==0);
require(call(kOfxActionDestroyInstance,e,nullptr,nullptr)==kOfxStatOK);std::cout<<"PASS: exported entrypoints, describe/create, RGBA render, negative stride, nonzero origin, Resolve source auto-read, load snapshot/manual override, stock gamma and overridable T/D\n";
}
