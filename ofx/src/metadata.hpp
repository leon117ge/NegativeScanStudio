#pragma once
#include "pipeline.hpp"
#include "../vendor/json/json.hpp"
#include <fstream>
#include <filesystem>
#include <stdexcept>
#include <optional>
#include <cstdio>
#include <cerrno>
#include <memory>
namespace nss {
using JSON=nlohmann::json;
inline std::string normalizePath(std::string path) {
 auto first=path.find_first_not_of(" \t\r\n");if(first==std::string::npos)return "";
 path=path.substr(first,path.find_last_not_of(" \t\r\n")-first+1);
 if(path.size()>1&&((path.front()=='"'&&path.back()=='"')||(path.front()==39&&path.back()==39)))path=path.substr(1,path.size()-2);
 if(path.rfind("file://",0)==0){
  path.erase(0,7);std::string decoded;
  for(size_t i=0;i<path.size();i++){if(path[i]=='%'&&i+2<path.size()){auto hex=[](char c){return c>='0'&&c<='9'?c-'0':c>='A'&&c<='F'?c-'A'+10:c>='a'&&c<='f'?c-'a'+10:-1;};int a=hex(path[i+1]),b=hex(path[i+2]);if(a>=0&&b>=0){decoded+=char(a*16+b);i+=2;continue;}}decoded+=path[i];}
  path=decoded;
#ifdef _WIN32
  if(path.size()>2&&path[0]=='/'&&path[2]==':')path.erase(0,1);
  else if(!path.empty()&&path[0]!='/')path="//"+path;
#endif
 }
 if(path.find('\0')!=std::string::npos)throw std::runtime_error("Invalid file path");
 return path;
}
inline std::string readDescription(const std::string &input) {
 auto path=std::filesystem::u8path(normalizePath(input));
#ifdef _WIN32
 FILE *raw=_wfopen(path.c_str(),L"rb");
#else
 FILE *raw=std::fopen(path.c_str(),"rb");
#endif
 if(!raw)throw std::runtime_error(errno==ENOENT?"File not found":"Cannot read file");
 std::unique_ptr<FILE,decltype(&std::fclose)> file(raw,&std::fclose);
 auto read=[&](uint64_t off,size_t n){
  if(n>1048576||off>uint64_t(INT64_MAX))throw std::runtime_error("Metadata too large");
#ifdef _WIN32
  int seek=_fseeki64(raw,static_cast<int64_t>(off),SEEK_SET);
#else
  int seek=fseeko(raw,static_cast<off_t>(off),SEEK_SET);
#endif
  if(seek)throw std::runtime_error("Cannot seek file");
  std::string b(n,'\0');if(std::fread(b.data(),1,n,raw)!=n)throw std::runtime_error("Truncated file");return b;
 };
 auto h=read(0,8);
 if((h[0]!='I'||h[1]!='I')&&(h[0]!='M'||h[1]!='M')) {
  std::fseek(raw,0,SEEK_END);auto n=std::ftell(raw);if(n<0||n>1048576)throw std::runtime_error("Not TIFF or NSS JSON");auto b=read(0,size_t(n));
  if(b.size()>=3&&static_cast<unsigned char>(b[0])==239&&static_cast<unsigned char>(b[1])==187&&static_cast<unsigned char>(b[2])==191)b.erase(0,3);
  return b;
 }
 bool le=h[0]=='I';
 auto num=[&](const std::string &b,size_t i,int n)->uint64_t{uint64_t v=0;for(int c=0;c<n;c++)v|=uint64_t(uint8_t(b.at(i+c)))<<(8*(le?c:n-1-c));return v;};
 bool big=num(h,2,2)==43;uint64_t off=0;int countBytes=2,entryBytes=12,inlineBytes=4;
 if(big){h=read(0,16);if(num(h,4,2)!=8||num(h,6,2)!=0)throw std::runtime_error("Unsupported BigTIFF");off=num(h,8,8);countBytes=8;entryBytes=20;inlineBytes=8;}
 else{if(num(h,2,2)!=42)throw std::runtime_error("Unsupported TIFF");off=num(h,4,4);}
 auto count=num(read(off,countBytes),0,countBytes);if(count>4096)throw std::runtime_error("Invalid TIFF IFD");
 auto entries=read(off+countBytes,size_t(count*entryBytes));
 for(uint64_t i=0;i<count;i++){size_t p=size_t(i*entryBytes);if(num(entries,p,2)!=270)continue;
  if(num(entries,p+2,2)!=2)throw std::runtime_error("Bad NSS description");
  auto n=num(entries,p+4,big?8:4);if(n>1048576)throw std::runtime_error("Metadata too large");size_t at=p+(big?12:8);
  auto b=n<=uint64_t(inlineBytes)?entries.substr(at,size_t(n)):read(num(entries,at,inlineBytes),size_t(n));
  while(!b.empty()&&b.back()=='\0')b.pop_back();
  if(b.empty())throw std::runtime_error("No NSS info (old scan)");
  return b;
 }
 throw std::runtime_error("No NSS info (old scan)");
}
struct Metadata {
 std::array<float,3> base{};std::optional<std::array<int,3>> light;
 std::optional<std::array<float,9>> camera;float strength=1;bool cameraApplied=false,baseAfterProfile=false;
};
inline Metadata parseMetadata(const std::string &text) {
 JSON j;try{j=JSON::parse(text);}catch(...){throw std::runtime_error("Bad NSS JSON");}
 if(j.value("schema","")!="nss.scan/1")throw std::runtime_error("No NSS info (old scan)");
 if(j.value("encoding","")!="linear" || !j.value("processing","").empty())throw std::runtime_error("Not a linear NSS scan");
 Metadata m;
 auto baseSpace=j.value("film_base_space","");
 if(!baseSpace.empty()&&baseSpace!="after_profile")throw std::runtime_error("Unknown film base coordinate space");
 m.baseAfterProfile=baseSpace=="after_profile";
 if(!j.contains("film_base"))throw std::runtime_error("No film base in file");
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
 // v0.91 app leveling is already in camera-profile coordinates. Keep the
 // camera enabled for TIFF pixels, but never transform this baseline twice.
 Vec3 b={m.base[0],m.base[1],m.base[2]};
 if(!m.baseAfterProfile)b=p.cameraPixel(b);
 if(b.x<.0001f||b.y<.0001f||b.z<.0001f)throw std::runtime_error("Camera-corrected base is not positive");
 p.density.base_r=b.x;p.density.base_g=b.y;p.density.base_b=b.z;
}
}
