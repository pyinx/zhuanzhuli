#!/usr/bin/env python3
"""Reproducible local media from original procedural 3D scenes.

Entry movies are Blender 3D renders; soundscapes are synthesized,
not recordings. Does not fetch assets or modify the original prototype.
"""
import math, pathlib, random, shutil, struct, subprocess, wave

def natural_guide():
    """Local-only voice production. Never used to create natural recordings."""
    import json, hashlib
    import mlx.core as mx
    import numpy as np
    import soundfile as sf
    from huggingface_hub import snapshot_download
    from mlx_audio.tts.utils import load_model
    folder=ROOT/'assets/media/v5';folder.mkdir(exist_ok=True)
    model_id='mlx-community/Qwen3-TTS-12Hz-1.7B-VoiceDesign-4bit'
    revision='5c390979e4b93af5f2932f90742ca99c7dd04687'
    source=snapshot_download(model_id,revision=revision)
    mx.random.seed(42)
    text='把注意力，轻轻放回这一件事。'
    instruction='年轻成年女性，声音温暖自然，像身旁朋友轻声说一句话。自然普通话，正常偏舒缓语速，句意连贯，轻松平静而不困倦。不使用播音腔、不刻意气声、不拖长字、不夸张表演。'
    model=load_model(source)
    results=list(model.generate_voice_design(text=text,language='Chinese',instruct=instruction))
    samples=np.concatenate([np.array(x.audio) for x in results]);rate=results[0].sample_rate
    if not 0<len(samples)/rate<=5:raise ValueError('guide must fit every entry')
    master=folder/'guide-natural.wav'
    sf.write(master,samples,rate,subtype='PCM_24')
    output=folder/'guide.m4a'
    run(['ffmpeg','-v','error','-y','-i',str(master),'-af','loudnorm=I=-20:TP=-3:LRA=7','-ar','44100','-c:a','alac',str(output)])
    (folder/'voice-source.json').write_text(json.dumps(dict(model=model_id,revision=revision,
        upstream='https://huggingface.co/Qwen/Qwen3-TTS-12Hz-1.7B-VoiceDesign',
        license='Apache-2.0',licenseURL='https://www.apache.org/licenses/LICENSE-2.0',
        text=text,instruction=instruction,seed=42,mlx_audio='0.5.5',sample_rate=rate,
        seconds=len(samples)/rate,processing='LUFS -20 / true peak -3, ALAC; local inference, no paid API',
        master_sha256=hashlib.sha256(master.read_bytes()).hexdigest(),
        sha256=hashlib.sha256(output.read_bytes()).hexdigest(),listening='待实际自然度验收'),ensure_ascii=False,indent=2)+'\n')
    print(output)
ROOT=pathlib.Path(__file__).resolve().parents[1]
OUT=ROOT/"assets/media"
def run(args, **kw):
    subprocess.run(args, check=True, **kw)
def main():
    import argparse
    parser=argparse.ArgumentParser();parser.add_argument('--from-rendered',action='store_true',help='encode already rendered .build/scenes3d frames; developer must render current source first');parser.add_argument('--natural-guide',action='store_true');args=parser.parse_args()
    if args.natural_guide:
        natural_guide();return
    OUT.mkdir(parents=True,exist_ok=True)
    for n,scene in enumerate(("forest","beach","meadow","snow")):
        rendered=ROOT/".build/scenes3d"/scene
        if not args.from_rendered:
            run(["/Applications/Blender.app/Contents/MacOS/Blender","--background","--threads","6","--python-exit-code","1","--python",str(ROOT/"scripts/scenes3d.py"),"--",scene])
        frames=[rendered/("frame-%04d.png"%i) for i in range(1,145)]
        if not (rendered/"background.png").is_file() or not all(f.is_file() for f in frames):raise RuntimeError("complete 3D render required: "+scene)
        shutil.copyfile(rendered/"background.png",OUT/(scene+".png"))
        run(["ffmpeg","-hide_banner","-loglevel","error","-y","-framerate","24","-i",str(rendered/"frame-%04d.png"),"-frames:v","144","-an","-c:v","libx264","-crf","20","-pix_fmt","yuv420p","-movflags","+faststart",str(OUT/(scene+".mp4"))])
        rng=random.Random(417+n);rate=22050;seconds=12;noise=0;samples=[]
        for i in range(rate*seconds):
            t=i/rate;noise=.975*noise+.025*rng.uniform(-1,1)
            cycle=.6+.4*math.cos(2*math.pi*t/seconds)
            v=noise*(.48 if scene=="snow" else .8)*cycle
            if scene in ("forest","meadow"):
                chirp=max(0,math.sin(2*math.pi*t/3))**35
                v+=.016*chirp*math.sin(2*math.pi*(1500*t+100*math.sin(2*math.pi*t)))
            if scene=="beach":v+=.014*math.sin(2*math.pi*83*t)*(1+math.cos(2*math.pi*t/6))
            samples.append(v)
        # Overlap the tail with the head, then start immediately after the head.
        fade=rate//8
        cross=[]
        for i in range(fade):
            f=i/(fade-1);cross.append(samples[-fade+i]*(1-f)+samples[i]*f)
        samples=samples[fade:-fade]+cross
        pcm=b"".join(struct.pack("<h",round(max(-1,min(1,v))*32767)) for v in samples)
        with wave.open(str(OUT/(scene+".wav")),"wb") as out:
            out.setparams((1,2,rate,0,"NONE","not compressed"));out.writeframes(pcm)
    run(["say","-v","Tingting","-r","150","-o",str(OUT/"guide.aiff"),
         "把注意力，轻轻放回这一件事。"])
    print("4 original 3D scenes, 6-second movies, synthesized soundscapes and local voice generated")
if __name__=="__main__":main()
