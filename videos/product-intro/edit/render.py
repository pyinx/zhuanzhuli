"""Compose real recordings with a caption canvas; never recreate product UI."""
import hashlib
import json
from pathlib import Path
import subprocess

from PIL import Image, ImageDraw, ImageFont

BASE = Path(__file__).resolve().parents[1]
PROJECT = BASE.parents[1]
EDIT = BASE / "edit"
WIDTH, HEIGHT = 1920, 1080
PAPER = "#f5f5f0"
INK = "#20251f"
MUTED = "#6b7268"
RED = "#e83147"
FONT = next(Path("/System/Library/AssetsV2").rglob("PingFang.ttc"))
ICON = PROJECT / ".build/归径.app/Contents/Resources/AppIcon.icns"
MUSIC = PROJECT / ".harness/view/background-sound-light/audio/morning.mp3"


def run(args):
    subprocess.run(args, check=True)


def font(size, weight=3):
    return ImageFont.truetype(str(FONT), size, index=weight)


def text(draw, position, value, size, fill=INK, weight=3, spacing=18):
    draw.multiline_text(position, value, font=font(size, weight), fill=fill, spacing=spacing)


def canvas(shot):
    image = Image.new("RGB", (WIDTH, HEIGHT), PAPER)
    d = ImageDraw.Draw(image)
    icon = Image.open(ICON).convert("RGBA")
    if "card" in shot:
        icon.thumbnail((340, 340))
        image.paste(icon, (1400, 345), icon)
        text(d, (130, 140), "macOS 专注工具", 32, MUTED)
        text(d, (125, 270), "归径", 106, weight=11)
        title = "回到正在做的\n那一件事。" if shot["card"] == "intro" else "为当下这一件事，\n留一片安静。"
        text(d, (130, 470), title, 72, weight=7, spacing=25)
        d.rectangle((134, 777, 218, 782), fill=RED)
        if shot["card"] == "intro":
            text(d, (135, 825), "设定目标   ·   选择环境   ·   开始专注", 30, MUTED)
        else:
            text(d, (135, 830), "Music: Morning — Kevin MacLeod (incompetech.com) · CC BY 4.0", 23, MUTED)
            text(d, (135, 871), "creativecommons.org/licenses/by/4.0/ · Excerpt, volume & fades adjusted", 22, MUTED)
            text(d, (135, 912), "Forest footage: Joshua Woroniecki / Pexels · Full credits: CREDITS.md", 22, MUTED)
    else:
        icon.thumbnail((64, 64))
        image.paste(icon, (75, 46), icon)
        text(d, (157, 51), "归径", 38, weight=7)
        text(d, (283, 62), "产品介绍", 24, MUTED)
        text(d, (80, 244), shot["chapter"], 27, RED, weight=7)
        text(d, (75, 326), shot["title"], 61, weight=7, spacing=20)
        text(d, (80, 550), shot["body"], 29, MUTED, spacing=18)
        d.rounded_rectangle((590, 125, 1860, 960), radius=20, fill="#ffffff", outline="#e3e6dd", width=2)
        d.rectangle((80, 880, 137, 884), fill=RED)
        text(d, (80, 906), "实机演示 · 画面经剪辑", 23, MUTED)
    caption = shot["subtitle"]
    caption_font = font(34, 7)
    box = d.textbbox((0, 0), caption, font=caption_font)
    d.text(((WIDTH - (box[2]-box[0])) / 2, 1004), caption, font=caption_font, fill=INK)
    return image


def stamp(seconds):
    total = round(seconds * 1000)
    return f"{total//3600000:02}:{total//60000%60:02}:{total//1000%60:02},{total%1000:03}"


def main():
    shots = json.loads((EDIT / "shots.json").read_text())
    outputs = []
    manifest = []
    subtitles = []
    total = 0
    for index, shot in enumerate(shots):
        stage = EDIT / (shot["id"] + ".png")
        canvas(shot).save(stage)
        target = EDIT / (shot["id"] + ".mp4")
        args = ["ffmpeg", "-hide_banner", "-loglevel", "error", "-y", "-loop", "1", "-framerate", "30", "-i", str(stage)]
        record = dict(shot, at=total, take=shot.get("take", "first-take"))
        if "source" in shot:
            source = BASE / "recordings" / shot["source"]
            if not source.is_file():
                raise RuntimeError("Missing actual recording: " + str(source))
            args += ["-ss", str(shot["start"]), "-t", str(shot["sourceDuration"]), "-i", str(source)]
            x,y,w,h = shot["crop"]
            scale = min(1230/w,795/h)
            sw, sh = int(w*scale)//2*2, int(h*scale)//2*2
            px,py = 610+(1230-sw)//2,145+(795-sh)//2
            speed = shot["sourceDuration"]/shot["duration"]
            exposure = ",lutrgb=r=val*2:g=val*2:b=val*2" if shot.get("exposureGain") == 2 else ""
            vf = f"[1:v]crop={w}:{h}:{x}:{y}{exposure},setpts=(PTS-STARTPTS)/{speed},fps=30,scale={sw}:{sh}:flags=lanczos,setsar=1[v];[0:v][v]overlay={px}:{py}:shortest=1"
            record.update(sourceSHA256=hashlib.sha256(source.read_bytes()).hexdigest(), speed=speed)
        else:
            vf = "[0:v]null"
        vf += f",fade=t=in:d=0.18:color=0xf5f5f0,fade=t=out:st={shot['duration']-.18}:d=0.18:color=0xf5f5f0,format=yuv420p[out]"
        args += ["-filter_complex", vf, "-map", "[out]", "-t", str(shot["duration"]), "-an", "-c:v", "libx264", "-preset", "fast", "-crf", "20", "-threads", "4", "-r", "30", "-movflags", "+faststart", str(target)]
        run(args)
        outputs.append(target)
        manifest.append(record)
        subtitles.append(f"{index+1}\n{stamp(total+.15)} --> {stamp(total+shot['duration']-.15)}\n{shot['subtitle']}\n")
        total += shot["duration"]
        print(f"Rendered {shot['id']} ({shot['duration']}s)", flush=True)
    (BASE / "guijing-intro.srt").write_text("\n".join(subtitles), encoding="utf-8")
    listing = EDIT / "concat.txt"
    listing.write_text("\n".join("file '" + p.name + "'" for p in outputs))
    target = BASE / "guijing-intro.mp4"
    audio_filter = f"loudnorm=I=-24:TP=-3:LRA=7,afade=t=in:d=2,afade=t=out:st={total-3}:d=3"
    run(["ffmpeg","-hide_banner","-loglevel","error","-y","-f","concat","-safe","0","-i",str(listing),"-i",str(MUSIC),"-map","0:v:0","-map","1:a:0","-c:v","copy","-af",audio_filter,"-c:a","aac","-b:a","128k","-t",str(total),"-movflags","+faststart",str(target)])
    canvas(shots[0]).save(BASE / "poster.jpg", quality=95)
    bindings = {str(p.relative_to(PROJECT)): hashlib.sha256(p.read_bytes()).hexdigest() for p in [MUSIC,ICON,EDIT/"shots.json",Path(__file__)]}
    (BASE / "evidence/timeline.json").write_text(json.dumps(dict(duration=total,shots=manifest,bindings=bindings,sequenceNote="多次真实演示剪辑，不是一次无剪辑会话；结束、统计和历史来自同一次结束动作补录，第一轮的准备、工作与暂停继续片段保留实际时间。"),ensure_ascii=False,indent=2))
    print(f"Final video: {total}s, {target.stat().st_size/1024/1024:.1f} MiB", flush=True)


if __name__ == "__main__":
    main()
