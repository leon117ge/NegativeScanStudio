// Shared C++/OpenCL scalar math. Constants are prepared once per render.
#ifndef NSS_PARAMS
#define NSS_PARAMS const float *
#endif
inline float nss_di_decode(float v){return v>.02740668f?NSS_EXP2(v/.07329248f-7.f)-.0075f:v/10.44426855f;}
inline float nss_channel(float value,NSS_PARAMS p,int c){
 float tr=NSS_MAX((value-p[9+c])*p[5]/NSS_MAX(p[6+c],.000001f),.000001f);
 float code=p[3]+NSS_LOG(tr)*p[4];
 if(p[1]!=0.f)code=tr;
 if(p[2]!=0.f)code=NSS_MIN(NSS_MAX(code,0.f),1.f);
 if(p[12]==0.f||p[1]!=0.f)return code;
 float d=(code*1023.f-95.f)*.002f;
 float l=p[22+c]*(NSS_EXP(NSS_MIN(NSS_MAX(2.302585f*(d-p[15])/p[16+c],-30.f),20.f))-p[19+c])/(1.f-p[19+c]);
 l=NSS_MIN(NSS_MAX(l,0.f),64.f);
 if(p[14]!=0.f){float y=.18f*NSS_POW(l/.18f,p[25]);l=y>p[26]?p[26]+(1.f-p[26])*NSS_TANH((y-p[26])/(1.f-p[26])):y;}
 int mode=(int)p[13];
 if(mode==0)return l<.018f?4.5f*l:1.099f*NSS_POW(l,.45f)-.099f;
 if(mode==1)return l>.00262409f?(NSS_LOG2(l+.0075f)+7.f)*.07329248f:l*10.44426855f;
 if(mode==2)return NSS_MAX((685.f+300.f*.4342944819f*NSS_LOG(l*.98921f+.01079f))/1023.f,0.f);
 if(mode==3)return l>.0078125f?(NSS_LOG2(l)+9.72f)/17.52f:10.5402377417f*l+.0729055342f;
 return l;
}
