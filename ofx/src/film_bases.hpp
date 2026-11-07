// Generated from docs/data/ofx_film_bases.json. One measured Base per stock.
#pragma once
namespace nss {
struct FilmBase {const char *label;float r,g,b;};
inline constexpr FilmBase filmBases[]={{"None",0.f,0.f,0.f},{"500T Base",1.4f,0.0f,0.8f}};
inline FilmBase filmBaseGains(int index){return index>0&&index<int(sizeof(filmBases)/sizeof(filmBases[0]))?filmBases[index]:filmBases[0];}
}
