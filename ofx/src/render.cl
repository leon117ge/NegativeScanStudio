// The build embeds render_math.h before this source.
__kernel void nss_render(__global const float *source,__global float *dest,__constant const float *p,
 int sx1,int sy1,int sx2,int sy2,int sourceStride,int dx1,int dy1,int dx2,int dy2,int destStride,
 int wx1,int wy1,int width,int height,int premult){
 int x=wx1+(int)get_global_id(0),y=wy1+(int)get_global_id(1);
 if(x>=wx1+width||y>=wy1+height||x<dx1||x>=dx2||y<dy1||y>=dy2)return;
 int oi=(y-dy1)*destStride+(x-dx1)*4;
 if(x<sx1||x>=sx2||y<sy1||y>=sy2){vstore4((float4)(0),0,dest+oi);return;}
 float4 rgba=vload4(0,source+(y-sy1)*sourceStride+(x-sx1)*4);float3 v=rgba.xyz;
 if(premult&&rgba.w!=0.f)v/=rgba.w;
 if(p[0]!=0.f)v=(float3)(nss_di_decode(v.x),nss_di_decode(v.y),nss_di_decode(v.z));
 if(p[27]!=0.f)v=(float3)(v.x+p[37]*(p[28]*v.x+p[29]*v.y+p[30]*v.z-v.x),v.y+p[37]*(p[31]*v.x+p[32]*v.y+p[33]*v.z-v.y),v.z+p[37]*(p[34]*v.x+p[35]*v.y+p[36]*v.z-v.z));
 if(!nss_tone_active(p))v=(float3)(nss_channel(v.x,p,0),nss_channel(v.y,p,1),nss_channel(v.z,p,2));
 else{v=(float3)(nss_linear(nss_code(v.x,p,0),p,0),nss_linear(nss_code(v.y,p,1),p,1),nss_linear(nss_code(v.z,p,2),p,2));
  v*=nss_shoulder_gain(nss_tone_norm(v.x,v.y,v.z),p);float t=nss_toe_offset(nss_tone_min(v.x,v.y,v.z),p);
  float r=v.x+t,g=v.y+t,b=v.z+t;if(p[48]!=0.f)nss_tone_color(&r,&g,&b,p);
  v=(float3)(nss_encode(r,p),nss_encode(g,p),nss_encode(b,p));}
 if(premult)v*=rgba.w;
 vstore4((float4)(v,rgba.w),0,dest+oi);
}
