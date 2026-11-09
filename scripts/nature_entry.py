#!/usr/bin/env python3
"""Build the six approved nature entries with FFmpeg, entirely offline.

The three imagegen plates retain their original pixels and alpha. FFmpeg alone
performs the scene compositing. This does not alter the release manifest.
"""
import argparse
import hashlib
import json
import math
import pathlib
import subprocess

ROOT = pathlib.Path(__file__).resolve().parents[1]
ART_ROOT = ROOT / "assets/artwork/v3"
ART = ART_ROOT / "forest"
SCENES = [x["id"] for x in json.loads((ROOT / "internal/media/scenes.json").read_text()) if not x.get("legacy")]
OUT = ROOT / "assets/media/v3"
BUILD = ROOT / ".build/nature-entry"
WIDTH, HEIGHT, FPS, FRAMES = 1920, 1248, 24, 120
LAYERS = (("background", .045), ("trees", .16), ("ferns", .29))


def run(args):
    subprocess.run([str(a) for a in args], cwd=ROOT, check=True)


def digest(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()


def probe(path):
    return json.loads(subprocess.check_output([
        "ffprobe", "-v", "error", "-show_streams", "-show_format", "-of", "json", str(path)
    ], text=True))


def measure(scene="forest"):
    art = ART_ROOT / scene
    layers = LAYERS if scene == "forest" else (("background", .045), ("middle", .16), ("near", .29))
    video = OUT / f"{scene}-entry.mp4"
    preview = OUT / f"{scene}-preview.mp4"
    for path, audio in ((video, False), (preview, True)):
        info = probe(path)
        v = next(s for s in info["streams"] if s["codec_type"] == "video")
        assert (v["width"], v["height"]) == (WIDTH, HEIGHT), info
        assert v["codec_name"] == "h264" and v["pix_fmt"] == "yuv420p", info
        assert int(v["nb_frames"]) == FRAMES and v["avg_frame_rate"] == "24/1", info
        assert abs(float(v["duration"]) - 5) < .01, info
        assert any(s["codec_type"] == "audio" for s in info["streams"]) == audio, info
        run(["ffmpeg", "-hide_banner", "-loglevel", "error", "-i", path, "-f", "null", "-"])
    # Lossy H.264 may refine a few reconstructed pixels after the source stops.
    # Check decoded stillness with a small luma tolerance, and also require the
    # same detector to reject a real moving segment of this exported video.
    import numpy as np
    raw = subprocess.check_output([
        "ffmpeg", "-v", "error", "-i", str(video), "-vf", "select=gte(n\\,113)",
        "-fps_mode", "passthrough", "-pix_fmt", "gray", "-f", "rawvideo", "-"
    ])
    tail = np.frombuffer(raw, np.uint8).reshape(-1, HEIGHT, WIDTH).astype(np.int16)
    delta = np.abs(np.diff(tail, axis=0))
    tail_mean, changed = float(delta.mean()), float((delta > 4).mean())
    assert len(tail) == 7 and tail_mean < .05 and changed < .0001, (tail_mean, changed)
    moving_raw = subprocess.check_output([
        "ffmpeg", "-v", "error", "-i", str(video), "-vf", "select=between(n\\,50\\,56)",
        "-fps_mode", "passthrough", "-pix_fmt", "gray", "-f", "rawvideo", "-"
    ])
    moving = np.frombuffer(moving_raw, np.uint8).reshape(-1, HEIGHT, WIDTH).astype(np.int16)
    moving_mean = float(np.abs(np.diff(moving, axis=0)).mean())
    assert len(moving) == 7 and moving_mean > .05, moving_mean
    report = {
        "date": "2026-11-04", "kind": "scene-media-verification", "scene": scene,
        "method": "built-in imagegen plates, FFmpeg multi-plane parallax",
        "source_image_size": [1536, 1024], "encoded_size": [WIDTH, HEIGHT],
        "source_resolution_note": "Images are resampled for the video canvas; not native 2K artwork.",
        "seconds": 5, "fps": FPS, "frames": FRAMES,
        "motion": {"curve": "quintic smootherstep, t=.15 through 4.60, then hold",
                   "scale_changes": dict(layers), "vanishing_point": [.5, .48]},
        "decoded_stillness": {"tail_mean_luma_difference": tail_mean,
                              "tail_fraction_changing_by_over_4": changed,
                              "moving_control_mean_luma_difference": moving_mean,
                              "mean_luma_tolerance": .05},
        "inputs": {str(p.relative_to(ROOT)): digest(p) for p in
                   [art / (name + ".png") for name, _ in layers] +
                   [art / ("prompts.json" if scene == "forest" else "generation.json"), OUT / (scene + "-work.wav")]},
        "outputs": {str(p.relative_to(ROOT)): digest(p) for p in
                    [video, preview, OUT / f"{scene}-background.png", OUT / f"{scene}-contact-sheet.png"]},
        "preview_audio": "Same scene original v3 work soundtrack, first five seconds with preview-only end fade.",
        "visual_review": "2.5D parallax, not full 3D photography; independent visual review tracked in harness",
        
        "ffmpeg": subprocess.check_output(["ffmpeg", "-version"], text=True).splitlines()[0],
        "script_sha256": digest(pathlib.Path(__file__))
    }
    (OUT / f"{scene}-report.json").write_text(json.dumps(report, ensure_ascii=False, indent=2) + "\n")
    print("PASS: both videos decode; 1920x1248, H.264, 24fps, 120 frames, 5s.")
    print(f"Decoded stillness: tail mean luma delta={tail_mean:.6f}; moving control={moving_mean:.6f}.")
    return report


def build(scene="forest"):
    art = ART_ROOT / scene
    layers = LAYERS if scene == "forest" else (("background", .045), ("middle", .16), ("near", .29))
    from PIL import Image
    OUT.mkdir(parents=True, exist_ok=True)
    BUILD.mkdir(parents=True, exist_ok=True)
    for name, _ in layers:
        with Image.open(art / (name + ".png")) as im:
            if name != "background":
                assert im.mode == "RGBA" and im.getchannel("A").getextrema()[0] == 0
                assert im.getpixel((im.width // 2, im.height // 2))[3] == 0
    # Work at double output size to reduce integer-pixel crop stepping. Each
    # depth plane moves independently, exposing scenery behind its alpha edges.
    u = "min(max((on/24-0.15)/4.45,0),1)"
    ease = f"(({u})*({u})*({u})*(10+({u})*(-15+6*({u}))))"
    inputs, filters = [], []
    for index, (name, gain) in enumerate(layers):
        inputs += ["-i", art / (name + ".png")]
        z = f"(1+{gain}*{ease})"
        filters.append(
            f"[{index}:v]scale=3840:2496:force_original_aspect_ratio=increase:flags=lanczos,"
            f"crop=3840:2496,format=rgba,zoompan=z='{z}':"
            "x='iw*0.5-iw/zoom*0.5':y='ih*0.48-ih/zoom*0.48':"
            f"d={FRAMES}:s={WIDTH}x{HEIGHT}:fps={FPS},format=rgba,setsar=1[p{index}]"
        )
    filters += ["[p0][p1]overlay=0:0:format=auto[m]",
                "[m][p2]overlay=0:0:format=auto,format=yuv420p[v]"]
    graph = ";\n".join(filters)
    (BUILD / f"{scene}-filter.txt").write_text(graph + "\n")
    command = ["ffmpeg", "-hide_banner", "-loglevel", "warning", "-y", *inputs,
               "-filter_complex_threads", "1", "-filter_complex", graph,
               "-map", "[v]", "-frames:v", str(FRAMES), "-an", "-c:v", "libx264",
               "-preset", "slow", "-crf", "18", "-threads", "4", "-pix_fmt", "yuv420p",
               "-movflags", "+faststart", OUT / f"{scene}-entry.mp4"]
    (BUILD / f"{scene}-command.json").write_text(json.dumps([str(x) for x in command], indent=2))
    run(command)
    run(["ffmpeg", "-v", "error", "-y", "-i", OUT / f"{scene}-entry.mp4", "-i",
         OUT / f"{scene}-work.wav", "-map", "0:v:0", "-map", "1:a:0",
         "-af", "afade=t=in:d=0.7,afade=t=out:st=4.55:d=0.45",
         "-c:v", "copy", "-c:a", "aac", "-b:a", "192k", "-t", "5", "-movflags", "+faststart",
         OUT / f"{scene}-preview.mp4"])
    run(["ffmpeg", "-v", "error", "-y", "-i", OUT / f"{scene}-entry.mp4", "-vf",
         "select=eq(n\\,119)", "-frames:v", "1", OUT / f"{scene}-background.png"])
    run(["ffmpeg", "-v", "error", "-y", "-i", OUT / f"{scene}-entry.mp4", "-vf",
         "select='eq(n,0)+eq(n,36)+eq(n,72)+eq(n,113)',scale=480:312,tile=2x2:padding=8:margin=8:color=white",
         "-frames:v", "1", OUT / f"{scene}-contact-sheet.png"])
    measure(scene)


def recorded_source_time(frame):
    """Keep the first five seconds real time, then ease velocity to zero."""
    time = frame / FPS
    if time <= 5:
        return time
    progress = min((time - 5) / 2.5, 1)
    return 5 + 1.25 * (progress + math.sin(math.pi * progress) / math.pi)


def render_recorded_frames(source, offset, video, frames):
    """Blend adjacent real frames; never warp foliage with optical flow."""
    import numpy as np
    u='min(on/179,1)'
    ease=f'(({u})*({u})*({u})*(10+({u})*(-15+6*({u}))))'
    graph=(f'scale=3840:2496:flags=lanczos,'
           f"zoompan=z='1+.015*{ease}':x='iw/2-iw/zoom/2':y='ih/2-ih/zoom/2':d=1:s={WIDTH}x{HEIGHT}:fps={FPS},"
           'setsar=1,format=yuv420p')
    decode,encode=None,None
    try:
        decode = subprocess.Popen([
            'ffmpeg', '-xerror', '-v', 'error', '-ss', str(offset), '-i', str(source),
            '-vf', f'fps={FPS},scale={WIDTH}:{HEIGHT}:force_original_aspect_ratio=increase:flags=lanczos,crop={WIDTH}:{HEIGHT}',
            '-frames:v', str(math.ceil(recorded_source_time(frames-1)*FPS)+2),
            '-pix_fmt', 'rgb24', '-f', 'rawvideo', '-'], stdout=subprocess.PIPE)
        encode = subprocess.Popen([
            'ffmpeg', '-xerror', '-v', 'error', '-y', '-f', 'rawvideo', '-pix_fmt', 'rgb24',
            '-s', f'{WIDTH}x{HEIGHT}', '-r', str(FPS), '-i', '-', '-vf', graph,
            '-frames:v', str(frames), '-an', '-c:v', 'libx264', '-preset', 'slow',
            '-crf', '18', '-threads', '4', '-movflags', '+faststart', str(video)], stdin=subprocess.PIPE)
        def read_frame():
            data=decode.stdout.read(WIDTH*HEIGHT*3)
            if len(data)!=WIDTH*HEIGHT*3:
                raise ValueError('recorded source ends before the entry timeline')
            return np.frombuffer(data,np.uint8).reshape(HEIGHT,WIDTH,3).astype(np.float32)
        left=read_frame();right=read_frame();index=0
        for frame in range(frames):
            position=recorded_source_time(frame)*FPS
            target=math.floor(position)
            while index<target:
                left,right=right,read_frame();index+=1
            weight=position-target
            image=np.rint(left*(1-weight)+right*weight).astype(np.uint8)
            encode.stdin.write(image.tobytes())
        encode.stdin.close()
        # Drain the decoder before waiting; a longer source is bounded above.
        decode.stdout.read();decode.stdout.close()
        if decode.wait()!=0 or encode.wait()!=0:
            raise RuntimeError('recorded entry decoding or encoding failed')
    finally:
        for process in (decode,encode):
            if process is None:
                continue
            if process.poll() is None:
                process.terminate()
                try:
                    process.wait(timeout=5)
                except subprocess.TimeoutExpired:
                    process.kill();process.wait()
            for pipe in (process.stdin,process.stdout):
                if pipe is not None:
                    pipe.close()


def recorded_entry(scene, render=False):
    """Encode/verify real footage; old parallax rendering remains reproducible."""
    import numpy as np
    folder=ROOT/'assets/media/v5'
    ledger=folder/'video-sources.json'
    item=json.loads(ledger.read_text())['scenes'][scene]
    source=ROOT/item['source']
    if (source.resolve()!=source.absolute() or not source.is_file() or digest(source)!=item['sha256']
            or item['license'] not in ('Pexels License','PD-self','CC0-1.0','CC-BY-3.0','CC-BY-4.0')
            or not item['author'] or not item['page'].startswith('https://')):
        raise ValueError('recorded footage source/license/hash invalid '+scene)
    out=folder/'entry';out.mkdir(exist_ok=True)
    video=out/(scene+'-entry.mp4');background=out/(scene+'-background.png')
    report_path=out/(scene+'-report.json')
    frames=192;seconds=frames/FPS
    if render:
        render_recorded_frames(source,item['offset_seconds'],video,frames)
        run(['ffmpeg','-xerror','-v','error','-y','-i',video,'-vf','select=eq(n\\,191)',
             '-frames:v','1',background])
        run(['ffmpeg','-v','error','-y','-i',video,'-vf',
             "select='eq(n,0)+eq(n,48)+eq(n,96)+eq(n,179)',scale=480:312,tile=2x2:padding=8:margin=8:color=white",
             '-frames:v','1',out/(scene+'-contact-sheet.png')])
    v=probe(video)
    stream=next(x for x in v['streams'] if x['codec_type']=='video')
    if ((stream['width'],stream['height'])!=(WIDTH,HEIGHT) or stream['codec_name']!='h264'
            or stream['pix_fmt']!='yuv420p' or stream['avg_frame_rate']!='24/1'
            or int(stream['nb_frames'])!=frames or abs(float(stream['duration'])-seconds)>.01
            or any(x['codec_type']=='audio' for x in v['streams'])):
        raise ValueError('entry encoding differs from eight-second silent-video contract')
    run(['ffmpeg','-xerror','-v','error','-i',video,'-f','null','-'])
    raw=subprocess.check_output(['ffmpeg','-v','error','-i',str(video),'-vf','scale=384:250',
                                 '-pix_fmt','gray','-f','rawvideo','-'])
    images=np.frombuffer(raw,np.uint8).reshape(-1,250,384).astype(np.int16)
    tail=float(np.abs(np.diff(images[-8:],axis=0)).mean())
    movement=float(np.abs(images[96]-images[48]).mean())
    if tail>.1 or movement<.2:raise ValueError('missing final hold or moving control '+scene)
    deltas=np.abs(np.diff(images,axis=0)).mean(axis=(1,2))
    start=float(deltas[120:132].mean())
    ending=float(deltas[168:180].mean())
    if start<=0 or ending/start>=.35:
        raise ValueError('recorded motion does not settle gradually '+scene)
    report=dict(kind='recorded-entry-verification',scene=scene,seconds=seconds,frames=frames,
                source=item,source_ledger_sha256=digest(ledger),script_sha256=digest(pathlib.Path(__file__)),
                motion='real footage: first 5s original time, next 2.5s cosine velocity easing with adjacent-frame blends, final .5s hold; supplementary eased 1.5% approach',
                encoding=dict(width=WIDTH,height=HEIGHT,fps=FPS,codec='h264',audio=False),
                decoded_tail_mean_delta=tail,moving_control_difference=movement,
                decoded_settling=dict(start_mean_delta=start,end_mean_delta=ending,ratio=ending/start),
                outputs={str(p.relative_to(ROOT)):digest(p) for p in [video,background]},
                visual_review='逐片自然运动、人物、转场与变形另行实际审查，数值不代判')
    if render:
        report_path.write_text(json.dumps(report,ensure_ascii=False,indent=2)+'\n')
    elif json.loads(report_path.read_text())!=report:
        raise ValueError('recorded entry report differs from current source/output '+scene)
    print('PASS: recorded eight-second entry, final hold and moving control '+scene,flush=True)
    return report


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--measure", action="store_true", help="read-only validation of the saved forest sample")
    parser.add_argument("--scene", choices=SCENES, default="forest")
    parser.add_argument("--all", action="store_true")
    parser.add_argument("--recorded", action="store_true")
    args = parser.parse_args()
    for scene in (SCENES if args.all else [args.scene]):
        if args.recorded:
            recorded_entry(scene,render=not args.measure)
        else:
            measure(scene) if args.measure else build(scene)
