#!/usr/bin/env python3
"""Build the ten-second scene entry movies.

Stage 1 asks MiniMax-H3 for the two motion clips (first frame plus last
frame, so the character stays the character of the artwork). Stage 2 assembles
those clips, the artwork keyframes and the local soundtrack into the final
movie. Stage 3 measures the result against the acceptance criteria.

Stage 2 never touches the network: it only reads assets/artwork/v1 and
assets/media/v2/clips, so the pipeline can be re-run from stored clips.
"""
import argparse, hashlib, json, math, os, pathlib, shutil, subprocess, sys

import numpy as np
from PIL import Image

ROOT = pathlib.Path(__file__).resolve().parents[1]
ART = ROOT / "assets/artwork/v1"
V2 = ROOT / "assets/media/v2"
CLIPS = V2 / "clips"
BUILD = ROOT / ".build/scene-animation"

FPS = 24
WIDTH, HEIGHT = 1920, 1248
APPROACH_END = 4.6
SETTLE_END = 8.2
TOTAL = 10.0
JUNCTION_FADE = 0.25

SCENES = ("forest", "beach", "meadow", "snow")

SCENE_PLACE = {
    "forest": "a sunlit forest path with mossy boulders, ferns and tall straight trees, "
              "soft morning light filtering through the canopy",
    "beach": "a quiet sandy cove with turquoise sea and low waves, dune grass and rocky "
             "headlands, bright clear daylight",
    "meadow": "a rolling green meadow with wildflowers and scattered boulders, a dirt "
              "trail, warm late afternoon light",
    "snow": "a snowy mountain valley with snow-laden pines and a distant frozen lake, "
            "low golden sunlight",
}


def prompts(scene):
    """The two prompts of one scene, both keeping the camera still."""
    place = SCENE_PLACE[scene]
    approach = (
        "A young woman with long brown hair, wearing a sage-green pinafore dress over a "
        "cream blouse and brown ankle boots, walks slowly forward along " + place + ", "
        "growing larger in frame as she comes closer. Her hair and the hem of her dress sway "
        "gently with each step. The camera stays completely still and the surroundings do not "
        "change. Calm meditative atmosphere, cinematic natural color, photorealistic, smooth "
        "continuous motion.")
    settle = (
        "The same young woman in the sage-green pinafore dress over a cream blouse takes one "
        "more step forward on " + place + ", then lowers herself and sits cross-legged on the "
        "ground. She settles her hands on her knees, closes her eyes and breathes calmly, her "
        "long brown hair resting on her shoulders. The camera stays completely still and the "
        "surroundings do not change. Soft light, calm meditative atmosphere, photorealistic, "
        "smooth continuous motion.")
    return approach, settle


MODEL = "MiniMax-H3"
RESOLUTION = "2K"


def run(args, **kw):
    subprocess.run(args, check=True, **kw)


def sha256(path):
    digest = hashlib.sha256()
    with open(path, "rb") as handle:
        for block in iter(lambda: handle.read(1 << 20), b""):
            digest.update(block)
    return digest.hexdigest()


def scene_paths(scene):
    """The five artwork keyframes of one scene."""
    return {name: ART / scene / (name + ".png")
            for name in ("00-clean", "01-distant", "02-near", "03-seated", "04-dissolve")}


def scaled(path, width=WIDTH, height=HEIGHT, scale=1):
    """Load an artwork frame at output size, centered crop, no stretch."""
    image = Image.open(path).convert("RGB")
    factor = max(width / image.width, height / image.height)
    image = image.resize((round(image.width * factor), round(image.height * factor)), Image.LANCZOS)
    left = (image.width - width) // 2
    top = (image.height - height) // 2
    frame = np.asarray(image.crop((left, top, left + width, top + height)), dtype=np.uint8)
    return frame[::scale, ::scale, :] if scale > 1 else frame


def decode_clip(path, label):
    """Decode one model clip into PNG frames at output size."""
    target = BUILD / label
    if target.exists():
        shutil.rmtree(target)
    target.mkdir(parents=True)
    run(["ffmpeg", "-hide_banner", "-loglevel", "error", "-y", "-i", str(path),
         "-vf", "scale=%d:%d:force_original_aspect_ratio=increase,crop=%d:%d,setsar=1"
         % (WIDTH, HEIGHT, WIDTH, HEIGHT),
         "-fps_mode", "passthrough", "-start_number", "0", str(target / "%05d.png")])
    return sorted(target.glob("*.png"))


def smoothstep(x):
    x = min(1.0, max(0.0, x))
    return x * x * (3 - 2 * x)


def frame_at(frames, index):
    """Read one decoded frame as a float array."""
    index = min(max(index, 0), len(frames) - 1)
    return np.asarray(Image.open(frames[index]).convert("RGB"), dtype=np.float32)


def build_movie(scene):
    """Assemble the ten-second movie from the stored clips."""
    art = scene_paths(scene)
    for name in ("approach", "settle"):
        if not (CLIPS / (scene + "-" + name + ".mp4")).is_file():
            raise RuntimeError("missing stored clip: " + scene + "-" + name)
    approach = decode_clip(CLIPS / (scene + "-approach.mp4"), scene + "-approach")
    settle = decode_clip(CLIPS / (scene + "-settle.mp4"), scene + "-settle")
    if not approach or not settle:
        raise RuntimeError("missing model clips for " + scene)
    clean = scaled(art["00-clean"])
    approach_last = frame_at(approach, len(approach) - 1)
    settle_last = frame_at(settle, len(settle) - 1)

    total_frames = int(round(TOTAL * FPS))
    out = V2 / (scene + ".mp4")
    out.parent.mkdir(parents=True, exist_ok=True)
    temporary = out.with_suffix(".tmp.mp4")
    encoder = subprocess.Popen(
        ["ffmpeg", "-hide_banner", "-loglevel", "error", "-y",
         "-f", "rawvideo", "-pix_fmt", "rgb24", "-s", "%dx%d" % (WIDTH, HEIGHT), "-r", str(FPS), "-i", "-",
         "-i", str(V2 / (scene + ".wav")),
         "-c:v", "libx264", "-preset", "slow", "-crf", "17", "-pix_fmt", "yuv420p",
         "-profile:v", "high", "-movflags", "+faststart",
         "-c:a", "aac", "-b:a", "192k", "-ar", "44100", "-ac", "2", "-shortest",
         str(temporary)], stdin=subprocess.PIPE)
    try:
        for index in range(total_frames):
            t = index / FPS
            if t < APPROACH_END:
                position = t / APPROACH_END * (len(approach) - 1)
                low = int(math.floor(position))
                high = min(low + 1, len(approach) - 1)
                frame = frame_at(approach, low) * (1 - (position - low)) + frame_at(approach, high) * (position - low)
            elif t < SETTLE_END:
                local = t - APPROACH_END
                position = local / (SETTLE_END - APPROACH_END) * (len(settle) - 1)
                low = int(math.floor(position))
                high = min(low + 1, len(settle) - 1)
                frame = frame_at(settle, low) * (1 - (position - low)) + frame_at(settle, high) * (position - low)
                blend = smoothstep(local / JUNCTION_FADE)
                frame = approach_last * (1 - blend) + frame * blend
            else:
                local = (t - SETTLE_END) / ((total_frames - 1) / FPS - SETTLE_END)
                weight = smoothstep(local)
                frame = settle_last * (1 - weight) + clean * weight
            encoder.stdin.write(np.clip(frame, 0, 255).astype(np.uint8).tobytes())
        encoder.stdin.close()
    except Exception:
        encoder.kill()
        encoder.wait()
        raise
    if encoder.wait() != 0:
        raise RuntimeError("ffmpeg failed while writing " + str(out))
    temporary.replace(out)
    return out


def decode_sound(path):
    raw = subprocess.check_output(
        ["ffmpeg", "-hide_banner", "-loglevel", "error", "-i", str(path), "-vn",
         "-ac", "2", "-ar", "44100", "-f", "s16le", "-"], stderr=subprocess.DEVNULL)
    return np.frombuffer(raw, dtype="<i2").astype(np.float64) / 32768.0


def measure(scene):
    """Check objective media properties; visual identity needs human review."""
    import wave
    from scene_music import band_gain, spectrum

    movie = V2 / (scene + ".mp4")
    metadata = json.loads(subprocess.check_output(
        ["ffprobe", "-v", "error", "-count_frames", "-show_streams", "-show_format",
         "-of", "json", str(movie)], text=True))
    video = [s for s in metadata["streams"] if s["codec_type"] == "video"]
    audio = [s for s in metadata["streams"] if s["codec_type"] == "audio"]
    if len(video) != 1 or len(audio) != 1:
        raise RuntimeError("expected one video and one audio stream")
    v = video[0]
    if (v["codec_name"], v["pix_fmt"], v["width"], v["height"], v["r_frame_rate"],
            int(v["nb_read_frames"])) != ("h264", "yuv420p", WIDTH, HEIGHT, "24/1", 240):
        raise RuntimeError("movie format/frame contract failed")
    duration = float(metadata["format"]["duration"])
    if abs(duration - TOTAL) > 0.05:
        raise RuntimeError("movie duration contract failed")
    run(["ffmpeg", "-v", "error", "-xerror", "-i", str(movie), "-f", "null", "-"],
        stdout=subprocess.DEVNULL)

    tracks = {}
    music = {}
    for name in SCENES:
        path = V2 / (name + ".wav")
        if not path.exists():
            continue
        with wave.open(str(path), "rb") as wav:
            if (wav.getnchannels(), wav.getsampwidth(), wav.getframerate()) != (2, 2, 44100):
                raise RuntimeError("soundtrack format failed: " + name)
            samples = np.frombuffer(wav.readframes(wav.getnframes()), dtype="<i2").reshape(-1, 2) / 32768.0
        peak = float(np.max(np.abs(samples)))
        bands = [band_gain(samples.mean(axis=1), lo, hi)
                 for lo, hi in ((200, 600), (1000, 4000), (4000, 8000))]
        if abs(len(samples) / 44100 - TOTAL) > .02 or not 0 < peak <= 10 ** (-1 / 20):
            raise RuntimeError("soundtrack duration/peak failed: " + name)
        if not all(np.isfinite(e) and e > 0 for e in bands):
            raise RuntimeError("soundtrack frequency band missing: " + name)
        tracks[name] = samples
        music[name] = {"duration": len(samples) / 44100, "peak_dbfs": 20 * math.log10(peak), "bands": bands}
    pairs = {}
    names = list(tracks)
    for i, a in enumerate(names):
        for b in names[i + 1:]:
            corr = float(np.dot(spectrum(tracks[a]), spectrum(tracks[b])))
            pairs[a + "/" + b] = corr
            if corr >= .95:
                raise RuntimeError("soundtracks are too similar: " + a + "/" + b)
    original = tracks[scene].ravel()
    decoded = decode_sound(movie)
    if len(decoded) < len(original) or len(decoded) - len(original) > 2048:
        raise RuntimeError("decoded audio length mismatch")
    decoded = decoded[:len(original)]
    corr = float(np.dot(original, decoded) / (np.linalg.norm(original) * np.linalg.norm(decoded)))
    if not np.isfinite(corr) or corr <= .99:
        raise RuntimeError("embedded soundtrack correlation failed")

    raw = subprocess.check_output(
        ["ffmpeg", "-v", "error", "-i", str(movie), "-vf", "select=eq(n\\,239)",
         "-frames:v", "1", "-f", "rawvideo", "-pix_fmt", "rgb24", "-"])
    last = np.frombuffer(raw, dtype=np.uint8).reshape(HEIGHT, WIDTH, 3)
    clean = scaled(scene_paths(scene)["00-clean"])
    difference = np.abs(last.astype(np.int16) - clean.astype(np.int16))
    result = {"scene": scene, "duration": duration, "frames": int(v["nb_read_frames"]),
              "size": [WIDTH, HEIGHT], "audio_correlation": corr, "music": music,
              "spectral_cosine_similarity": pairs,
              "last_frame": {"pixel_equal": bool(np.array_equal(last, clean)),
                             "mean_absolute_rgb_error": float(difference.mean()),
                             "max_rgb_error": int(difference.max())},
              "visual_acceptance": "UNVERIFIED: automatic background subtraction and colour gating are not valid identity/person-height evidence"}
    print(json.dumps(result, ensure_ascii=False, indent=2))
    return result


def generate_clips(scene):
    """Ask MiniMax-H3 for the two motion clips of one scene."""
    import minimax_h3
    art = scene_paths(scene)
    CLIPS.mkdir(parents=True, exist_ok=True)
    plans = [
        ("approach", art["01-distant"], art["02-near"], 5, prompts(scene)[0]),
        ("settle", art["02-near"], art["03-seated"], 4, prompts(scene)[1]),
    ]
    log_path = CLIPS / (scene + "-prompts.json")
    log = json.loads(log_path.read_text()) if log_path.exists() else []
    for name, first, last, duration, prompt in plans:
        path = CLIPS / (scene + "-" + name + ".mp4")
        if path.is_file():
            print("reusing", path)
            continue
        path, task_id, payload = minimax_h3.generate(
            prompt, first, last, path, model=MODEL, resolution=RESOLUTION,
            duration=duration, ratio="adaptive")
        log = [entry for entry in log if entry["clip"] != name]
        log.append({"clip": name, "task_id": task_id, "prompt": prompt,
                    "first_frame": str(first.relative_to(ROOT)), "last_frame": str(last.relative_to(ROOT)),
                    "model": MODEL, "resolution": RESOLUTION, "duration": duration, "ratio": "adaptive"})
        log_path.write_text(json.dumps(log, ensure_ascii=False, indent=2), encoding="utf-8")
    return log


def write_manifest(scene):
    """Record what produced the assets of one scene."""
    movie = V2 / (scene + ".mp4")
    sound = V2 / (scene + ".wav")
    entries = {}
    for kind, path in (("movie", movie), ("soundtrack", sound)):
        if not path.is_file():
            continue
        metadata = json.loads(subprocess.check_output(
            ["ffprobe", "-v", "error", "-show_entries",
             "format=duration,size:stream=codec_name,width,height,r_frame_rate,sample_rate,channels",
             "-of", "json", str(path)], text=True))
        entries[kind] = {"path": str(path.relative_to(ROOT)), "sha256": sha256(path),
                         "probe": metadata}
    prompts = CLIPS / (scene + "-prompts.json")
    entries["model"] = MODEL
    entries["resolution"] = RESOLUTION
    entries["timeline"] = {"approach": [0, APPROACH_END], "settle": [APPROACH_END, SETTLE_END],
                           "merge": [SETTLE_END, TOTAL], "fps": FPS, "size": [WIDTH, HEIGHT]}
    if not prompts.is_file():
        raise RuntimeError("missing clip provenance: " + str(prompts))
    entries["clips"] = json.loads(prompts.read_text(encoding="utf-8"))
    entries["inputs"] = {str(path.relative_to(ROOT)): sha256(path)
                         for path in list(scene_paths(scene).values()) +
                         [CLIPS / (scene + "-" + name + ".mp4") for name in ("approach", "settle")]}
    entries["scripts"] = {name: sha256(ROOT / "scripts" / name)
                          for name in ("scene_animation.py", "scene_music.py")}
    entries["visual_acceptance"] = "pending user review; AC2/AC3 not certified"
    manifest = V2 / "manifest.json"
    data = json.loads(manifest.read_text(encoding="utf-8")) if manifest.is_file() else {}
    data[scene] = entries
    manifest.write_text(json.dumps(data, ensure_ascii=False, indent=2), encoding="utf-8")
    return manifest


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--scene", choices=SCENES, required=True)
    parser.add_argument("--stage", choices=("clips", "assemble", "measure", "all"), default="assemble")
    parser.add_argument("--measure", action="store_true")
    args = parser.parse_args()
    if args.measure or args.stage == "measure":
        measure(args.scene)
        return
    BUILD.mkdir(parents=True, exist_ok=True)
    if args.stage in ("clips", "all"):
        generate_clips(args.scene)
    if args.stage in ("assemble", "all"):
        from scene_music import render, write_wav
        for name in ("approach", "settle"):
            if not (CLIPS / (args.scene + "-" + name + ".mp4")).is_file():
                raise RuntimeError("missing stored clip: " + args.scene + "-" + name)
        write_wav(V2 / (args.scene + ".wav"), render(args.scene))
        build_movie(args.scene)
        write_manifest(args.scene)


if __name__ == "__main__":
    sys.path.insert(0, str(pathlib.Path(__file__).resolve().parent))
    sys.exit(main())
