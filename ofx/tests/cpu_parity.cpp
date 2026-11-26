// CPU render parity fixture: renders fixed synthetic frames through the real plugin
// entry point (mock host from host_test.cpp, CPU path, the plugin's own worker threads)
// and writes the raw float32 RGBA output of every variant. The same source is built
// for Windows x64 (release toolchain and flags) and for macOS arm64 (build-mac.sh flags);
// check_cpu_parity.py compares the two output sets.
// Usage: cpu_parity <output directory>
#include <cstdlib>
#ifdef _WIN32
// host_test.cpp calls POSIX setenv; MinGW only has _putenv_s.
static int setenv(const char *key,const char *value,int){return _putenv_s(key,value);}
#endif
#define main nss_host_test_main
#include "host_test.cpp"
#undef main
#include <cstdint>
namespace parity {
constexpr int W=256,H=256;
// Inputs use only exactly rounded IEEE operations (no libm), so they are bit-identical on every platform.
std::vector<float> frame(){
 std::vector<float> px(size_t(W)*H*4);uint32_t state=2383u;
 auto rnd=[&]{state=state*1664525u+1013904223u;return float(state>>8)*(1.f/16777216.f);};
 for(int y=0;y<H;y++)for(int x=0;x<W;x++){
  float *p=&px[(size_t(y)*W+x)*4];
  if(y<64){float t=float(x)/float(W-1);p[0]=t*t;p[1]=t*t*.9f+.001f;p[2]=t*t*.8f+.002f;}        // neutral-ish ramps
  else if(y<128){float t=float(x)/float(W-1),u=float(y-64)/63.f;p[0]=.02f+.7f*t;p[1]=.02f+.7f*u;p[2]=.02f+.7f*(1.f-t)*u;} // colour sweep
  else if(y<192){p[0]=.001f+.799f*rnd();p[1]=.001f+.799f*rnd();p[2]=.001f+.799f*rnd();}           // scan-like noise
  else{static const float edge[][3]={{0,0,0},{1,1,1},{-.05f,.000001f,1.2f},{.02740667f,.02740668f,.02740669f},{.5f,.5f,.5f},{4,2,1},{.18f,.18f,.18f},{1e-7f,1e-6f,1e-5f}};
   int k=(x/8)%8;float s=1.f+float(y-192)/64.f;p[0]=edge[k][0]*s;p[1]=edge[k][1]*s;p[2]=edge[k][2]*s;} // edge cases
  p[3]=1.f;
 }
 return px;
}
struct Set {const char *key;bool integer;double value;};
struct Variant {const char *name;std::vector<Set> sets;bool premult;};
std::vector<Variant> variants(){
 return {
  {"default",{},false},
  {"out_mode0",{{"positive_out_mode",true,0}},false},{"out_mode1",{{"positive_out_mode",true,1}},false},
  {"out_mode3",{{"positive_out_mode",true,3}},false},{"out_mode4",{{"positive_out_mode",true,4}},false},
  {"contrast1",{{"positive_contrast",true,1}},false},{"contrast2",{{"positive_contrast",true,2}},false},{"contrast3",{{"positive_contrast",true,3}},false},
  {"stage0",{{"stage",true,0}},false},
  {"tone_off",{{"tone_shoulder_on",true,0},{"tone_toe_on",true,0},{"tone_highlight_desat_on",true,0},{"tone_highlight_cream_on",true,0},{"tone_shadow_desat_on",true,0}},false},
  {"gray_point",{{"positive_gray_on",true,1},{"positive_gray_r",false,520},{"positive_gray_g",false,470},{"positive_gray_b",false,430}},false},
  {"manual_wb",{{"positive_film_wb",true,0},{"positive_gam_r",false,.45},{"positive_gam_g",false,.54},{"positive_gam_b",false,.53},{"positive_expo",false,-.5},{"positive_bal_r",false,.1}},false},
  {"density_di_trans",{{"density_in_di",true,1},{"density_out_trans",true,1},{"density_clamp01",true,0}},false},
  {"density_controls",{{"density_ev_stops",false,-.7},{"density_blk_r",false,.0001},{"density_blk_b",false,.002},{"density_clamp01",true,0},{"density_base_r",false,.48},{"density_base_b",false,.46}},false},
  {"camera_matrix",{{"camera_0",false,.86426},{"camera_1",false,.10467},{"camera_2",false,-.01969},{"camera_3",false,-.05068},{"camera_4",false,.98176},{"camera_5",false,.01644},{"camera_6",false,.11287},{"camera_7",false,-.14623},{"camera_8",false,1.04095},{"camera_strength",false,.8}},false},
  {"tone_strong",{{"tone_shoulder_strength",false,1.6},{"tone_toe_strength",false,1.4},{"tone_highlight_desat",false,.9},{"tone_highlight_cream",false,1.5},{"tone_shadow_desat",false,.95}},false},
  {"premultiplied",{},true},
 };
}
}
int main(int argc,char **argv){
 if(argc!=2){std::cerr<<"usage: cpu_parity <output directory>\n";return 2;}
 std::filesystem::path out(argv[1]);std::filesystem::create_directories(out);
 // Keep user balance presets out of the run: point the plugin at a file inside the output folder.
 setenv("NSS_BALANCE_PRESETS",(out/"balance_presets.json").string().c_str(),1);
 mock::init();OfxHost h{nullptr,mock::fetch};OfxGetPlugin(0)->setHost(&h);auto call=OfxGetPlugin(0)->mainEntry;
 auto bag=[](mock::Bag &b){return reinterpret_cast<OfxPropertySetHandle>(&b);};
 auto input=parity::frame();std::ofstream manifest(out/"manifest.txt");
 manifest<<"nss-cpu-parity 1 "<<parity::W<<"x"<<parity::H<<" rgba float32 little-endian\n";
 for(auto &v:parity::variants()){
  mock::Effect fx;auto e=reinterpret_cast<OfxImageEffectHandle>(&fx);
  require(call(kOfxActionLoad,nullptr,nullptr,nullptr)==kOfxStatOK);require(call(kOfxActionDescribe,e,nullptr,nullptr)==kOfxStatOK);
  require(call(kOfxImageEffectActionDescribeInContext,e,nullptr,nullptr)==kOfxStatOK);require(call(kOfxActionCreateInstance,e,nullptr,nullptr)==kOfxStatOK);
  auto s=reinterpret_cast<OfxParamSetHandle>(&fx.params);setI(s,"auto_metadata",0);setI(s,"render_device",1);
  for(auto &x:v.sets){if(x.integer)setI(s,x.key,int(x.value));else setD(s,x.key,x.value);}
  std::vector<float> src=input,dst(src.size(),-1.f);
  if(v.premult)for(size_t i=0;i<src.size();i+=4){float a=.25f+.5f*float((i/4)%3);src[i]*=a;src[i+1]*=a;src[i+2]*=a;src[i+3]=a;}
  int bounds[4]={0,0,parity::W,parity::H};
  for(auto *b:{&fx.source.image,&fx.output.image}){auto p=bag(*b);mock::ss(p,kOfxImageEffectPropPixelDepth,0,kOfxBitDepthFloat);mock::ss(p,kOfxImageEffectPropComponents,0,kOfxImageComponentRGBA);
   mock::ss(p,kOfxImageEffectPropPreMultiplication,0,v.premult?kOfxImagePreMultiplied:kOfxImageUnPreMultiplied);mock::si(p,kOfxImagePropRowBytes,0,parity::W*16);for(int j=0;j<4;j++)mock::si(p,kOfxImagePropBounds,j,bounds[j]);}
  mock::sp(bag(fx.source.image),kOfxImagePropData,0,src.data());mock::sp(bag(fx.output.image),kOfxImagePropData,0,dst.data());
  mock::Bag args;mock::sd(bag(args),kOfxPropTime,0,0);for(int j=0;j<4;j++)mock::si(bag(args),kOfxImageEffectPropRenderWindow,j,bounds[j]);
  require(call(kOfxImageEffectActionRender,e,bag(args),nullptr)==kOfxStatOK);
  std::string engine;{auto i=instance(e);std::lock_guard<std::mutex> lock(i->mutex);engine=i->lastRender;}
  require(engine.rfind("CPU ",0)==0);
  std::ofstream f(out/(std::string(v.name)+".f32"),std::ios::binary);f.write(reinterpret_cast<const char*>(dst.data()),std::streamsize(dst.size()*sizeof(float)));require(bool(f));
  manifest<<v.name<<" "<<engine<<"\n";std::cout<<v.name<<": "<<engine<<"\n";
  require(call(kOfxActionDestroyInstance,e,nullptr,nullptr)==kOfxStatOK);
 }
 std::cout<<"PASS: wrote "<<parity::variants().size()<<" CPU render variants to "<<out.string()<<"\n";
}
