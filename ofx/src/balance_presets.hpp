// User-saved Color balance presets. Shared across Resolve projects via a JSON file.
#pragma once
#include "../vendor/json/json.hpp"
#include <array>
#include <cmath>
#include <cstdlib>
#include <filesystem>
#include <fstream>
#include <mutex>
#include <string>

namespace nss {

constexpr int kBalancePresetSlots=8;
constexpr int kBalancePresetNameMax=32;

struct BalancePreset {
 std::string name;
 double r=0,g=0,b=0;
 bool empty() const {return name.empty();}
};

struct BalancePresetsFile {
 std::array<BalancePreset,kBalancePresetSlots> slots{};
 bool valid=true;
};

inline std::mutex &balancePresetsMutex(){static std::mutex m;return m;}

inline std::string trimAscii(const std::string &s){
 size_t a=0,b=s.size();
 while(a<b&&(s[a]==' '||s[a]=='\t'||s[a]=='\n'||s[a]=='\r'))++a;
 while(b>a&&(s[b-1]==' '||s[b-1]=='\t'||s[b-1]=='\n'||s[b-1]=='\r'))--b;
 return s.substr(a,b-a);
}

inline bool validPresetName(const std::string &name,std::string *why=nullptr){
 if(name.empty()){if(why)*why="Enter a preset name";return false;}
 if(name.size()>size_t(kBalancePresetNameMax)){if(why)*why="Name is too long";return false;}
 for(unsigned char c:name){
  if(c<32||c>126){if(why)*why="Use English (ASCII) names only";return false;}
 }
 return true;
}

inline bool inBalanceRange(double v){return std::isfinite(v)&&v>=-5&&v<=5;}

inline std::filesystem::path balancePresetsPath(){
 if(const char *env=std::getenv("NSS_BALANCE_PRESETS");env&&*env)return std::filesystem::u8path(env);
#ifdef _WIN32
 if(const char *appdata=std::getenv("APPDATA");appdata&&*appdata)
  return std::filesystem::u8path(appdata)/"NegativeScanStudio"/"balance_presets.json";
#endif
 if(const char *home=std::getenv("HOME");home&&*home)
  return std::filesystem::u8path(home)/".config"/"NegativeScanStudio"/"balance_presets.json";
 return std::filesystem::u8path("balance_presets.json");
}

inline std::string slotLabel(int slot,const BalancePreset &p){
 auto label="Slot "+std::to_string(slot);
 if(!p.empty())label+=": "+p.name;
 return label;
}

inline std::string presetsListStatus(const BalancePresetsFile &file){
 if(!file.valid)return "Presets file was invalid; treated as empty.";
 std::string out;
 for(int i=0;i<kBalancePresetSlots;i++)if(!file.slots[i].empty()){
  if(!out.empty())out+="  ";
  out+=std::to_string(i+1)+": "+file.slots[i].name;
 }
 return out.empty()?"No saved presets":out;
}

inline int findPresetName(const BalancePresetsFile &file,const std::string &name){
 for(int i=0;i<kBalancePresetSlots;i++)if(file.slots[i].name==name)return i;
 return -1;
}

inline int firstEmptySlot(const BalancePresetsFile &file){
 for(int i=0;i<kBalancePresetSlots;i++)if(file.slots[i].empty())return i;
 return -1;
}

inline bool parsePresetObject(const nlohmann::json &item,BalancePreset &out){
 if(!item.is_object()||!item.contains("name")||!item["name"].is_string())return false;
 auto name=trimAscii(item["name"].get<std::string>());
 std::string why;
 if(!validPresetName(name,&why))return false;
 if(!item.contains("r")||!item.contains("g")||!item.contains("b"))return false;
 if(!item["r"].is_number()||!item["g"].is_number()||!item["b"].is_number())return false;
 double r=item["r"].get<double>(),g=item["g"].get<double>(),b=item["b"].get<double>();
 if(!inBalanceRange(r)||!inBalanceRange(g)||!inBalanceRange(b))return false;
 out={name,r,g,b};return true;
}

inline void placePreset(BalancePresetsFile &file,const BalancePreset &preset,int preferred){
 int existing=findPresetName(file,preset.name);
 int slot=existing>=0?existing:preferred;
 if(slot<0||slot>=kBalancePresetSlots||(!file.slots[slot].empty()&&file.slots[slot].name!=preset.name))
  slot=firstEmptySlot(file);
 if(slot<0)return;
 file.slots[slot]=preset;
}

inline void takePresetsList(BalancePresetsFile &file,const nlohmann::json &list,bool indexIsSlot){
 for(size_t i=0;i<list.size();i++){
  auto &item=list[i];
  if(item.is_null())continue;
  BalancePreset preset;
  if(!parsePresetObject(item,preset))continue;
  int preferred=indexIsSlot&&i<size_t(kBalancePresetSlots)?int(i):firstEmptySlot(file);
  if(item.contains("slot")&&item["slot"].is_number_integer()){
   int s=item["slot"].get<int>();
   if(s>=1&&s<=kBalancePresetSlots)preferred=s-1;
  }
  placePreset(file,preset,preferred);
 }
}

inline BalancePresetsFile loadBalancePresets(){
 std::lock_guard<std::mutex> lock(balancePresetsMutex());
 BalancePresetsFile file;
 auto path=balancePresetsPath();
 std::error_code error;
 if(!std::filesystem::exists(path,error)||error)return file;
 std::ifstream in(path);
 if(!in){file.valid=false;return file;}
 try{
  auto json=nlohmann::json::parse(in);
  if(json.is_array())takePresetsList(file,json,true);
  else if(json.is_object()&&json.contains("slots")&&json["slots"].is_array())takePresetsList(file,json["slots"],true);
  else if(json.is_object()&&json.contains("presets")&&json["presets"].is_array())takePresetsList(file,json["presets"],false);
  else file.valid=false;
 }catch(...){file=BalancePresetsFile{};file.valid=false;}
 return file;
}

inline bool saveBalancePresets(const BalancePresetsFile &file){
 std::lock_guard<std::mutex> lock(balancePresetsMutex());
 auto path=balancePresetsPath();
 std::error_code error;
 if(path.has_parent_path())std::filesystem::create_directories(path.parent_path(),error);
 auto tmp=path;tmp+=".tmp";
 nlohmann::json json=nlohmann::json::array();
 for(int i=0;i<kBalancePresetSlots;i++){
  if(file.slots[i].empty())json.push_back(nullptr);
  else json.push_back({{"slot",i+1},{"name",file.slots[i].name},{"r",file.slots[i].r},{"g",file.slots[i].g},{"b",file.slots[i].b}});
 }
 {
  std::ofstream out(tmp,std::ios::binary|std::ios::trunc);
  if(!out)return false;
  out<<json.dump(2)<<'\n';
  if(!out)return false;
 }
 std::filesystem::rename(tmp,path,error);
 if(error){
  std::filesystem::remove(path,error);
  std::filesystem::rename(tmp,path,error);
  if(error){std::filesystem::remove(tmp,error);return false;}
 }
 return true;
}

}
