// render_math.h is embedded before this kernel by tools/compile_cuda.py.
struct NssBlock {float values[40];};
extern "C" __global__ void nss_render_cuda(const float *source,float *dest,NssBlock block,
 int sx1,int sy1,int sx2,int sy2,int sourceStride,int dx1,int dy1,int dx2,int dy2,int destStride,
 int wx1,int wy1,int width,int height,int premult){
 int ix=int(blockIdx.x*blockDim.x+threadIdx.x),iy=int(blockIdx.y*blockDim.y+threadIdx.y);
 if(ix>=width||iy>=height)return;int x=wx1+ix,y=wy1+iy;
 if(x<dx1||x>=dx2||y<dy1||y>=dy2)return;
 long long oi=(long long)(y-dy1)*destStride+(long long)(x-dx1)*4;
 if(x<sx1||x>=sx2||y<sy1||y>=sy2){for(int c=0;c<4;c++)dest[oi+c]=0.f;return;}
 long long si=(long long)(y-sy1)*sourceStride+(long long)(x-sx1)*4;
 float r=source[si],g=source[si+1],b=source[si+2],a=source[si+3];const float *p=block.values;
 if(premult&&a!=0.f){r/=a;g/=a;b/=a;}
 if(p[0]!=0.f){r=nss_di_decode(r);g=nss_di_decode(g);b=nss_di_decode(b);}
 if(p[27]!=0.f){float nr=r+p[37]*(p[28]*r+p[29]*g+p[30]*b-r),ng=g+p[37]*(p[31]*r+p[32]*g+p[33]*b-g),nb=b+p[37]*(p[34]*r+p[35]*g+p[36]*b-b);r=nr;g=ng;b=nb;}
 r=nss_channel(r,p,0);g=nss_channel(g,p,1);b=nss_channel(b,p,2);
 if(premult){r*=a;g*=a;b*=a;}
 dest[oi]=r;dest[oi+1]=g;dest[oi+2]=b;dest[oi+3]=a;
}
