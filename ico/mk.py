from PIL import Image, ImageDraw
S=1024
def draw(S=1024):
    im=Image.new("RGBA",(S,S),(0,0,0,0)); d=ImageDraw.Draw(im)
    k=S/1024
    # rounded app tile
    d.rounded_rectangle([0,0,S-1,S-1],radius=int(200*k),fill=(30,32,38,255))
    # film strip (tilted)
    st=Image.new("RGBA",(S,S),(0,0,0,0)); sd=ImageDraw.Draw(st)
    x0,x1=int(150*k),int(874*k)
    sd.rounded_rectangle([x0,int(-40*k),x1,int(1064*k)],radius=int(30*k),fill=(24,16,12,255))
    # sprocket holes both sides
    hh,hw,gap=int(72*k),int(54*k),int(120*k)
    y=int(-10*k)
    while y<S+60*k:
        for hx in (x0+int(42*k), x1-int(42*k)-hw):
            sd.rounded_rectangle([hx,y,hx+hw,y+hh],radius=int(14*k),fill=(236,232,222,255))
        y+=gap
    # frames with orange mask color (3 frames)
    fx0,fx1=x0+int(150*k),x1-int(150*k)
    fh=int(250*k); fg=int(30*k)
    ys=[int(100*k)+i*(fh+fg) for i in range(3)]
    cols=[(214,120,40,255),(226,138,52,255),(214,120,40,255)]
    for yy,c in zip(ys,cols):
        sd.rounded_rectangle([fx0,yy,fx1,yy+fh],radius=int(18*k),fill=c)
    # center frame gradient hint: highlight
    sd.rounded_rectangle([fx0+int(30*k),ys[1]+int(28*k),fx1-int(30*k),ys[1]+int(70*k)],radius=int(10*k),fill=(244,176,96,255))
    st=st.rotate(-14,resample=Image.BICUBIC,center=(S/2,S/2))
    # clip to tile
    mask=Image.new("L",(S,S),0); ImageDraw.Draw(mask).rounded_rectangle([0,0,S-1,S-1],radius=int(200*k),fill=255)
    im.paste(st,(0,0),Image.composite(st.split()[3],Image.new("L",(S,S),0),mask))
    return im
big=draw(1024)
big.save("icon1024.png")
sizes=[256,128,64,48,32,24,16]
imgs=[big.resize((s,s),Image.LANCZOS) for s in sizes]
imgs[0].save("app.ico",sizes=[(s,s) for s in sizes],append_images=imgs[1:])
