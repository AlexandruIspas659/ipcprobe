# Re-render the icon master and assemble a standard macOS .icns by hand (PNG-encoded chunks),
# with each size downscaled from the 4x master for clean antialiasing.
import struct, io, runpy, sys
from PIL import Image

# run draw.py's drawing to get the 4096 master `img`
ns = runpy.run_path("draw.py")
master = ns["img"]            # 4096 RGBA

def png_at(px):
    im = master.resize((px, px), Image.LANCZOS)
    b = io.BytesIO(); im.save(b, format="PNG"); return b.getvalue()

# (OSType, pixel size) — standard modern set incl. @2x retina variants
plan = [
    (b"icp4", 16), (b"icp5", 32),
    (b"ic07", 128), (b"ic08", 256), (b"ic09", 512), (b"ic10", 1024),
    (b"ic11", 32),  (b"ic12", 64),  (b"ic13", 256), (b"ic14", 512),
]
chunks = b""
for ostype, px in plan:
    data = png_at(px)
    chunks += ostype + struct.pack(">I", len(data) + 8) + data

icns = b"icns" + struct.pack(">I", len(chunks) + 8) + chunks
open("AppIcon.icns", "wb").write(icns)
print("AppIcon.icns", len(icns), "bytes,", len(plan), "sizes")

# also keep a clean 1024 master png as the editable source
master.resize((1024,1024), Image.LANCZOS).save("AppIcon.png")
