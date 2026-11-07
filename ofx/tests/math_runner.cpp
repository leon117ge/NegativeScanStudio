#include "../src/metadata.hpp"
#include "../src/prepared.hpp"
#include <iostream>
int main(int argc,char **argv) {
 if(argc>1){try {nss::Pipeline p;auto m=nss::parseMetadata(nss::readDescription(argv[1]));nss::applyMetadata(p,m);std::cout<<nss::JSON{{"base",{p.density.base_r,p.density.base_g,p.density.base_b}},{"camera_on",p.cameraOn},{"light",m.light? nss::JSON(*m.light):nss::JSON(nullptr)}}.dump()<<"\n";return 0;}catch(const std::exception &e){std::cerr<<e.what()<<"\n";return 2;}}
 std::string line;while(std::getline(std::cin,line)){
  auto j=nss::JSON::parse(line);nss::Pipeline p;p.cameraOn=0;
#include "json_params.inc"
  p.stage=j.value("stage",2);p.cameraOn=j.value("camera_on",0);p.cameraStrength=j.value("camera_strength",1.f);if(j.contains("camera"))p.camera=j.at("camera").get<std::array<float,9>>();
  nss::JSON out=nss::JSON::array();nss::Prepared prepared(p);
  if(j.value("dump_constants",false)){std::cout<<nss::JSON(prepared.values).dump()<<"\n";continue;}
  for(auto &v:j.at("pixels")){auto o=prepared.pixel({v[0].get<float>(),v[1].get<float>(),v[2].get<float>()});out.push_back({o.x,o.y,o.z});}
  std::cout<<out.dump()<<"\n";
 }
}
