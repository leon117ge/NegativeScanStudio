// Prints every parameter definition from Describe + DescribeInContext(Filter) in definition order,
// with all int/double/string properties, as canonical text. check_param_defs.py compares the
// Windows/Linux build with the 0.90.14 golden copy and the macOS variant (-DNSS_MAC_RENDER_UI=1)
// with the allowed Rendering-group differences.
#define main nss_original_host_test
#include "host_test.cpp"
#undef main
#include <iomanip>
int main(){
 mock::init();
 // Strict: a second definition of any name (param, group or button) is an error, as in Resolve.
 static auto lenient=mock::params_.paramDefine;
 mock::params_.paramDefine=[](OfxParamSetHandle set,const char *type,const char *name,OfxPropertySetHandle *props)->OfxStatus{
  if(mock::ptr<mock::Set>(set)->values.count(name)){std::cerr<<"duplicate parameter name: "<<name<<"\n";std::abort();}return lenient(set,type,name,props);};
 OfxHost host{nullptr,mock::fetch};setHost(&host);require(entry(kOfxActionLoad,nullptr,nullptr,nullptr)==kOfxStatOK);
 mock::Effect fx;auto e=reinterpret_cast<OfxImageEffectHandle>(&fx);
 require(entry(kOfxActionDescribe,e,nullptr,nullptr)==kOfxStatOK);require(entry(kOfxImageEffectActionDescribeInContext,e,nullptr,nullptr)==kOfxStatOK);
 std::cout<<std::setprecision(17);
 for(auto &name:fx.params.order){auto &b=fx.params.values[name];std::cout<<"param "<<name<<" type="<<b.type<<"\n";
  for(auto &[k,v]:b.ints){std::cout<<"  int "<<k<<" =";for(auto x:v)std::cout<<" "<<x;std::cout<<"\n";}
  for(auto &[k,v]:b.doubles){std::cout<<"  double "<<k<<" =";for(auto x:v)std::cout<<" "<<x;std::cout<<"\n";}
  for(auto &[k,v]:b.strings){std::cout<<"  string "<<k<<" =";for(auto &x:v)std::cout<<" ["<<x<<"]";std::cout<<"\n";}}
}
