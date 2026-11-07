#define main nss_existing_host_main
#include "host_test.cpp"
#undef main
namespace draw {
std::vector<std::pair<double,double>> vertices;int pushes=0,pops=0,begins=0,ends=0;
void push(unsigned){++pushes;}void pop(){++pops;}void disable(unsigned){}void width(float){}void color(float,float,float){}
void begin(unsigned mode){require(mode==1);++begins;}void vertex(double x,double y){vertices.push_back({x,y});}void end(){++ends;}
void *context(){return reinterpret_cast<void*>(1);}
}
static OfxStatus rejectSetter(OfxParamHandle,...){return kOfxStatErrValue;}
static OfxStatus rod(OfxImageClipHandle,double,OfxRectD *r){*r={100,200,1100,2200};return kOfxStatOK;}
int main(){
 mock::init();mock::effects.clipGetRegionOfDefinition=rod;
 OfxHost host_{nullptr,mock::fetch};OfxGetPlugin(0)->setHost(&host_);auto run=OfxGetPlugin(0)->mainEntry;
 mock::Effect fx;auto e=reinterpret_cast<OfxImageEffectHandle>(&fx);auto bag=[](mock::Bag &b){return reinterpret_cast<OfxPropertySetHandle>(&b);};
 check(run(kOfxActionLoad,nullptr,nullptr,nullptr));check(run(kOfxActionDescribe,e,nullptr,nullptr));check(run(kOfxImageEffectActionDescribeInContext,e,nullptr,nullptr));check(run(kOfxActionCreateInstance,e,nullptr,nullptr));auto s=params(e);
 void *address=nullptr;check(prop->propGetPointer(properties(e),kOfxImageEffectPluginPropOverlayInteractV1,0,&address));OfxPluginEntryPoint *callback=nullptr;std::memcpy(&callback,&address,sizeof(callback));require(callback!=nullptr);
 require(fx.params.values["graypick"].ints[kOfxParamPropGroupOpen][0]==1);
 require(integer(s,"pick_on_viewer",0)==0&&integer(s,"pick_auto",0)==0);
 require(fx.params.values["pick_point"].strings[kOfxParamPropDoubleType][0]==kOfxParamDoubleTypeXYAbsolute);
 mock::Bag desc,widget,args;check(callback(kOfxActionDescribe,&desc,nullptr,nullptr));mock::sp(bag(widget),kOfxPropEffectInstance,0,e);check(callback(kOfxActionCreateInstance,&widget,nullptr,nullptr));require(widget.strings[kOfxInteractPropSlaveToParam].size()==4);
 mock::sd(bag(args),kOfxPropTime,0,42);mock::sd(bag(args),kOfxInteractPropPenPosition,0,345.25);mock::sd(bag(args),kOfxInteractPropPenPosition,1,678.75);
 mock::sd(bag(args),kOfxImageEffectPropRenderScale,0,.5);mock::sd(bag(args),kOfxImageEffectPropRenderScale,1,.25);mock::sd(bag(args),kOfxInteractPropPixelScale,0,2);mock::sd(bag(args),kOfxInteractPropPixelScale,1,3);mock::sd(bag(fx.source.props),kOfxImagePropPixelAspectRatio,0,2);
 require(callback(kOfxInteractActionPenDown,&widget,bag(args),nullptr)==kOfxStatReplyDefault);require(integer(s,"pick_auto",0)==0);
 setI(s,"pick_on_viewer",1);check(callback(kOfxInteractActionPenDown,&widget,bag(args),nullptr));double x=0,y=0;check(param->paramGetValue(handle(s,"pick_point"),&x,&y));require(x==345.25&&y==678.75);require(integer(s,"pick_auto",0)==1);require(number(s,"positive_bal_r",0)==0&&number(s,"positive_bal_g",0)==0&&number(s,"positive_bal_b",0)==0);
 mock::sd(bag(args),kOfxInteractPropPenPosition,0,456.5);mock::sd(bag(args),kOfxInteractPropPenPosition,1,789.5);check(callback(kOfxInteractActionPenMotion,&widget,bag(args),nullptr));check(param->paramGetValue(handle(s,"pick_point"),&x,&y));require(x==456.5&&y==789.5);
 check(callback(kOfxInteractActionPenUp,&widget,bag(args),nullptr));require(!overlayState(bag(widget))->dragging&&!overlayState(bag(widget))->editing);
 mock::sd(bag(args),kOfxInteractPropPenPosition,0,567.5);require(callback(kOfxInteractActionPenMotion,&widget,bag(args),nullptr)==kOfxStatReplyDefault);check(param->paramGetValue(handle(s,"pick_point"),&x,&y));require(x==456.5);
 // A subsequent pick leaves visible manual fine tuning intact when auto is already on.
 setD(s,"positive_bal_r",.25);check(callback(kOfxInteractActionPenDown,&widget,bag(args),nullptr));require(number(s,"positive_bal_r",0)==.25);check(callback(kOfxInteractActionLoseFocus,&widget,nullptr,nullptr));require(!overlayState(bag(widget))->editing);
 // Outside-image down/invalid positions are ignored; active drags clamp to canonical RoD.
 mock::sd(bag(args),kOfxInteractPropPenPosition,0,10);require(callback(kOfxInteractActionPenDown,&widget,bag(args),nullptr)==kOfxStatReplyDefault);
 mock::sd(bag(args),kOfxInteractPropPenPosition,0,500);check(callback(kOfxInteractActionPenDown,&widget,bag(args),nullptr));mock::sd(bag(args),kOfxInteractPropPenPosition,0,5000);check(callback(kOfxInteractActionPenMotion,&widget,bag(args),nullptr));check(param->paramGetValue(handle(s,"pick_point"),&x,&y));require(x<1100&&x>1099);check(callback(kOfxInteractActionPenUp,&widget,bag(args),nullptr));
 mock::sd(bag(args),kOfxInteractPropPenPosition,0,std::nan(""));require(callback(kOfxInteractActionPenDown,&widget,bag(args),nullptr)==kOfxStatReplyDefault);
 // A host-native point handle may own the drag; armed UserEdited still enables sampling.
 setI(s,"pick_auto",0);setD(s,"positive_bal_r",1.07);mock::Bag changed;mock::ss(bag(changed),kOfxPropName,0,"pick_point");mock::ss(bag(changed),kOfxPropChangeReason,0,kOfxChangeUserEdited);check(run(kOfxActionInstanceChanged,e,bag(changed),nullptr));require(integer(s,"pick_auto",0)==1&&number(s,"positive_bal_r",0)==0);
 // A failed parameter edit must release the drag/undo transaction at the ABI boundary.
 auto savedSetter=param->paramSetValue;param->paramSetValue=rejectSetter;mock::sd(bag(args),kOfxInteractPropPenPosition,0,500);require(callback(kOfxInteractActionPenDown,&widget,bag(args),nullptr)==kOfxStatErrValue);require(!overlayState(bag(widget))->editing&&!overlayState(bag(widget))->dragging);param->paramSetValue=savedSetter;
 // Record the GL1 command stream without needing a physical viewer/context.
 auto &gl=nss::OverlayGL::get();gl.available=true;gl.pushAttrib=draw::push;gl.popAttrib=draw::pop;gl.disable=draw::disable;gl.lineWidth=draw::width;gl.color=draw::color;gl.begin=draw::begin;gl.vertex=draw::vertex;gl.end=draw::end;gl.currentContext=draw::context;
 check(param->paramSetValue(handle(s,"pick_point"),345.25,678.75));setD(s,"pick_radius",3);check(callback(kOfxInteractActionDraw,&widget,bag(args),nullptr));require(draw::pushes==1&&draw::pops==1&&draw::begins==2&&draw::ends==2&&draw::vertices.size()==24);
 require(draw::vertices[0]==std::make_pair(329.25,678.75));require(draw::vertices[1]==std::make_pair(361.25,678.75));
 // Pixel-scale crosshair; radius3 at render(.5,.25), PAR2 spans 28x28 canonical units.
 require(draw::vertices[4]==std::make_pair(332.,664.));require(draw::vertices[5]==std::make_pair(360.,664.));require(draw::vertices[7]==std::make_pair(360.,692.));
 gl.currentContext=[]()->void*{return nullptr;};require(callback(kOfxInteractActionDraw,&widget,bag(args),nullptr)==kOfxStatReplyDefault);require(draw::pushes==1);gl.currentContext=draw::context;
 setI(s,"pick_on_viewer",0);require(callback(kOfxInteractActionPenDown,&widget,bag(args),nullptr)==kOfxStatReplyDefault);setI(s,"pick_auto",0);require(callback(kOfxInteractActionDraw,&widget,bag(args),nullptr)==kOfxStatReplyDefault);require(draw::pushes==1);require(mock::redraws>=5);
 check(callback(kOfxActionDestroyInstance,&widget,nullptr,nullptr));check(run(kOfxActionDestroyInstance,e,nullptr,nullptr));
 std::cout<<"PASS: advertised overlay lifecycle, arm/canonical click/drag/up/focus, auto gray and visible reset, bounds, GL crosshair/radius/PAR/render-scale geometry and state restore. Mock GL only; not Resolve.\n";
}
