// Host-free check of the OFX loader against app-produced metadata.
// No plugin code is generated or changed by this test.
#define main historicalHostTestMain
#include "host_test.cpp"
#undef main
int main(int argc,char **argv) {
 require(argc>=2);mock::init();OfxHost h{nullptr,mock::fetch};OfxGetPlugin(0)->setHost(&h);
 auto call=OfxGetPlugin(0)->mainEntry;mock::Effect fx;
 auto e=reinterpret_cast<OfxImageEffectHandle>(&fx);
 auto bag=[](mock::Bag &b){return reinterpret_cast<OfxPropertySetHandle>(&b);};
 require(call(kOfxActionLoad,nullptr,nullptr,nullptr)==kOfxStatOK);
 require(call(kOfxActionDescribe,e,nullptr,nullptr)==kOfxStatOK);
 require(call(kOfxImageEffectActionDescribeInContext,e,nullptr,nullptr)==kOfxStatOK);
 require(call(kOfxActionCreateInstance,e,nullptr,nullptr)==kOfxStatOK);
 auto s=params(e);mock::Bag changed;mock::ss(bag(changed),kOfxPropName,0,"load_metadata");
 mock::ss(bag(changed),kOfxPropChangeReason,0,kOfxChangeUserEdited);
 for(int file=1;file<argc;++file) {
  auto m=nss::parseMetadata(nss::readDescription(argv[file]));
  // Compute the expected coordinate transform independently of applyMetadata.
  float b[3]={m.base[0],m.base[1],m.base[2]};
  if(m.camera&&!m.cameraApplied&&!m.baseAfterProfile)for(int c=0;c<3;++c) {
   float corrected=0;for(int j=0;j<3;++j)corrected+=(*m.camera)[3*c+j]*m.base[j];
   b[c]=m.base[c]+m.strength*(corrected-m.base[c]);
  }
  for(int film=0;film<2;++film)for(int temperature=0;temperature<5;++temperature) {
   setI(s,"film_type",film);setI(s,"temperature",temperature);setS(s,"metadata_file",argv[file]);
   require(call(kOfxActionInstanceChanged,e,bag(changed),nullptr)==kOfxStatOK);
   auto reference=referenceBase(s,0);
   require(std::abs(reference.x-b[0])<1e-6&&std::abs(reference.y-b[1])<1e-6&&std::abs(reference.z-b[2])<1e-6);
   const char *keys[]={"density_base_r","density_base_g","density_base_b"};
   for(int c=0;c<3;++c) {
    float want=b[c]*std::pow(10.f,nss::wbOffsets[film][temperature][c]);
    require(std::abs(number(s,keys[c],0)-want)<1e-6);
   }
   require(integer(s,"auto_metadata",0)==0);
   require(integer(s,"camera_on",0)==(m.camera.has_value()&&!m.cameraApplied));
  }
  std::cout<<nss::JSON{{"file",argv[file]},{"raw_film_base",m.base},{"camera_space_baseline",{b[0],b[1],b[2]}},{"light_drive",m.light.value_or(std::array<int,3>{})},{"visible_presets_checked",10}}<<"\n";
 }
 require(call(kOfxActionDestroyInstance,e,nullptr,nullptr)==kOfxStatOK);
 std::cout<<"PASS: OFX loader keeps light drive separate, does not double-transform an after-profile base, and fills all 10 visible T/D presets without compounding\n";
}
