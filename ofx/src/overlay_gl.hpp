#pragma once
#include <cstring>
#ifdef _WIN32
#ifndef NOMINMAX
#define NOMINMAX
#endif
#include <windows.h>
#define NSS_GL_CALL __stdcall
#else
#include <dlfcn.h>
#define NSS_GL_CALL
#endif
namespace nss {
// Only called by OFX Draw with the host's compatibility OpenGL context current.
struct OverlayGL {
 void (NSS_GL_CALL *pushAttrib)(unsigned)=nullptr;
 void (NSS_GL_CALL *popAttrib)()=nullptr;
 void (NSS_GL_CALL *disable)(unsigned)=nullptr;
 void (NSS_GL_CALL *lineWidth)(float)=nullptr;
 void (NSS_GL_CALL *color)(float,float,float)=nullptr;
 void (NSS_GL_CALL *begin)(unsigned)=nullptr;
 void (NSS_GL_CALL *vertex)(double,double)=nullptr;
 void (NSS_GL_CALL *end)()=nullptr;
 void* (NSS_GL_CALL *currentContext)()=nullptr;
 bool available=false;
 OverlayGL(){
#ifdef _WIN32
  auto module=LoadLibraryExW(L"opengl32.dll",nullptr,LOAD_LIBRARY_SEARCH_SYSTEM32);
  auto symbol=[&](const char *name){return module?GetProcAddress(module,name):nullptr;};
#else
  auto module=dlopen("libGL.so.1",RTLD_NOW|RTLD_LOCAL);
  auto symbol=[&](const char *name){return module?dlsym(module,name):nullptr;};
#endif
#define LOAD(member,name) {auto address=symbol(name);static_assert(sizeof(address)==sizeof(member));std::memcpy(&member,&address,sizeof(member));if(!member)return;}
  LOAD(pushAttrib,"glPushAttrib");LOAD(popAttrib,"glPopAttrib");LOAD(disable,"glDisable");LOAD(lineWidth,"glLineWidth");LOAD(color,"glColor3f");LOAD(begin,"glBegin");LOAD(vertex,"glVertex2d");LOAD(end,"glEnd");
#ifdef _WIN32
  LOAD(currentContext,"wglGetCurrentContext");
#else
  LOAD(currentContext,"glXGetCurrentContext");
#endif
#undef LOAD
  available=true;
 }
 static OverlayGL &get(){static OverlayGL gl;return gl;}
 void marker(double x,double y,double screenX,double screenY,double x1,double y1,double x2,double y2){
  // Preserve host state; leave the host's canonical projection/matrices untouched.
  pushAttrib(0x00000001|0x00000004|0x00002000|0x00004000|0x00000100);
  disable(0x0B71);disable(0x0B50);disable(0x0DE1);disable(0x0BE2);disable(0x0B24);
  for(int pass=0;pass<2;pass++){
   lineWidth(pass?1.f:3.f);color(pass?.1f:0.f,pass?1.f:0.f,pass?1.f:0.f);
   begin(0x0001); // GL_LINES: crosshair + exact sampled pixel rectangle.
   vertex(x-8*screenX,y);vertex(x+8*screenX,y);vertex(x,y-8*screenY);vertex(x,y+8*screenY);
   vertex(x1,y1);vertex(x2,y1);vertex(x2,y1);vertex(x2,y2);vertex(x2,y2);vertex(x1,y2);vertex(x1,y2);vertex(x1,y1);
   end();
  }
  popAttrib();
 }
};
}
#undef NSS_GL_CALL
