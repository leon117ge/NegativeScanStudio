// Prints every parameter definition from Describe + DescribeInContext(Filter) in definition order,
// with all int/double/string properties, as canonical text. check_param_defs.py compares the
// Windows/Linux build with the 0.90.14 golden copy and the macOS variant (-DNSS_MAC_RENDER_UI=1)
// with the allowed Rendering-group differences.
#define main nss_original_host_test
#include "host_test.cpp"
#undef main
#include \u003ciomanip\u003e
int main(){
 mock::init();
 // Strict: a second definition of any name (param, group or button) is an error, as in Resolve.
 static auto lenient=mock::params_.paramDefine;
 mock::params_.paramDefine=[](OfxParamSetHandle set,const char *type,const char *name,OfxPropertySetHandle *props)-\u003eOfxStatus{
  if(mock::ptr\u003cmock::Set\u003e(set)-\u003evalues.count(name)){std::cerr\u003c\u003c"duplicate parameter name: "\u003c\u003cname\u003c\u003c"\n";std::abort();}return lenient(set,type,name,props);};
 OfxHost host{nullptr,mock::fetch};setHost(\u0026host);require(entry(kOfxActionLoad,nullptr,nullptr,nullptr)==kOfxStatOK);
 mock::Effect fx;auto e=reinterpret_cast\u003cOfxImageEffectHandle\u003e(\u0026fx);
 require(entry(kOfxActionDescribe,e,nullptr,nullptr)==kOfxStatOK);require(entry(kOfxImageEffectActionDescribeInContext,e,nullptr,nullptr)==kOfxStatOK);
 std::cout\u003c\u003cstd::setprecision(17);
 for(auto \u0026name:fx.params.order){auto \u0026b=fx.params.values[name];std::cout\u003c\u003c"param "\u003c\u003cname\u003c\u003c" type="\u003c\u003cb.type\u003c\u003c"\n";
  for(auto \u0026[k,v]:b.ints){std::cout\u003c\u003c"  int "\u003c\u003ck\u003c\u003c" =";for(auto x:v)std::cout\u003c\u003c" "\u003c\u003cx;std::cout\u003c\u003c"\n";}
  for(auto \u0026[k,v]:b.doubles){std::cout\u003c\u003c"  double "\u003c\u003ck\u003c\u003c" =";for(auto x:v)std::cout\u003c\u003c" "\u003c\u003cx;std::cout\u003c\u003c"\n";}
  for(auto \u0026[k,v]:b.strings){std::cout\u003c\u003c"  string "\u003c\u003ck\u003c\u003c" =";for(auto \u0026x:v)std::cout\u003c\u003c" ["\u003c\u003cx\u003c\u003c"]";std::cout\u003c\u003c"\n";}}
}
