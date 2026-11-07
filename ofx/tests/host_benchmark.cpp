// Actual OFX Render action under the same minimal suite host on both revisions.
#define main nss_unused_host_main
#include NSS_HOST_HEADER
#undef main
#include <chrono>
#include <fstream>
static uint64_t valueCalls=0,fullSourceRequests=0,sourceRequests=0;static OfxImageClipHandle sourceClip=nullptr;
OfxStatus countedValue(OfxParamHandle h,OfxTime t,...){(void)t;++valueCalls;va_list a;va_start(a,t);auto result=mock::value(h,a);va_end(a);return result;}
OfxStatus countedImage(OfxImageClipHandle c,OfxTime t,const OfxRectD *r,OfxPropertySetHandle *p){if(c==sourceClip){++sourceRequests;if(!r)++fullSourceRequests;}return mock::image(c,t,r,p);}
int main(int argc,char **argv){if(argc!=7)return 2;int w=std::atoi(argv[2]),h=std::atoi(argv[3]),iterations=std::atoi(argv[6]);bool tile=std::string(argv[5])=="tile";
 std::vector<float> input(size_t(w)*h*4),output(input.size());std::ifstream f(argv[1],std::ios::binary);f.read(reinterpret_cast<char*>(input.data()),input.size()*4);require(size_t(f.gcount())==input.size()*4);
 mock::init();mock::params_.paramGetValueAtTime=countedValue;mock::effects.clipGetImage=countedImage;OfxHost host{nullptr,mock::fetch};setHost(&host);check(entry(kOfxActionLoad,nullptr,nullptr,nullptr));mock::Effect fx;auto e=reinterpret_cast<OfxImageEffectHandle>(&fx);check(describe(e));check(describeContext(e));check(entry(kOfxActionCreateInstance,e,nullptr,nullptr));auto s=params(e);
 setI(s,"auto_metadata",1);setS(s,"metadata_file",argv[4]);setD(s,"positive_expo",.9);setD(s,"positive_bal_r",1.4);setD(s,"positive_bal_g",0);setD(s,"positive_bal_b",.8);
 bool current=fx.params.values.count("gamma_scale")!=0;
 if(current)setD(s,"gamma_scale",1.15);else{setD(s,"positive_gam_r",.55f*1.15f);setD(s,"positive_gam_g",.635f*1.15f);setD(s,"positive_gam_b",.62f*1.15f);}
 auto bag=[](mock::Bag &b){return reinterpret_cast<OfxPropertySetHandle>(&b);};sourceClip=reinterpret_cast<OfxImageClipHandle>(&fx.source);
 int bounds[]={0,0,w,h};for(auto *b:{&fx.source.image,&fx.output.image}){auto p=bag(*b);mock::ss(p,kOfxImageEffectPropPixelDepth,0,kOfxBitDepthFloat);mock::ss(p,kOfxImageEffectPropComponents,0,kOfxImageComponentRGBA);mock::ss(p,kOfxImageEffectPropPreMultiplication,0,kOfxImageUnPreMultiplied);mock::si(p,kOfxImagePropRowBytes,0,w*16);for(int c=0;c<4;c++)mock::si(p,kOfxImagePropBounds,c,bounds[c]);}
 mock::sp(bag(fx.source.image),kOfxImagePropData,0,input.data());mock::sp(bag(fx.output.image),kOfxImagePropData,0,output.data());mock::Bag args;mock::sd(bag(args),kOfxPropTime,0,0);
 int window[]={0,0,tile?16:w,tile?16:h};for(int c=0;c<4;c++)mock::si(bag(args),kOfxImageEffectPropRenderWindow,c,window[c]);
 check(entry(kOfxImageEffectActionRender,e,bag(args),nullptr));valueCalls=fullSourceRequests=sourceRequests=0;
 auto start=std::chrono::steady_clock::now();for(int i=0;i<iterations;i++)check(entry(kOfxImageEffectActionRender,e,bag(args),nullptr));auto end=std::chrono::steady_clock::now();double ms=std::chrono::duration<double,std::milli>(end-start).count()/iterations;
 auto timedValueCalls=valueCalls;
 auto p=readParams(s,0);nss::applyMetadata(p,nss::parseMetadata(nss::readDescription(argv[4])));nss::Prepared reference(p);double error=0,sum=0;
 for(int y=window[1];y<window[3];y++)for(int x=window[0];x<window[2];x++)for(int c=0;c<4;c++){auto i=(size_t(y)*w+x)*4+c;require(std::isfinite(output[i]));sum+=output[i];}
 for(int y=0;y<window[3];y+=std::max(1,window[3]/16))for(int x=0;x<window[2];x+=std::max(1,window[2]/16)){float expected[4];auto i=(size_t(y)*w+x)*4;reference.rgba(input.data()+i,expected,false);for(int c=0;c<4;c++)error=std::max(error,double(std::abs(output[i+c]-expected[c])));}
 require(error<1e-5);nss::JSON report{{"ms_per_render",ms},{"iterations",iterations},{"width",w},{"height",h},{"window",window},{"max_sample_error",error},{"checksum",sum},{"failures",0},{"value_calls",timedValueCalls},{"source_requests",sourceRequests},{"full_source_requests",fullSourceRequests}};
#ifdef NSS_BENCH_CURRENT
 report["prepared_builds"]=instance(e)->preparedCache.builds;report["metadata_reads"]=instance(e)->metadataReads;report["metadata_probes"]=instance(e)->metadataProbes;
#endif
 check(entry(kOfxActionDestroyInstance,e,nullptr,nullptr));std::cout<<report.dump()<<"\n";
}
