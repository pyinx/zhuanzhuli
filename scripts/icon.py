#!/usr/bin/env python3
"""Deterministic rounded release derivative; original artwork remains untouched."""
from PIL import Image, ImageDraw

def render(source, destination):
    scale, size, inset = 4, 1024, 51
    canvas = Image.new("RGBA", (size * scale, size * scale))
    width = (size - 2 * inset) * scale
    image = Image.open(source).convert("RGBA").resize((width, width), Image.Resampling.LANCZOS)
    mask = Image.new("L", (width, width))
    ImageDraw.Draw(mask).rounded_rectangle((0, 0, width - 1, width - 1), radius=width * .22, fill=255)
    image.putalpha(mask)
    canvas.paste(image, (inset * scale, inset * scale))
    canvas.resize((size, size), Image.Resampling.LANCZOS).save(destination)

def verify(path):
    image = Image.open(path).convert("RGBA")
    alpha = image.getchannel("A")
    w, h = image.size
    if any(alpha.getpixel(point) != 0 for point in ((0,0),(w-1,0),(0,h-1),(w-1,h-1))):
        raise ValueError("icon corners must be transparent")
    if alpha.getpixel((w//2,h//2)) != 255 or alpha.getbbox() is None:
        raise ValueError("icon center must remain opaque")
    return dict(size=[w,h], alphaBounds=list(alpha.getbbox()), cornersTransparent=True)
