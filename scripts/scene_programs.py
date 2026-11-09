#!/usr/bin/env python3
# coding: utf-8
"""Author and independently rebuild six licensed whole-track scene programs."""
import argparse
import hashlib
import importlib.util
import json
import math
import pathlib
import subprocess
import tempfile

import numpy as np
import scene_music as audio

ROOT = pathlib.Path(__file__).resolve().parents[1]
ASSETS = ROOT / 'assets/media/v4'
PLAN = ASSETS / 'programs.json'
CATALOG = ASSETS / 'music-sources.json'
REPORT = ASSETS / 'program-report.json'
NOTICES = ASSETS / 'THIRD-PARTY-NOTICES.txt'
SHARED = ROOT / '.harness/view/background-sound-program/render-program.py'
spec = importlib.util.spec_from_file_location('whole_track_authoring', SHARED)
whole = importlib.util.module_from_spec(spec)
spec.loader.exec_module(whole)
RATE = audio.RATE


def digest(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()


def load_inputs():
    plan = json.loads(PLAN.read_text())
    catalog = json.loads(CATALOG.read_text())['tracks']
    ledger = json.loads((ASSETS / 'sources.json').read_text())
    validate_plan(plan, catalog)
    paths = {title: audio.checked_source(ROOT, item) for title, item in catalog.items()}
    sources = {name: audio.checked_source(ROOT / ledger['cacheRoot'], ledger['sources'][name])
               for name in ('birds', 'stream')}
    return plan, catalog, ledger, paths, sources


def validate_plan(plan, catalog):
    known = [item['id'] for item in json.loads((ROOT / 'internal/media/scenes.json').read_text())
             if not item.get('legacy')]
    if [scene['id'] for scene in plan['scenes']] != known:
        raise ValueError('programs must contain the six canonical scenes in order')
    if (plan['crossfade_seconds'] != 12 or plan['minimum_cycle_seconds'] != 600 or
            plan['target_lufs'] != -23):
        raise ValueError('program parameters differ from the agreed structure')
    groups = set()
    for scene in plan['scenes']:
        titles = scene['tracks']
        hashes = [catalog[title]['sha256'] for title in titles]
        group = frozenset(hashes)
        if len(titles) < 2 or len(group) != len(titles):
            raise ValueError('a program must contain distinct complete sources')
        if group in groups:
            raise ValueError('renaming or reordering the same group is not a new scene')
        groups.add(group)
        positions = scene['nature_positions']
        if (not isinstance(positions, list) or not 3 <= len(positions) <= 12 or
                any(not isinstance(at, (int, float)) or not math.isfinite(at) for at in positions) or
                any(b <= a for a, b in zip(positions, positions[1:])) or
                len({round(b-a, 3) for a, b in zip(positions, positions[1:])}) < 2):
            raise ValueError('natural events must use an explicit irregular ordered timeline')
        if (scene['nature'] not in ('birds', 'stream') or
                not 0 < scene['nature_ratio'] <= .06 or
                not 0 < scene['event_seconds'] <= 30):
            raise ValueError('invalid restrained natural-event plan')


def nature_events(scene, frames, music_rms):
    seconds = scene['event_seconds']
    positions = scene['nature_positions']
    # Different source intervals; irregular destination times are owned by plan.
    offsets = [10 + index * (seconds + 4) for index in range(len(positions))]
    events = [dict(source=scene['nature'], at=at, offset=offset, seconds=seconds,
                   rms=music_rms * scene['nature_ratio'])
              for at, offset in zip(positions, offsets)]
    previous_end = -1
    for event in events:
        if event['at'] < 12 or event['at'] + seconds > frames / RATE - 12:
            raise ValueError('natural event crosses the loop join')
        if event['offset'] < previous_end:
            raise ValueError('natural recording source excerpts repeat')
        previous_end = event['offset'] + seconds
    return events


def compose(scene, plan, paths, sources, independent=False):
    tracks, levels = [], []
    for title in scene['tracks']:
        path = paths[title]
        samples = audio.decode_audio(path)
        lufs, peak = audio.audio_loudness(path)
        gain_db = plan['target_lufs'] - lufs
        samples *= 10 ** (gain_db / 20)
        tracks.append(samples)
        levels.append(dict(title=title, input_lufs=lufs, input_peak_dbfs=peak,
                           fixed_gain_db=gain_db))
    music, timeline = whole.join_whole_tracks(tracks, round(plan['crossfade_seconds'] * RATE))
    del tracks
    if len(music) / RATE <= plan['minimum_cycle_seconds']:
        raise ValueError('complete group must exceed ten minutes')
    rms = float(np.sqrt(np.mean(music ** 2)))
    if independent:
        return music, None, music, timeline, levels, []
    events = nature_events(scene, len(music), rms)
    nature = audio.render_focus_nature(
        dict(seconds=math.ceil(len(music) / RATE), nature_events=events), sources
    )[:len(music)]
    mix = music + nature
    if not np.isfinite(mix).all() or float(np.max(np.abs(mix))) > 10 ** (-3 / 20):
        raise ValueError('program exceeds finite peak headroom')
    return music, nature, mix, timeline, levels, events


def waveform_checks(music, mix):
    quiet = audio.longest_quiet_seconds(music)
    repeated = audio.duplicate_audio_windows(music)
    seam = float(np.max(np.abs(mix[0] - mix[-1])))
    if quiet >= 8 or repeated or seam > .02:
        raise ValueError('program has a long quiet gap, copied cycle or sample discontinuity')
    return dict(longest_quiet_seconds=quiet, duplicate_waveform_windows=repeated,
                loop_boundary_sample_step=seam)


def bindings():
    return {str(path.relative_to(ROOT)): digest(path) for path in
            (PLAN, CATALOG, ASSETS / 'sources.json', pathlib.Path(__file__),
             ROOT / 'scripts/scene_music.py', SHARED)}


def attribution(catalog, ledger):
    lines = ['专注力 — 六场景背景音乐与自然录音来源', '',
             '以下完整音乐均为 Kevin MacLeod 创作，来自 Incompetech。',
             '许可：Creative Commons Attribution 4.0 International（CC BY 4.0）',
             'https://creativecommons.org/licenses/by/4.0/', '']
    for title, item in catalog.items():
        lines += [f'“{title}” — {item["author"]}', item['page'], '']
    lines += ['音乐修改：固定增益校准、整曲间及首尾十二秒等功率交叠、低声自然录音混音，',
              '以 Apple Lossless（ALAC）编码。无变速、变调或短片段复制延长。',
              '部分曲目在不同场景共享；每组曲目组合不同，组内每曲仅出现一次。', '']
    for name in ('birds', 'stream'):
        item = ledger['sources'][name]
        lines += [f'{item["title"]} — {item["author"]}', item['page'],
                  item['license'], item['licenseURL'], '']
    lines += ['自然录音修改：各组采用不同源区间摘录、带通滤波、淡入淡出和音量调整。',
              '溪流与鸟声是配景，不代表录制于应用图片对应地点。',
              '原 MP3／Vorbis 有损，ALAC 仅无损保存混音，不恢复原始录音质量。', '']
    return '\n'.join(lines)


def render_all():
    plan, catalog, ledger, paths, sources = load_inputs()
    whole.boundary_examples()
    report = dict(version=1, bindings=bindings(), source_catalog=catalog,
                  nature_sources={key: ledger['sources'][key] for key in sources},
                  listening='待实际听感验收；数值检查不判定柔和、正向或接缝听感', scenes=[])
    out = ASSETS / 'programs'
    out.mkdir(exist_ok=True)
    for scene in plan['scenes']:
        print('Rendering ' + scene['id'], flush=True)
        music, nature, mix, timeline, levels, events = compose(scene, plan, paths, sources)
        checks = waveform_checks(music, mix)
        with tempfile.TemporaryDirectory(prefix='scene-program-') as temporary:
            pcm = pathlib.Path(temporary) / 'mix.wav'
            encoded = pathlib.Path(temporary) / 'mix.m4a'
            audio.write_wav(pcm, mix)
            subprocess.run(['ffmpeg', '-v', 'error', '-y', '-i', str(pcm),
                            '-c:a', 'alac', '-movflags', '+faststart', str(encoded)], check=True)
            if not np.array_equal(audio.decode_audio(pcm), audio.decode_audio(encoded)):
                raise ValueError('ALAC differs from PCM master')
            lufs, peak = audio.audio_loudness(encoded)
            if not -24 <= lufs <= -22 or peak > -3:
                raise ValueError('encoded program loudness or true peak outside target')
            sha = digest(encoded)
            destination = out / f'{scene["id"]}-{sha[:12]}.m4a'
            # The digest-named resource may already be referenced by a working
            # release. Never truncate it in place during a repeat render.
            with tempfile.TemporaryDirectory(prefix='program-publish-', dir=out) as staged:
                staged_file = pathlib.Path(staged) / destination.name
                staged_file.write_bytes(encoded.read_bytes())
                if digest(staged_file) != sha:
                    raise ValueError('staged program changed before publication')
                staged_file.replace(destination)
        report['scenes'].append(dict(
            id=scene['id'], name=scene['name'], tracks=scene['tracks'],
            reason=scene['reason'], timeline=timeline, levels=levels, nature_events=events,
            frames=len(mix), seconds=len(mix) / RATE, sample_rate=RATE,
            lufs=lufs, true_peak_dbfs=peak, checks=checks,
            source=str(destination.relative_to(ROOT)),
            path=str(destination.relative_to(ROOT / 'assets')), sha256=sha,
            bytes=destination.stat().st_size))
        del music, nature, mix
    REPORT.write_text(json.dumps(report, ensure_ascii=False, indent=2) + '\n')
    NOTICES.write_text(attribution(catalog, ledger))
    print('Authored six complete groups; subjective listening remains pending.', flush=True)


def check_all():
    if json.loads((ROOT / 'assets/release-media.json').read_text()).get('version') == 2:
        return independent_programs(False)
    plan, catalog, ledger, paths, sources = load_inputs()
    report = json.loads(REPORT.read_text())
    if report['bindings'] != bindings() or report['source_catalog'] != catalog:
        raise ValueError('program report predates current plan, sources or authoring code')
    if report['nature_sources'] != {key: ledger['sources'][key] for key in sources}:
        raise ValueError('natural recording ledger changed')
    if NOTICES.read_text() != attribution(catalog, ledger):
        raise ValueError('packaged attribution differs from actual sources and modifications')
    if [item['id'] for item in report['scenes']] != [item['id'] for item in plan['scenes']]:
        raise ValueError('program report omits or reorders scenes')
    hashes, loudness = set(), []
    for scene, item in zip(plan['scenes'], report['scenes']):
        print('Rebuilding and verifying ' + scene['id'], flush=True)
        path = ROOT / item['source']
        if not path.is_file() or digest(path) != item['sha256'] or item['sha256'] in hashes:
            raise ValueError('program missing, changed or duplicated across scene identities')
        hashes.add(item['sha256'])
        music, nature, mix, timeline, levels, events = compose(scene, plan, paths, sources)
        decoded = audio.decode_audio(path)
        if (item['tracks'] != scene['tracks'] or item['timeline'] != timeline or
                item['levels'] != levels or item['nature_events'] != events or
                item['frames'] != len(mix) or abs(item['seconds'] - len(mix) / RATE) > 1 / RATE):
            raise ValueError('program report does not cover each complete original source')
        if decoded.shape != mix.shape or np.max(np.abs(decoded - mix)) > 2 / 32767:
            raise ValueError('program differs from original tracks, full joins and natural recording plan')
        actual_checks = waveform_checks(music, decoded)
        if (item['checks']['longest_quiet_seconds'] != actual_checks['longest_quiet_seconds'] or
                item['checks']['duplicate_waveform_windows'] != actual_checks['duplicate_waveform_windows'] or
                abs(item['checks']['loop_boundary_sample_step'] - actual_checks['loop_boundary_sample_step']) > 2 / 32767):
            raise ValueError('program boundary report differs from encoded output')
        lufs, peak = audio.audio_loudness(path)
        if not -24 <= lufs <= -22 or peak > -3 or (lufs, peak) != (item['lufs'], item['true_peak_dbfs']):
            raise ValueError('program loudness metadata changed or outside target')
        probe = json.loads(subprocess.check_output([
            'ffprobe', '-v', 'error', '-show_streams', '-of', 'json', str(path)]))
        stream = probe['streams'][0]
        if stream['codec_name'] != 'alac' or stream['sample_rate'] != '44100' or stream['channels'] != 2:
            raise ValueError('program must be 44.1kHz stereo ALAC')
        loudness.append(lufs)
        del music, nature, mix, decoded
    if max(loudness) - min(loudness) > 1.5:
        raise ValueError('scene loudness differs by more than 1.5 LU')
    print('PASS: six distinct whole-track programs rebuilt from licensed originals; all exceed ten minutes.')
    print('Actual subjective listening and two complete application cycles remain unverified.')
    return report


def recording_samples(item):
    """Only decode and crossfade a continuous excerpt of the licensed recording."""
    source = audio.checked_source(ROOT, item)
    samples = audio.decode_audio(source)
    start = round(item['offset_seconds'] * RATE)
    end = start + round(item['excerpt_seconds'] * RATE)
    if start < 0 or end > len(samples) or end-start < RATE*30:
        raise ValueError('recording excerpt outside original')
    samples = samples[start:end].copy()
    lufs, _ = audio.audio_loudness(source)
    samples *= 10 ** ((-27-lufs)/20)
    fade = round(3*RATE)
    # Equal-power overlap joins the original tail to its original head. No noise,
    # synthesis, copied interior events or music is added to the recording.
    angle = np.linspace(0, math.pi/2, fade).astype(np.float32)
    samples[:fade] *= np.sin(angle)[:, None]
    samples[-fade:] *= np.cos(angle)[:, None]
    samples[:fade] += samples[-fade:]
    samples = samples[:-fade]
    peak = float(np.max(np.abs(samples)))
    if math.isfinite(peak) and peak > 10**(-4/20):
        samples *= 10**(-4/20)/peak
    if not np.isfinite(samples).all() or np.max(np.abs(samples)) > 10**(-3/20):
        raise ValueError('natural recording exceeds headroom')
    return samples


def independent_programs(render):
    plan, catalog, ledger, paths, sources = load_inputs()
    destination = ROOT/'assets/media/v5'
    destination.mkdir(exist_ok=True)
    report_path = destination/'audio-report.json'
    recordings_path = destination/'recording-sources.json'
    recordings = json.loads(recordings_path.read_text())['sources']
    binding = {**bindings(), str(recordings_path.relative_to(ROOT)): digest(recordings_path)}
    if render:
        report = dict(version=2, bindings=binding, source_catalog=catalog, scenes=[], ambiences=[],
                      listening='待实际完整听感及两轮接缝验收，数值不替代听感')
    else:
        report = json.loads(report_path.read_text())
        if report['bindings'] != binding or report['source_catalog'] != catalog:
            raise ValueError('independent audio report predates its source or authoring code')
        if [x['id'] for x in report['scenes']] != [x['id'] for x in plan['scenes']]:
            raise ValueError('independent report omits scenes')
        if [x['id'] for x in report['ambiences']] != list(recordings):
            raise ValueError('independent report omits recordings')

    def export_or_check(kind, ident, samples, metadata):
        print(('Rendering ' if render else 'Rebuilding ') + kind+' '+ident, flush=True)
        out = destination/kind
        out.mkdir(exist_ok=True)
        if render:
            with tempfile.TemporaryDirectory(prefix='focus-independent-') as temp:
                pcm, encoded = pathlib.Path(temp)/'master.wav', pathlib.Path(temp)/'master.m4a'
                audio.write_wav(pcm, samples)
                subprocess.run(['ffmpeg','-v','error','-y','-i',str(pcm),'-c:a','alac','-movflags','+faststart',str(encoded)],check=True)
                sha = digest(encoded)
                path = out/(ident+'-'+sha[:12]+'.m4a')
                path.write_bytes(encoded.read_bytes())
            lufs, peak = audio.audio_loudness(path)
            item = dict(id=ident, **metadata, source=str(path.relative_to(ROOT)),
                        path=str(path.relative_to(ROOT/'assets')),sha256=sha,
                        seconds=len(samples)/RATE, frames=len(samples),lufs=lufs,true_peak_dbfs=peak,
                        checks=waveform_checks(samples,samples))
            report['scenes' if kind=='music' else 'ambiences'].append(item)
        else:
            item = next(x for x in report['scenes' if kind=='music' else 'ambiences'] if x['id']==ident)
            path = ROOT/item['source']
            if digest(path) != item['sha256'] or any(item.get(k)!=v for k,v in metadata.items()):
                raise ValueError('changed resource or provenance '+ident)
        decoded = audio.decode_audio(path)
        if decoded.shape != samples.shape or np.max(np.abs(decoded-samples)) > 2/32767:
            raise ValueError('export does not reproduce licensed source '+ident)
        actual = waveform_checks(samples,decoded)
        if actual['loop_boundary_sample_step'] > .02:
            raise ValueError('loop discontinuity '+ident)
        lufs, peak = audio.audio_loudness(path)
        if peak > -3 or item['frames'] != len(samples) or (lufs,peak)!=(item['lufs'],item['true_peak_dbfs']):
            raise ValueError('invalid encoded audio metadata '+ident)
        if kind=='music' and not -24<=lufs<=-22:
            raise ValueError('music loudness outside target')

    for scene in plan['scenes']:
        music, _, _, timeline, levels, _ = compose(scene,plan,paths,sources,independent=True)
        export_or_check('music',scene['id'],music,dict(name=scene['name'],tracks=scene['tracks'],timeline=timeline,levels=levels))
        del music
    for ident, item in recordings.items():
        samples = recording_samples(item)
        export_or_check('nature',ident,samples,dict(name=item['name'],original=item))
        del samples
    if render:
        report_path.write_text(json.dumps(report,ensure_ascii=False,indent=2)+'\n')
        lines = ['专注力 — 独立背景音乐与自然录音','']
        for title,item in catalog.items():
            lines += [title+' — '+item['author'],item['page'],'CC BY 4.0 https://creativecommons.org/licenses/by/4.0/','']
        lines += ['音乐修改：固定增益、完整曲目间和首尾十二秒交叠，ALAC；无自然声混入。','']
        for item in recordings.values():
            lines += [item['title']+' — '+item['author'],item['page'],item['license']+' '+item['licenseURL'],'']
        lines += ['录音修改：连续区间摘取、固定增益、三秒首尾交叠；不生成白噪音。','原 MP3／Vorbis 有损；ALAC 仅无损保存加工结果。']
        (destination/'AUDIO-NOTICES.txt').write_text('\n'.join(lines)+'\n')
    print('PASS: six pure music programs and four original natural recordings reproduce licensed sources.',flush=True)
    return report


if __name__ == '__main__':
    parser = argparse.ArgumentParser()
    parser.add_argument('--render', action='store_true')
    parser.add_argument('--independent', action='store_true')
    args = parser.parse_args()
    if args.independent:
        independent_programs(args.render)
    elif args.render:
        render_all()
        check_all()
    else:
        check_all()
