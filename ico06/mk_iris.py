import math
from PIL import Image, ImageDraw, ImageFilter, ImageChops
N=7
def lerp(a,b,t): return tuple(int(a[i]+(b[i]-a[i])*t) for i in range(len(a)))
def render(S, small=False):
    SS=S*8 if S<128 else 1024*2
    k=SS/1024
    im=Image.new("RGBA",(SS,SS),(0,0,0,0))
    # badge: rounded square, vertical gradient
    pad=0 if small else int(24*k)
    rad=int(220*k)
    grad=Image.new("RGBA",(SS,SS))
    gd=ImageDraw.Draw(grad)
    for y in range(SS):
        t=y/SS
        gd.line([(0,y),(SS,y)],fill=lerp((52,52,58,255),(22,22,25,255),t))
    mask=Image.new("L",(SS,SS),0); ImageDraw.Draw(mask).rounded_rectangle([pad,pad,SS-1-pad,SS-1-pad],radius=rad,fill=255)
    im.paste(grad,(0,0),mask)
    d=ImageDraw.Draw(im)
    if not small:
        d.rounded_rectangle([pad,pad,SS-1-pad,SS-1-pad],radius=rad,outline=(80,80,88,255),width=int(6*k))
    cx=cy=SS/2
    R=(400 if small else 372)*k     # outer blade radius
    r=(150 if small else 140)*k     # opening radius
    rot=-math.pi/2+0.35
    P=[(cx+r*math.cos(rot+2*math.pi*i/N), cy+r*math.sin(rot+2*math.pi*i/N)) for i in range(N)]
    def hit(a,b):
        # extend ray a->b to outer circle
        dx,dy=b[0]-a[0],b[1]-a[1]; L=math.hypot(dx,dy); dx/=L; dy/=L
        fx,fy=a[0]-cx,a[1]-cy
        bb=fx*dx+fy*dy; c=fx*fx+fy*fy-R*R
        t=-bb+math.sqrt(bb*bb-c)
        return (a[0]+dx*t,a[1]+dy*t)
    Q=[hit(P[i],P[(i+1)%N]) for i in range(N)]
    # blade layer with radial-ish amber gradient
    blade=Image.new("RGBA",(SS,SS),(0,0,0,0)); bd=ImageDraw.Draw(blade)
    for i in range(N):
        q0=Q[i]; q1=Q[(i+1)%N]
        a0=math.atan2(q0[1]-cy,q0[0]-cx); a1=math.atan2(q1[1]-cy,q1[0]-cx)
        while a1<a0: a1+=2*math.pi
        arc=[(cx+R*math.cos(a0+(a1-a0)*j/40), cy+R*math.sin(a0+(a1-a0)*j/40)) for j in range(41)]
        poly=[P[(i+1)%N]]+arc
        # per-blade tone: lit from top-left
        mid=(a0+a1)/2
        light=0.5+0.5*math.cos(mid-(-2.3))
        col=lerp((176,72,18,255),(255,178,72,255),light)
        bd.polygon(poly,fill=col)
    # soft overall gradient (orange mask feel): darken toward bottom-right
    shade=Image.new("L",(SS,SS),0); sd=ImageDraw.Draw(shade)
    for y in range(SS):
        sd.line([(0,y),(SS,y)],fill=int(70*y/SS))
    dark=Image.new("RGBA",(SS,SS),(60,20,0,255))
    blade=Image.composite(Image.blend(blade,dark,0.0),blade,Image.new("L",(SS,SS),0))
    bl_rgb=blade.convert("RGB"); bl_a=blade.getchannel("A")
    bl_rgb=Image.composite(Image.new("RGB",(SS,SS),(70,25,0)),bl_rgb,shade)
    blade=Image.merge("RGBA",(*bl_rgb.split(),bl_a))
    # gaps between blades (bg colour lines along blade edges)
    gw=int((34 if small else 16)*k)
    gap=Image.new("L",(SS,SS),255); gdd=ImageDraw.Draw(gap)
    for i in range(N):
        gdd.line([P[i],Q[i]],fill=0,width=gw)
    a=ImageChops.multiply(blade.getchannel("A"),gap)
    blade.putalpha(a)
    # clip blades to the outer circle
    cm=Image.new("L",(SS,SS),0); ImageDraw.Draw(cm).ellipse([cx-R,cy-R,cx+R,cy+R],fill=255)
    blade.putalpha(ImageChops.multiply(blade.getchannel("A"),cm))
    # outer ring
    ring_w=(40 if small else 26)*k
    d.ellipse([cx-R-ring_w,cy-R-ring_w,cx+R+ring_w,cy+R+ring_w],fill=(14,14,16,255))
    if not small:
        d.ellipse([cx-R-ring_w,cy-R-ring_w,cx+R+ring_w,cy+R+ring_w],outline=(95,95,104,255),width=int(7*k))
    im.alpha_composite(blade)
    # opening: dark with small highlight
    d=ImageDraw.Draw(im)
    d.polygon(P,fill=(12,12,14,255))
    if not small:
        hl=Image.new("RGBA",(SS,SS),(0,0,0,0)); hd=ImageDraw.Draw(hl)
        hd.ellipse([cx-r*0.55,cy-r*0.62,cx-r*0.05,cy-r*0.22],fill=(255,255,255,46))
        hl=hl.filter(ImageFilter.GaussianBlur(10*k))
        im.alpha_composite(hl)
        # glossy highlight on upper blades
        gl=Image.new("RGBA",(SS,SS),(0,0,0,0)); gd2=ImageDraw.Draw(gl)
        gd2.ellipse([cx-R*1.1,cy-R*1.55,cx+R*1.1,cy-R*0.05],fill=(255,255,255,26))
        gl.putalpha(ImageChops.multiply(gl.getchannel("A"),blade.getchannel("A")))
        im.alpha_composite(gl)
    return im.resize((S,S),Image.LANCZOS)
sizes=[16,20,24,32,40,48,64,256]
imgs={}
for s in sizes:
    imgs[s]=render(s, small=(s<=24))
    imgs[s].save(f"./ico06/icon_{s}.png")
big=render(1024); big.save("./ico06/icon_1024.png")
imgs[256].save("./ico06/app.ico",sizes=[(s,s) for s in sizes],append_images=[imgs[s] for s in sizes if s!=256])
# preview sheet
W=40+sum(max(s,32)+24 for s in sizes[:-1])
sheet=Image.new("RGBA",(W,2*110),(0,0,0,255))
x=20
d=ImageDraw.Draw(sheet)
for bgc,y0 in (((31,31,34,255),0),((236,236,236,255),110)):
    d.rectangle([0,y0,W,y0+110],fill=bgc)
for s in sizes[:-1]:
    for y0 in (0,110):
        sheet.alpha_composite(imgs[s],(x,y0+55-s//2))
    d.text((x,y0+95-110+110 if False else 92),f"{s}",fill=(200,200,200,255))
    d.text((x,202),f"{s}",fill=(60,60,60,255))
    x+=max(s,32)+24
sheet=sheet.resize((sheet.width*2,sheet.height*2),Image.NEAREST)
sheet.save("./ico06/preview_small_sizes.png")
