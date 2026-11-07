// Included inside plugin.cpp's private namespace, sharing its OFX boundary helpers.
struct OverlayState {
 OfxImageEffectHandle effect=nullptr;OfxParamSetHandle set=nullptr;bool dragging=false,editing=false;
 void finish(){dragging=false;if(editing){editing=false;param->paramEditEnd(set);}}
 ~OverlayState(){finish();}
};
OfxPropertySetHandle overlayProps(const void *handle_){
 if(!interact||!interact->interactGetPropertySet)throw kOfxStatErrMissingHostFeature;
 OfxPropertySetHandle p=nullptr;check(interact->interactGetPropertySet(reinterpret_cast<OfxInteractHandle>(const_cast<void*>(handle_)),&p));return p;
}
OverlayState *overlayState(OfxPropertySetHandle p){void *state=nullptr;check(prop->propGetPointer(p,kOfxPropInstanceData,0,&state));if(!state)throw kOfxStatErrBadHandle;return static_cast<OverlayState*>(state);}
bool overlayPoint(OverlayState &s,OfxPropertySetHandle args,double time,bool clamp,double &x,double &y){
 if(!args||prop->propGetDouble(args,kOfxInteractPropPenPosition,0,&x)!=kOfxStatOK||prop->propGetDouble(args,kOfxInteractPropPenPosition,1,&y)!=kOfxStatOK||!std::isfinite(x)||!std::isfinite(y))return false;
 OfxRectD rod{};auto i=instance(s.effect);
 if(effect->clipGetRegionOfDefinition&&effect->clipGetRegionOfDefinition(i->src,time,&rod)==kOfxStatOK&&std::isfinite(rod.x1)&&std::isfinite(rod.y1)&&std::isfinite(rod.x2)&&std::isfinite(rod.y2)&&rod.x2>rod.x1&&rod.y2>rod.y1){
  if(clamp){x=std::clamp(x,rod.x1,std::nextafter(rod.x2,rod.x1));y=std::clamp(y,rod.y1,std::nextafter(rod.y2,rod.y1));}
  else if(x<rod.x1||x>=rod.x2||y<rod.y1||y>=rod.y2)return false;
 }
 return true;
}
void overlayWritePoint(OverlayState &s,double x,double y){
 // OFX pen coordinates and XYAbsolute values are already canonical. Never apply
 // viewer zoom/render scale here; render converts them to source pixels once.
 check(param->paramSetValue(handle(s.set,"pick_point"),x,y));
}
OfxStatus overlayEntry(const char *action,const void *handle_,OfxPropertySetHandle args,OfxPropertySetHandle){
 OverlayState *active=nullptr;
 try{
  if(!action||!handle_)return kOfxStatErrBadHandle;
  auto p=overlayProps(handle_);
  if(!std::strcmp(action,kOfxActionDescribe))return kOfxStatOK;
  if(!std::strcmp(action,kOfxActionCreateInstance)){
   void *owner=nullptr;check(prop->propGetPointer(p,kOfxPropEffectInstance,0,&owner));if(!owner)throw kOfxStatErrBadHandle;
   auto state=std::make_unique<OverlayState>();state->effect=static_cast<OfxImageEffectHandle>(owner);state->set=params(state->effect);
   const char *slaves[]={"pick_on_viewer","pick_auto","pick_point","pick_radius"};for(int c=0;c<4;c++)check(prop->propSetString(p,kOfxInteractPropSlaveToParam,c,slaves[c]));
   check(prop->propSetPointer(p,kOfxPropInstanceData,0,state.get()));state.release();return kOfxStatOK;
  }
  auto s=overlayState(p);active=s;
  if(!std::strcmp(action,kOfxActionDestroyInstance)){active=nullptr;delete s;prop->propSetPointer(p,kOfxPropInstanceData,0,nullptr);return kOfxStatOK;}
  if(!std::strcmp(action,kOfxInteractActionLoseFocus)){s->finish();return kOfxStatOK;}
  double time=0;if(args)prop->propGetDouble(args,kOfxPropTime,0,&time);
  bool armed=integer(s->set,"pick_on_viewer",time)!=0;
  if(!std::strcmp(action,kOfxInteractActionDraw)){
   if(!armed&&!integer(s->set,"pick_auto",time))return kOfxStatReplyDefault;
   auto &gl=nss::OverlayGL::get();if(!gl.available||!gl.currentContext())return kOfxStatReplyDefault;
   double x=0,y=0;check(param->paramGetValueAtTime(handle(s->set,"pick_point"),time,&x,&y));if(!std::isfinite(x)||!std::isfinite(y))return kOfxStatReplyDefault;
   double screenX=1,screenY=1,sx=1,sy=1,aspect=1;
   if(args){prop->propGetDouble(args,kOfxInteractPropPixelScale,0,&screenX);prop->propGetDouble(args,kOfxInteractPropPixelScale,1,&screenY);prop->propGetDouble(args,kOfxImageEffectPropRenderScale,0,&sx);prop->propGetDouble(args,kOfxImageEffectPropRenderScale,1,&sy);}
   auto valid=[](double v){return std::isfinite(v)&&v>0;};
   if(!valid(screenX))screenX=1;
   if(!valid(screenY))screenY=1;
   if(!valid(sx))sx=1;
   if(!valid(sy))sy=1;
   OfxPropertySetHandle cp=nullptr;if(effect->clipGetPropertySet(instance(s->effect)->src,&cp)==kOfxStatOK)prop->propGetDouble(cp,kOfxImagePropPixelAspectRatio,0,&aspect);if(!valid(aspect))aspect=1;
   double radius=std::clamp(number(s->set,"pick_radius",time),0.,32.);if(!std::isfinite(radius))radius=0;radius=std::floor(radius);
   double ix=std::floor(x*sx/aspect),iy=std::floor(y*sy);
   if(!std::isfinite(ix)||!std::isfinite(iy))return kOfxStatReplyDefault;
   gl.marker(x,y,screenX,screenY,(ix-radius)*aspect/sx,(iy-radius)/sy,(ix+radius+1)*aspect/sx,(iy+radius+1)/sy);return kOfxStatOK;
  }
  if(!armed){s->finish();return kOfxStatReplyDefault;}
  if(!std::strcmp(action,kOfxInteractActionPenDown)){
   double x=0,y=0;if(!overlayPoint(*s,args,time,false,x,y))return kOfxStatReplyDefault;
   s->finish();check(param->paramEditBegin(s->set,"Pick gray on viewer"));s->editing=true;s->dragging=true;
   if(!integer(s->set,"pick_auto",time)){clearPickedBalance(s->set);setI(s->set,"pick_auto",1);}
   overlayWritePoint(*s,x,y);
  }else if(!std::strcmp(action,kOfxInteractActionPenMotion)){
   if(!s->dragging)return kOfxStatReplyDefault;
   double x=0,y=0;if(overlayPoint(*s,args,time,true,x,y))overlayWritePoint(*s,x,y);
  }else if(!std::strcmp(action,kOfxInteractActionPenUp)){
   if(!s->dragging)return kOfxStatReplyDefault;
   double x=0,y=0;if(overlayPoint(*s,args,time,true,x,y))overlayWritePoint(*s,x,y);s->finish();
  }else return kOfxStatReplyDefault;
  if(interact->interactRedraw)interact->interactRedraw(reinterpret_cast<OfxInteractHandle>(const_cast<void*>(handle_)));
  return kOfxStatOK;
 }catch(OfxStatus status){if(active)active->finish();return status;}catch(const std::bad_alloc &){if(active)active->finish();return kOfxStatErrMemory;}catch(...){if(active)active->finish();return kOfxStatFailed;}
}
