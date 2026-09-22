from PIL import Image, ImageDraw
import math

SS = 4                      # supersample
W = 1024 * SS
def s(v): return int(round(v * SS))

# ---- palette (neutral graphite / slate) ----
TOP  = (69, 75, 84)         # #454B54
BOT  = (33, 37, 43)         # #21252B
GLY  = (247, 249, 251)      # near-white glyph
LENS = (30, 33, 39)         # dark lens glass
LENS_IN = (86, 93, 104)     # lens inner ring
SHADOW = (0, 0, 0)

img = Image.new("RGBA", (W, W), (0, 0, 0, 0))

# ---- tile geometry (macOS: rounded rect with padding + soft shadow) ----
TX0, TY0, TX1, TY1 = 112, 108, 912, 908       # 800x800 tile, sits a touch high
RAD = 182

# soft drop shadow under the tile
sh = Image.new("RGBA", (W, W), (0, 0, 0, 0))
sd = ImageDraw.Draw(sh)
sd.rounded_rectangle([s(TX0), s(TY0+26), s(TX1), s(TY1+26)], radius=s(RAD), fill=(0,0,0,150))
from PIL import ImageFilter
sh = sh.filter(ImageFilter.GaussianBlur(s(22)))
img.alpha_composite(sh)

# ---- gradient tile, masked to the rounded rect ----
grad = Image.new("RGB", (1, s(TY1-TY0)), TOP)
gp = grad.load()
h = grad.size[1]
for y in range(h):
    t = y / (h - 1)
    gp[0, y] = tuple(int(TOP[i] + (BOT[i]-TOP[i]) * t) for i in range(3))
grad = grad.resize((s(TX1-TX0), s(TY1-TY0)))

mask = Image.new("L", (W, W), 0)
md = ImageDraw.Draw(mask)
md.rounded_rectangle([s(TX0), s(TY0), s(TX1), s(TY1)], radius=s(RAD), fill=255)
tile = Image.new("RGBA", (W, W), (0,0,0,0))
tile.paste(grad, (s(TX0), s(TY0)))
img.paste(tile, (0,0), mask)

# faint top sheen
sheen = Image.new("RGBA", (W, W), (0,0,0,0))
shd = ImageDraw.Draw(sheen)
shd.ellipse([s(TX0-40), s(TY0-260), s(TX1+40), s(TY0+300)], fill=(255,255,255,26))
sheen = sheen.filter(ImageFilter.GaussianBlur(s(30)))
img.alpha_composite(Image.composite(sheen, Image.new("RGBA",(W,W),(0,0,0,0)), mask))

d = ImageDraw.Draw(img)

# ---- camera glyph (centered on tile) ----
OX, OY = 43, -20
def rr(x0,y0,x1,y1,r,fill): d.rounded_rectangle([s(x0+OX),s(y0+OY),s(x1+OX),s(y1+OY)], radius=s(r), fill=fill)
def circ(cx,cy,r,fill): d.ellipse([s(cx-r+OX),s(cy-r+OY),s(cx+r+OX),s(cy+r+OY)], fill=fill)

# mount arm + base plate
d.line([(s(330+OX),s(548+OY)),(s(304+OX),s(614+OY))], fill=GLY, width=s(30))
circ(330,548,15,GLY); circ(304,614,15,GLY)
rr(256, 604, 372, 632, 14, GLY)

# barrel + sun hood
rr(250, 452, 552, 548, 48, GLY)
rr(452, 434, 580, 468, 17, GLY)

# lens (concentric)
circ(548, 500, 31, LENS)
circ(548, 500, 18, LENS_IN)
circ(540, 491, 7, GLY)

# ---- signal arcs radiating from the lens ----
lcx, lcy = 548+OX, 500+OY
for r, a in [(80, 235), (112, 200), (144, 165)]:
    layer = Image.new("RGBA", (W, W), (0,0,0,0))
    ld = ImageDraw.Draw(layer)
    bbox = [s(lcx-r), s(lcy-r), s(lcx+r), s(lcy+r)]
    ld.arc(bbox, start=-52, end=52, fill=(247,249,251,a), width=s(15))
    img.alpha_composite(layer)

# ---- downscale ----
out = img.resize((1024,1024), Image.LANCZOS)
out.save("/home/claude/icon/icon_1024.png")
out.resize((128,128), Image.LANCZOS).save("/home/claude/icon/icon_128.png")
out.resize((64,64), Image.LANCZOS).save("/home/claude/icon/icon_64.png")
# side-by-side preview sheet
sheet = Image.new("RGBA", (1024+16+128, 1024), (245,245,247,255))
sheet.alpha_composite(out, (0,0))
sheet.alpha_composite(out.resize((128,128), Image.LANCZOS), (1024+16, 0))
sheet.alpha_composite(out.resize((64,64), Image.LANCZOS), (1024+16, 140))
sheet.alpha_composite(out.resize((32,32), Image.LANCZOS), (1024+16, 220))
sheet.convert("RGB").save("/home/claude/icon/preview.png")
print("done")
