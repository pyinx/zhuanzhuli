"""Technical verification; interpretation and visual quality need independent review."""
import hashlib
import json
from pathlib import Path
import re
import struct
import subprocess

from PIL import Image

BASE = Path(__file__).resolve().parent
PROJECT = BASE.parents[1]


def require(condition, message):
    if not condition:
        raise RuntimeError(message)


def digest(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()


def probe(path):
    result = subprocess.run(["ffprobe","-v","error","-show_format","-show_streams","-of","json",str(path)],check=True,capture_output=True,text=True)
    return json.loads(result.stdout)


def timestamp(value):
    h,m,s,ms = map(int,re.split("[:,]",value))
    return h*3600+m*60+s+ms/1000


def check():
    video = BASE / "guijing-intro.mp4"
    metadata = probe(video)
    streams = metadata["streams"]
    vs = [s for s in streams if s["codec_type"] == "video"]
    au = [s for s in streams if s["codec_type"] == "audio"]
    require(len(vs)==1 and len(au)==1,"Expected exactly one video and one audio stream")
    v = vs[0]
    duration = float(metadata["format"]["duration"])
    require(60 <= duration <= 80,"Video duration outside agreed range")
    require((v["width"],v["height"])==(1920,1080),"Video is not 1080p")
    require(v["codec_name"]=="h264" and v["pix_fmt"]=="yuv420p","Unexpected video format")
    require(v["r_frame_rate"]=="30/1" and v["avg_frame_rate"]=="30/1","Video is not constant 30 fps")
    require(au[0]["codec_name"]=="aac","Audio is not AAC")
    boxes = []
    with video.open("rb") as f:
        while True:
            header = f.read(8)
            if not header:
                break
            require(len(header)==8,"Truncated MP4 box")
            size,kind = struct.unpack(">I4s",header)
            extra = 8
            if size==1:
                size = struct.unpack(">Q",f.read(8))[0]
                extra += 8
            boxes.append(kind.decode("ascii"))
            if size==0:
                break
            require(size>=extra,"Invalid MP4 box")
            f.seek(size-extra,1)
    require("moov" in boxes and "mdat" in boxes and boxes.index("moov")<boxes.index("mdat"),"MP4 lacks faststart")
    subprocess.run(["ffmpeg","-v","error","-xerror","-i",str(video),"-map","0:v:0","-map","0:a:0","-f","null","-"],check=True,capture_output=True)
    levels = subprocess.run(["ffmpeg","-hide_banner","-nostats","-i",str(video),"-vn","-af","volumedetect","-f","null","-"],check=True,capture_output=True,text=True).stderr
    maximum = float(re.search(r"max_volume: ([-\w.]+) dB",levels).group(1))
    mean = float(re.search(r"mean_volume: ([-\w.]+) dB",levels).group(1))
    require(-50 < mean < -12 and maximum <= -.5,"Audio silent, too loud or clipped")
    with Image.open(BASE/"poster.jpg") as image:
        require(image.size==(1920,1080),"Poster is not 1080p")
        image.verify()

    timeline = json.loads((BASE/"evidence/timeline.json").read_text())
    require(abs(timeline["duration"]-duration)<.08,"Timeline and MP4 duration differ")
    ids = {s["id"] for s in timeline["shots"]}
    require({"goal","apps","scene","sound","start","work","pause","resume-prepare","resume-active","ended","template","stats","history"} <= ids,"A required demo chapter is missing")
    checked_sources = {}
    for shot in timeline["shots"]:
        if "source" not in shot:
            continue
        source = BASE/"recordings"/shot["source"]
        if source.name not in checked_sources:
            checked_sources[source.name] = dict(sha256=digest(source),duration=float(probe(source)["format"]["duration"]))
        observed = checked_sources[source.name]
        require(observed["sha256"]==shot["sourceSHA256"],"Recording hash changed: "+source.name)
        require(0<=shot["start"] and shot["start"]+shot["sourceDuration"]<=observed["duration"]+.03,"Cut outside original recording")
    for name,sha in timeline["bindings"].items():
        require(digest(PROJECT/name)==sha,"Render dependency changed: "+name)
    blocks = (BASE/"guijing-intro.srt").read_text().strip().split("\n\n")
    require(len(blocks)==len(timeline["shots"]),"Subtitle count differs from timeline")
    last = 0
    for block,shot in zip(blocks,timeline["shots"]):
        lines = block.splitlines()
        start,end = map(timestamp,lines[1].split(" --> "))
        require(last<=start<end<=duration,"Overlapping or out-of-range subtitle")
        require("\n".join(lines[2:])==shot["subtitle"],"Subtitle differs from burned-in canvas")
        last=end

    for name in ["script.md","CREDITS.md","README.md"]:
        require((BASE/name).is_file() and (BASE/name).stat().st_size>150,"Missing delivery file: "+name)
    credit = (BASE/"CREDITS.md").read_text()
    for phrase in ["Morning","Kevin MacLeod","creativecommons.org/licenses/by/4.0/","Joshua Woroniecki","Pexels"]:
        require(phrase in credit,"Missing required attribution: "+phrase)
    first_take = json.loads((BASE/"evidence/demo-data.json").read_text())
    require(first_take.get("current") is None and len(first_take["history"])==1,"First demo session snapshot is invalid")
    data = json.loads((BASE/"evidence/retake-data.json").read_text())
    require(data.get("current") is None,"Demo session still active")
    require(data["preferences"]["focusLinked"] is False,"System Focus linkage was enabled")
    require(len(data["history"])==1 and data["history"][0]["goal"]=="写完方案第一版","Unexpected demo history")
    history = data["history"][0]
    require(history["phase"]=="ended" and 60 < history["elapsed"]/1e9 < 180,"History is not a real short demo session")
    for prefix in ["demo","retake"]:
        journal = json.loads((BASE/f"evidence/{prefix}-environment.json").read_text())
        require(all(e["status"] in ["released","abandoned"] for e in journal["entries"]),"Unresolved recording environment lease")
    take_bindings = {s["id"]:s.get("take") for s in timeline["shots"]}
    require(all(take_bindings.get(name)=="end-retake" for name in ["ended","stats","history"]),"End action and statistics must use the same documented retake")
    prior = json.loads((PROJECT/".harness/changes/2026-11-18-product-intro-video/pre-video-worktree.json").read_text())
    external = json.loads((BASE/"evidence/external-changes.json").read_text())
    for name,sha in prior["sha256"].items():
        actual = digest(PROJECT/name)
        if actual != sha:
            observed = external.get(name,{})
            require(observed.get("before")==sha and observed.get("after")==actual,
                    "Unaccounted change to pre-existing work: "+name)
    session = json.loads((BASE/"evidence/session.json").read_text())
    require(digest(Path(session["sourceApp"])/"Contents/MacOS/Focus")==session["sourceExecutableSHA256"],"Original product binary changed")

    files = ["guijing-intro.mp4","poster.jpg","guijing-intro.srt","script.md","CREDITS.md","README.md"]
    report = dict(technicalStatus="pass",duration=duration,width=1920,height=1080,fps=30,
                  maxVolumeDB=maximum,meanVolumeDB=mean,fullDecode=True,faststart=True,
                  sourceRecordings=checked_sources,demoElapsedSeconds=history["elapsed"]/1e9,
                  externalChangesPreserved=external,
                  files={name:dict(bytes=(BASE/name).stat().st_size,sha256=digest(BASE/name)) for name in files},
                  review="Visual interpretation, privacy and usefulness require independent review; this command does not grade them.")
    (BASE/"evidence/verification.json").write_text(json.dumps(report,ensure_ascii=False,indent=2))
    print(f"PASS: {duration:.2f}s, 1920x1080 30fps H.264/AAC, faststart, full decode")
    print(f"PASS: subtitles, source hashes, real demo history, worktree attribution; audio peak {maximum}dB")
    print("Visual and feature claims: independent review required")


if __name__ == "__main__":
    check()
