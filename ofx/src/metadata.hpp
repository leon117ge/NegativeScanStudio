#pragma once
#include "pipeline.hpp"
#include "../vendor/json/json.hpp"
#include <fstream>
#include <filesystem>
#include <stdexcept>
#include <optional>
namespace nss {
using JSON=nlohmann::json;
inline std::string readDescription(const std::string &path) {
 std::ifstream f(std::filesystem::u8path(path),std::ios::binary);
 if(!f)throw std::runtime_error("Cannot open TIFF/JSON file");
 auto read=[&](uint64_t off,size_t n){if(n>1048576)throw std::runtime_error("Metadata exceeds 1 MiB");std::string b(n,'\0');f.seekg(off);f.read(b.data(),n);if(size_t(f.gcount())!=n)throw std::runtime_error("Truncated TIFF/JSON");return b;};
 auto h=read(0,8);
 if((h[0]!='I'||h[1]!='I')&&(h[0]!='M'||h[1]!='M')) {
  f.clear();f.seekg(0,std::ios::end);auto n=f.tellg();if(n<0||n>1048576)throw std::runtime_error("Invalid JSON size");return read(0,size_t(n));
 }
 bool le=h[0]=='I';
 auto num=[&](const std::string &b,size_t i,int n)->uint32_t {uint32_t v=0;for(int c=0;c<n;c++)v|=uint32_t(uint8_t(b[i+c]))<<(8*(le?c:n-1-c));return v;};
 if(num(h,2,2)!=42)throw std::runtime_error("Only classic TIFF supported (use JSON for BigTIFF)");
 uint64_t off=num(h,4,4);auto count=num(read(off,2),0,2);
 if(count>4096)throw std::runtime_error("Invalid TIFF IFD");
 auto entries=read(off+2,count*12);
 for(unsigned i=0;i<count;i++) {size_t p=i*12;if(num(entries,p,2)!=270)continue;
  if(num(entries,p+2,2)!=2)throw std::runtime_error("Description is not ASCII JSON");
  auto n=num(entries,p+4,4);auto b=n<=4?entries.substr(p+8,n):read(num(entries,p+8,4),n);
  while(!b.empty()&&b.back()=='\0')b.pop_back();
  return b;
 }
 throw std::runtime_error("No NSS metadata in TIFF; old scans require manual base values");
}
struct Metadata {
 std::array<float,3> base{};std::optional<std::array<int,3>> light;
 std::optional<std::array<float,9>> camera;float strength=1;bool cameraApplied=false;
};
inline Metadata parseMetadata(const std::string &text) {
 auto j=JSON::parse(text);
 if(j.value("schema","")!="nss.scan/1")throw std::runtime_error("Unsupported NSS schema");
 if(j.value("encoding","")!="linear" || !j.value("processing","").empty())throw std::runtime_error("Metadata is not an unprocessed linear negative");
 Metadata m;
 if(!j.contains("film_base"))throw std::runtime_error("No measured film base in metadata");
 if(!j.at("film_base").is_array()||j.at("film_base").size()!=3)throw std::runtime_error("Film base must have 3 elements");
 m.base=j.at("film_base").get<std::array<float,3>>();
 for(float v:m.base)if(!std::isfinite(v)||v<.0001f||v>16.f)throw std::runtime_error("Invalid film base");
 if(j.contains("light_rgb")) {m.light=j.at("light_rgb").get<std::array<int,3>>();for(int v:*m.light)if(v<0||v>65535)throw std::runtime_error("Invalid light drive value");}
 if(j.contains("camera_matrix")) {
  if(!j.at("camera_matrix").is_array()||j.at("camera_matrix").size()!=9)throw std::runtime_error("Matrix must have 9 elements");
  m.camera=j.at("camera_matrix").get<std::array<float,9>>();for(float v:*m.camera)if(!std::isfinite(v)||std::abs(v)>16)throw std::runtime_error("Invalid camera matrix");
  m.strength=j.at("camera_strength").get<float>();if(!std::isfinite(m.strength)||m.strength<0||m.strength>1)throw std::runtime_error("Invalid camera strength");
 }
 m.cameraApplied=j.value("camera_applied",false);return m;
}
inline void applyMetadata(Pipeline &p,const Metadata &m) {
 // A new file never inherits a previous roll's camera profile.
 p.cameraOn=m.camera.has_value()&&!m.cameraApplied;p.neutralise=0;
 if(m.camera){p.camera=*m.camera;p.cameraStrength=m.strength;}
 Vec3 b=p.cameraPixel({m.base[0],m.base[1],m.base[2]});
 if(b.x<.0001f||b.y<.0001f||b.z<.0001f)throw std::runtime_error("Camera-corrected base is not positive");
 p.density.base_r=b.x;p.density.base_g=b.y;p.density.base_b=b.z;
}
}
