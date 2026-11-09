#!/usr/bin/env python3
"""Offline scene audio authoring. Legacy synthesis and v4 sampled instruments.

The v4 forest candidate uses a scored MIDI performance, licensed recorded
instruments, and field recordings. It never changes the release manifest.
"""
import argparse, hashlib, json, math, pathlib, random, shutil, subprocess, sys, tempfile, wave

import numpy as np

ROOT = pathlib.Path(__file__).resolve().parents[1]
OUT = ROOT / "assets/media/v2"
RATE = 44100
DURATION = 10.0
FOCUS_ASSETS = ROOT / "assets/media/v4"
FOCUS_CACHE = ROOT / ".cache/focus-audio-v4/sources"


def scene_plan(scene):
    """Per-scene key, tempo, voicing and spectral balance."""
    return {
        "forest": {
            "pad": [146.83, 220.0, 293.66, 369.99], "pad_gain": 0.30,
            "arp": [293.66, 369.99, 440.0, 587.33, 440.0, 369.99], "arp_step": 0.5,
            "arp_gain": 0.16, "chime": [1174.7, 1567.98, 1567.98, 2093.0], "chime_gain": 0.10,
            "air": 0.012, "tilt": {"low": 1.0, "mid": 0.55, "high": 0.30},
            "seed": 4101, "hue": "D major, warm and low",
        },
        "beach": {
            "pad": [110.0, 164.81, 220.0, 277.18], "pad_gain": 0.32,
            "arp": [220.0, 277.18, 329.63, 440.0, 329.63, 277.18], "arp_step": 0.5,
            "arp_gain": 0.18, "chime": [880.0, 1318.5, 1174.7, 1760.0], "chime_gain": 0.11,
            "air": 0.020, "tilt": {"low": 0.95, "mid": 0.80, "high": 0.45},
            "seed": 4102, "hue": "A major, rolling and airy",
        },
        "meadow": {
            "pad": [196.0, 246.94, 293.66, 392.0], "pad_gain": 0.26,
            "arp": [392.0, 493.88, 587.33, 392.0, 587.33, 493.88], "arp_step": 0.375,
            "arp_gain": 0.21, "chime": [1046.5, 1396.9, 1567.98, 1046.5], "chime_gain": 0.13,
            "air": 0.014, "tilt": {"low": 0.55, "mid": 1.15, "high": 0.60},
            "seed": 4103, "hue": "G major, bright and flowing",
        },
        "snow": {
            "pad": [110.0, 130.81, 164.81, 220.0], "pad_gain": 0.30,
            "arp": [220.0, 261.63, 329.63, 440.0], "arp_step": 0.75,
            "arp_gain": 0.14, "chime": [2093.0, 2637.0, 3135.96, 2093.0], "chime_gain": 0.15,
            "air": 0.008, "tilt": {"low": 0.45, "mid": 0.50, "high": 1.20},
            "seed": 4104, "hue": "A minor, sparse and crystalline",
        },
    }[scene]


def band_gain(signal, low, high):
    """Average energy of a signal inside a frequency band."""
    spectrum = np.abs(np.fft.rfft(signal))
    freqs = np.fft.rfftfreq(len(signal), 1 / RATE)
    mask = (freqs >= low) & (freqs < high)
    return float(np.mean(spectrum[mask] ** 2))


def string_pad(track, plan, rng, start, stop, gain):
    """Warm sustained strings: detuned harmonic stacks with slow envelopes."""
    count = int((stop - start) * RATE)
    t = np.arange(count) / RATE
    attack = np.clip(t / 1.4, 0, 1) ** 1.6
    release = np.clip((stop - start - t) / 1.6, 0, 1) ** 1.4
    envelope = attack * release
    voice = np.zeros(count)
    for index, freq in enumerate(plan["pad"]):
        for side in (-1, 1):
            detune = 1 + side * (0.0012 + 0.0006 * rng.random())
            vibrato = 1 + 0.0022 * np.sin(2 * math.pi * (0.11 + 0.05 * rng.random()) * t
                                         + rng.random() * 6.28)
            for harmonic, weight in ((1, 1.0), (2, 0.42), (3, 0.20), (4, 0.09), (5, 0.04)):
                voice += weight / (1 + 0.6 * index) * np.sin(
                    2 * math.pi * freq * detune * harmonic * vibrato * t
                    + rng.random() * 6.28)
    voice *= envelope / 8.0
    track[int(start * RATE):int(stop * RATE)] += (gain * voice)[:, None]


def harp_note(track, freq, start, gain, rng, decay=2.6):
    """One plucked harp string: inharmonic partials with staggered decays."""
    start = int(start * RATE)
    count = min(int(decay * 1.6 * RATE), len(track) - start)
    if count <= 0:
        return
    t = np.arange(count) / RATE
    note = np.zeros(count)
    stiffness = 0.00022
    for harmonic in range(1, 13):
        partial = harmonic * freq * math.sqrt(1 + stiffness * harmonic * harmonic)
        if partial > 9000:
            break
        weight = 1.0 / (harmonic ** 1.45)
        life = decay / (1 + 0.22 * (harmonic - 1))
        note += weight * np.exp(-t / life) * np.sin(
            2 * math.pi * partial * t + rng.random() * 6.28)
    burst = int(0.006 * RATE)
    note[:burst] += 0.25 * rng.uniform(-1, 1) * np.exp(-np.arange(burst) / 0.0012)
    pan = rng.choice([-0.35, 0.35])
    track[start:start + count, 0] += gain * note * (0.75 - 0.25 * pan)
    track[start:start + count, 1] += gain * note * (0.75 + 0.25 * pan)


def wind_chime(track, freq, start, gain, rng, life=2.4):
    """One wind chime strike: inharmonic bell partials, fast decay."""
    start = int(start * RATE)
    count = min(int(life * 1.4 * RATE), len(track) - start)
    if count <= 0:
        return
    t = np.arange(count) / RATE
    strike = np.zeros(count)
    for ratio, weight, life_scale in ((1.0, 1.0, 1.0), (2.76, 0.55, 0.62), (5.40, 0.30, 0.40),
                                       (8.93, 0.16, 0.26)):
        partial = freq * ratio
        if partial > 12000:
            continue
        strike += weight * np.exp(-t / (life * life_scale)) * np.sin(
            2 * math.pi * partial * t + rng.random() * 6.28)
    pan = rng.uniform(-0.7, 0.7)
    track[start:start + count, 0] += gain * strike * (0.5 - 0.5 * pan)
    track[start:start + count, 1] += gain * strike * (0.5 + 0.5 * pan)


def air_bed(track, level, rng):
    """Very quiet filtered noise so the mix is not digitally sterile."""
    count = len(track)
    noise = np.random.default_rng(rng.randrange(2 ** 31)).uniform(-1, 1, count)
    smooth = np.empty(count)
    state = 0.0
    for index in range(count):
        state = 0.92 * state + 0.08 * noise[index]
        smooth[index] = state
    shape = np.sin(np.pi * np.arange(count) / count) ** 0.6
    track[:, 0] += level * smooth * shape
    track[:, 1] += level * np.roll(smooth, 971) * shape


def equalize(track, tilt):
    """Measure the three band energies so the scene balance can be set."""
    low = band_gain(track[:, 0], 200, 600)
    mid = band_gain(track[:, 0], 1000, 4000)
    high = band_gain(track[:, 0], 4000, 8000)
    gains = []
    for current, target in ((low, tilt["low"]), (mid, tilt["mid"]), (high, tilt["high"])):
        gains.append(math.sqrt(target / current) if current > 1e-9 else 1.0)
    return gains


def render(scene):
    """Synthesize one scene track and return the float samples."""
    plan = scene_plan(scene)
    rng = random.Random(plan["seed"])
    count = int(DURATION * RATE)
    track = np.zeros((count, 2))

    string_pad(track, plan, rng, 0.0, 9.6, plan["pad_gain"])
    string_pad(track, plan, rng, 2.2, 9.6, plan["pad_gain"] * 0.45)

    step = plan["arp_step"]
    time = 2.4
    index = 0
    while time < 8.4:
        freq = plan["arp"][index % len(plan["arp"])]
        harp_note(track, freq, time, plan["arp_gain"] * (0.7 + 0.3 * rng.random()), rng)
        if rng.random() < 0.35:
            harp_note(track, freq * 2, time + step * 0.5, plan["arp_gain"] * 0.35, rng, decay=1.8)
        time += step
        index += 1

    time = 3.6
    index = 0
    while time < 9.2:
        freq = plan["chime"][index % len(plan["chime"])]
        wind_chime(track, freq, time, plan["chime_gain"] * (0.6 + 0.4 * rng.random()), rng)
        time += 1.1 + 0.9 * rng.random()
        index += 1

    air_bed(track, plan["air"], rng)

    low_gain, mid_gain, high_gain = equalize(track, plan["tilt"])
    mixed = track.copy()
    for channel in range(2):
        mixed[:, channel] = (track[:, channel] * low_gain * 0.6
                              + np.roll(track[:, channel], 3) * mid_gain * 0.30
                              + np.roll(track[:, channel], 11) * high_gain * 0.12)

    envelope = np.ones(count)
    envelope[:int(0.9 * RATE)] = np.linspace(0, 1, int(0.9 * RATE)) ** 1.5
    tail = np.linspace(0, 1, int(2.2 * RATE))
    envelope[-int(2.2 * RATE):] = 0.5 * (1 + np.cos(np.pi * tail))
    mixed *= envelope[:, None]

    peak = float(np.max(np.abs(mixed)))
    if peak > 0:
        mixed *= 0.708 / peak
    return mixed


def write_wav(path, samples):
    """Write float samples as 16-bit PCM stereo."""
    clipped = np.clip(samples, -1.0, 1.0)
    pcm = (clipped * 32767).astype("<i2").tobytes()
    path.parent.mkdir(parents=True, exist_ok=True)
    with wave.open(str(path), "wb") as out:
        out.setnchannels(2)
        out.setsampwidth(2)
        out.setframerate(RATE)
        out.writeframes(pcm)

def render_work():
    """A separate two-minute low-density bed; never loop the entry soundtrack."""
    duration = 120
    plan = scene_plan("forest")
    rng = random.Random(4105)
    track = np.zeros((duration * RATE, 2))
    # Reuse the authored instruments at quieter levels and longer intervals.
    for start in range(0, duration - 12, 9):
        string_pad(track, plan, rng, start, min(start + 18, duration), 0.10)
    for start in range(7, duration - 8, 13):
        harp_note(track, plan["arp"][rng.randrange(6)], start, 0.018, rng, decay=5.0)
    air_bed(track, 0.006, rng)
    # Both endpoints meet digital silence with zero slope, avoiding a click.
    count = 2 * RATE
    fade = np.sin(np.linspace(0, np.pi / 2, count)) ** 2
    track[:count] *= fade[:, None]
    track[-count:] *= fade[::-1, None]
    peak = float(np.max(np.abs(track)))
    if peak > 0:
        track *= 0.20 / peak
    return track


def render_nature_work(scene):
    """An original, periodic 120-second meditation bed with low nature textures.

    Frequencies and noise live on a circular time domain. Notes wrap their tails
    across the boundary; there is no silent release at each loop. All textures,
    including the distant bird-like sounds, are synthesized rather than field recordings.
    """
    plans = {
        "mountain": (45, [0, 7, 12, 16], [0, 5, 2, 0], [11, 29, 53, 78, 101], .22, 620, 5101),
        "snow-mountain": (57, [0, 5, 7, 12], [0, -2, 5, 0], [19, 47, 83, 106], .13, 480, 5102),
        "meadow": (55, [0, 4, 7, 14], [0, -5, 2, 5], [7, 21, 39, 58, 79, 99, 113], .27, 2200, 5103),
        "forest": (50, [0, 4, 9, 12], [0, 5, -2, 7], [9, 26, 48, 69, 92, 110], .21, 1500, 5104),
        "pond": (53, [0, 7, 9, 16], [0, -2, 5, 2], [13, 36, 61, 89, 108], .18, 1800, 5105),
        "garden": (48, [0, 4, 7, 19], [0, 7, 5, 2], [6, 18, 34, 51, 73, 94, 111], .30, 2000, 5106),
    }
    root, voicing, changes, starts, pluck_gain, cutoff, seed = plans[scene]
    duration, count = 120, 120 * RATE
    rng = np.random.default_rng(seed)
    t = np.arange(count, dtype=np.float64) / RATE
    track = np.zeros((count, 2), dtype=np.float64)
    for chord, change in enumerate(changes):
        distance = (t - chord * 30 + 60) % duration - 60
        envelope = np.where(np.abs(distance) < 30, .5 + .5 * np.cos(np.pi * distance / 30), 0)
        for voice, interval in enumerate(voicing):
            hz = 440 * 2 ** ((root + change + interval - 69) / 12)
            phase = float(rng.uniform(0, 2 * np.pi))
            for side in (-1, 1):
                frequency = round(hz * (1 + side * .0008) * duration) / duration
                signal = (np.sin(2 * np.pi * frequency * t + phase) +
                          .12 * np.sin(4 * np.pi * frequency * t + phase / 2))
                gain = .035 * envelope / (1 + voice * .35)
                track[:, 0] += signal * gain * (1 + side * .12)
                track[:, 1] += signal * gain * (1 - side * .12)
    for note, start in enumerate(starts):
        hz = 440 * 2 ** ((root + 12 + voicing[(note * 3) % len(voicing)] - 69) / 12)
        length = int(8 * RATE)
        nt = np.arange(length) / RATE
        envelope = (1 - np.exp(-nt / .10)) * np.exp(-nt / 2.2) * np.sin(np.pi * nt / 8) ** 2
        tone = (np.sin(2 * np.pi * hz * nt) + .10 * np.sin(4 * np.pi * hz * nt))
        indices = (int(start * RATE) + np.arange(length)) % count
        track[indices, 0] += pluck_gain * .09 * envelope * tone
        track[indices, 1] += pluck_gain * .08 * envelope * tone
    frequencies = np.fft.rfftfreq(count, 1 / RATE)
    spectrum_noise = np.fft.rfft(rng.normal(size=count))
    shape = 1 / np.sqrt(np.maximum(frequencies, 80))
    shape *= np.exp(-(frequencies / cutoff) ** 4) * (1 - np.exp(-(frequencies / 100) ** 4))
    wind = np.fft.irfft(spectrum_noise * shape, n=count)
    wind *= .0015 / max(float(np.std(wind)), 1e-9)
    wind *= .7 + .3 * np.sin(2 * np.pi * t / 24 + seed) ** 2
    track[:, 0] += wind
    track[:, 1] += np.roll(wind, 631)
    if scene == "pond":
        # Quiet filtered water texture with a periodic, non-rhythmic rippling envelope.
        water = np.fft.irfft(spectrum_noise * shape * frequencies / 650, n=count)
        water *= .0012 / max(float(np.std(water)), 1e-9)
        water *= .5 + .25 * np.sin(2 * np.pi * t / 15) + .2 * np.sin(2 * np.pi * t / 8)
        track += np.column_stack((water, np.roll(water, 389)))
    if scene in ("forest", "meadow", "garden"):
        for start in (23 + seed % 11, 81 + seed % 13):
            nt = np.arange(int(.45 * RATE)) / RATE
            chirp = np.sin(2 * np.pi * (1250 * nt + 380 * nt ** 2)) * np.sin(np.pi * nt / .45) ** 4
            indices = (int(start * RATE) + np.arange(len(nt))) % count
            track[indices, :] += .0016 * chirp[:, None]
    # Same RMS across scenes, leaving ample headroom without compression or AGC.
    rms = float(np.sqrt(np.mean(track ** 2)))
    track *= .055 / max(rms, 1e-9)
    peak = float(np.max(np.abs(track)))
    if peak > .60:
        track *= .60 / peak
    return track


def write_nature_music():
    import json
    scenes = json.loads((ROOT / "internal/media/scenes.json").read_text())
    report = {}
    for scene in scenes:
        if scene.get("legacy"):
            continue
        name = scene["id"]
        samples = render_nature_work(name)
        write_wav(ROOT / "assets/media/v3" / (name + "-work.wav"), samples)
        seam = float(np.max(np.abs(samples[0] - samples[-1])))
        peak = float(np.max(np.abs(samples)))
        report[name] = {"seconds": 120, "sample_rate": RATE, "channels": 2,
                        "peak_dbfs": 20 * math.log10(max(peak, 1e-9)), "seam_step": seam,
                        "origin": "original local synthesis, including nature textures; no external samples",
                        "listening_acceptance": "requires actual listening; numeric checks are not a verdict"}
        print(name, report[name], flush=True)
    (ROOT / "assets/media/v3/music-provenance.json").write_text(json.dumps(report, ensure_ascii=False, indent=2) + "\n")


def spectrum(samples):
    """Normalized log magnitude spectrum used for cross-scene comparison."""
    mono = samples.mean(axis=1)
    values = np.abs(np.fft.rfft(mono))
    values = np.log1p(values)
    norm = np.linalg.norm(values)
    return values / norm if norm else values


def load_focus_score():
    score = json.loads((FOCUS_ASSETS / "forest-score.json").read_text())
    validate_focus_score(score)
    return score


def validate_focus_score(score):
    """Validate the agreed ten-minute score before invoking external tools."""
    if score.get("scene") != "forest" or score.get("seconds") != 600:
        raise ValueError("forest score must span 600 seconds")
    bpm = score.get("bpm", 0)
    if not isinstance(bpm, (int, float)) or not math.isfinite(bpm) or not 48 <= bpm <= 60:
        raise ValueError("invalid slow tempo")
    sections = score.get("sections", [])
    if len(sections) != 8 or abs(len(sections) * 16 * 4 * 60 / bpm - 600) > .001:
        raise ValueError("eight sections must fill 600 seconds")
    beats = score.get("melody_beats", [])
    if len(beats) != 8 or any(not isinstance(b, (int, float)) or not math.isfinite(b) or not 0 <= b < 16 for b in beats) or beats != sorted(set(beats)):
        raise ValueError("invalid phrase timing")
    signatures = set()
    for section in sections:
        duration = section.get("note_seconds", 0)
        if not isinstance(duration, (int, float)) or not math.isfinite(duration) or not .2 <= duration <= 4:
            raise ValueError("note duration must leave a natural release")
        for key in ("velocity", "guitar_velocity"):
            if not isinstance(section.get(key), int) or not 20 <= section[key] <= 70:
                raise ValueError("invalid soft velocity")
        melody, chords = section.get("melody", []), section.get("chords", [])
        if len(melody) != 4 or any(len(phrase) != 8 for phrase in melody) or len(chords) != 4 or any(len(chord) != 4 for chord in chords):
            raise ValueError("each section needs four phrases and four voicings")
        for note in [n for phrase in melody for n in phrase if n is not None] + [n for chord in chords for n in chord]:
            if not isinstance(note, int) or not 47 <= note <= 78:
                raise ValueError("note outside the authored middle register")
        signature = json.dumps(melody)
        if signature in signatures:
            raise ValueError("duplicate section is not a new variation")
        signatures.add(signature)
        support = section.get("support", [])
        if len(support) != 4:
            raise ValueError("each phrase needs its own quiet support")
        for phrase in support:
            for when, voice, seconds, velocity in phrase:
                if not all(isinstance(value, (int, float)) and math.isfinite(value) for value in (when, voice, seconds, velocity)):
                    raise ValueError("invalid support event")
                if not 0 <= when < 16 or not isinstance(voice, int) or voice not in (0, 1, 2, 3) or not .2 <= seconds <= 4 or not isinstance(velocity, int) or not 20 <= velocity <= 60:
                    raise ValueError("support event exceeds the soft score boundary")
    for event in score.get("nature_events", []):
        if event.get("source") not in ("birds", "stream"):
            raise ValueError("unknown nature source")
        for key in ("at", "offset", "seconds", "rms"):
            if not isinstance(event.get(key), (int, float)) or not math.isfinite(event[key]):
                raise ValueError("invalid nature event")
        if event["at"] < 0 or event["offset"] < 0 or not 2 <= event["seconds"] <= 120 or event["at"] + event["seconds"] > 585 or not 0 < event["rms"] <= .01:
            raise ValueError("nature event exceeds the quiet score boundary")


def checked_source(cache, item):
    if item.get("license") not in ("CC0-1.0", "CC-BY-4.0"):
        raise ValueError("source license is not approved for this route")
    if not item.get("author") or not item.get("page", "").startswith("https://"):
        raise ValueError("source needs author and provenance page")
    relative = pathlib.Path(item.get("path", ""))
    if relative.is_absolute() or ".." in relative.parts or not relative.parts:
        raise ValueError("source path escapes the cache")
    root = cache.resolve()
    path = root / relative
    if path.resolve() != path or not path.is_file():
        raise ValueError("source path missing or replaced by a link")
    if not 0 < path.stat().st_size <= 128 * 1024 * 1024:
        raise ValueError("source size outside the authoring boundary")
    if hashlib.sha256(path.read_bytes()).hexdigest() != item.get("sha256"):
        raise ValueError("source hash mismatch")
    return path


def fold_tail(signal, frames):
    """Wrap the post-cycle decay onto the head, leaving the original untouched."""
    if frames <= 0 or not frames <= len(signal) <= frames * 2:
        raise ValueError("invalid circular tail length")
    loop = signal[:frames].copy()
    tail = signal[frames:]
    loop[:len(tail)] += tail
    return loop


def focus_events(score, instrument):
    """Derive performance events from composed phrases, not random melodies."""
    validate_focus_score(score)
    rng = random.Random(score["seed"])
    beat = 60 / score["bpm"]
    events = []
    for section_index, section in enumerate(score["sections"]):
        start = section_index * 64 * beat
        if instrument == "piano":
            for phrase_index, phrase in enumerate(section["melody"]):
                for index, note in enumerate(phrase):
                    if note is None:
                        continue
                    when = start + (phrase_index * 16 + score["melody_beats"][index]) * beat
                    when = max(0, when + rng.uniform(-.025, .025))
                    events.append((when, section["note_seconds"], note, section["velocity"] + rng.randint(-3, 3), 0))
            for phrase_index, support in enumerate(section["support"]):
                chord = section["chords"][phrase_index]
                for when, voice, duration, velocity in support:
                    events.append((start + (phrase_index * 16 + when) * beat,
                                   duration, chord[voice], velocity, 1))
        elif instrument == "guitar":
            for phrase_index, chord in enumerate(section["chords"]):
                # One quiet two-note response per phrase, not a strumming loop.
                when = start + (phrase_index * 16 + 7.4 + (section_index % 3) * .35) * beat
                for delay, note in ((0, chord[2]), (.16, chord[3])):
                    events.append((when + delay, 1.6, note, section["guitar_velocity"], 0))
        else:
            raise ValueError("unknown forest instrument")
    events.sort()
    validate_note_lifetimes(events)
    return events


def validate_note_lifetimes(events):
    ends = {}
    for when, duration, note, velocity, channel in sorted(events):
        key = (channel, note)
        if when < ends.get(key, -1) - 1e-7:
            raise ValueError("same-channel note overlap would truncate the performance")
        ends[key] = when + duration


def write_focus_midi(path, score, instrument):
    # Offline-only dependency; legacy commands and ordinary tests need no mido.
    import mido
    midi = mido.MidiFile(ticks_per_beat=960)
    track = mido.MidiTrack()
    midi.tracks.append(track)
    # Use 60 BPM for exact seconds-to-ticks; musical tempo already owns event time.
    track.append(mido.MetaMessage("set_tempo", tempo=1000000, time=0))
    events = focus_events(score, instrument)
    for channel in sorted({event[4] for event in events}):
        track.append(mido.Message("program_change", program=0, channel=channel, time=0))
    messages = []
    for when, duration, note, velocity, channel in events:
        messages.append((round(when * 960), 1, channel, note, velocity))
        messages.append((round((when + duration) * 960), 0, channel, note, 0))
    previous = 0
    for tick, on, channel, note, velocity in sorted(messages):
        track.append(mido.Message("note_on" if on else "note_off", channel=channel,
                                  note=note, velocity=velocity, time=tick - previous))
        previous = tick
    end = (score["seconds"] + 10) * 960
    track.append(mido.MetaMessage("end_of_track", time=end - previous))
    midi.save(str(path))


def decode_audio(path):
    data = subprocess.check_output(["ffmpeg", "-hide_banner", "-loglevel", "error",
        "-i", str(path), "-ar", str(RATE), "-ac", "2", "-f", "f32le", "-"])
    result = np.frombuffer(data, dtype="<f4").reshape(-1, 2).copy()
    if not len(result) or not np.isfinite(result).all():
        raise ValueError("empty or invalid decoded source")
    return result


def render_focus_instrument(score, source, instrument, folder):
    midi = folder / (instrument + ".mid")
    dry = folder / (instrument + "-render.wav")
    write_focus_midi(midi, score, instrument)
    command = ["fluidsynth", "-ni", "-C", "0", "-R", "1", "-g", ".5",
        "-r", str(RATE), "-o", "synth.reverb.room-size=.3", "-o", "synth.reverb.damp=.7",
        "-o", "synth.reverb.level=.12", "-F", str(dry), "-T", "wav", "-O", "float",
        str(source), str(midi)]
    with (folder / (instrument + ".log")).open("w") as log:
        subprocess.run(command, check=True, stdout=log, stderr=subprocess.STDOUT, timeout=180)
    samples = decode_audio(dry)
    # Ten seconds allow the real instrument and room tails to settle.
    samples = samples[:(score["seconds"] + 10) * RATE]
    if len(samples) < (score["seconds"] + 9) * RATE:
        raise ValueError("instrument renderer truncated the authored cycle")
    return fold_tail(samples, score["seconds"] * RATE)


def render_focus_nature(score, sources):
    from scipy.signal import butter, sosfilt
    track = np.zeros((score["seconds"] * RATE, 2), dtype=np.float32)
    decoded = {name: decode_audio(sources[name]) for name in ("birds", "stream")}
    for event in score["nature_events"]:
        offset, frames = round(event["offset"] * RATE), round(event["seconds"] * RATE)
        clip = decoded[event["source"]][offset:offset + frames].copy()
        if len(clip) != frames:
            raise ValueError("nature excerpt extends beyond its original recording")
        # Keep natural calls and water movement; remove sub-bass rumble only.
        low, high = (850, 6500) if event["source"] == "birds" else (140, 4200)
        clip = sosfilt(butter(2, [low, high], btype="bandpass", fs=RATE, output="sos"), clip, axis=0).astype(np.float32)
        rms = float(np.sqrt(np.mean(clip ** 2)))
        if rms < 1e-5:
            raise ValueError("nature excerpt is effectively silent")
        clip *= event["rms"] / rms
        fade = min(round(4 * RATE), frames // 3)
        curve = np.sin(np.linspace(0, np.pi / 2, fade)) ** 2
        clip[:fade] *= curve[:, None]
        clip[-fade:] *= curve[::-1, None]
        start = round(event["at"] * RATE)
        track[start:start + frames] += clip
    return track


def audio_loudness(path):
    import re
    result = subprocess.run(["ffmpeg", "-hide_banner", "-i", str(path),
        "-af", "ebur128=peak=true", "-f", "null", "-"], capture_output=True, text=True, check=True)
    integrated = re.findall(r"I:\s*(-?[\d.]+) LUFS", result.stderr)
    peaks = re.findall(r"Peak:\s*(-?[\d.]+) dBFS", result.stderr)
    if not integrated or not peaks:
        raise ValueError("loudness measurement produced no result")
    return float(integrated[-1]), float(peaks[-1])


def duplicate_audio_windows(samples, sample_rate=RATE):
    """Find waveform cycles up to half the track, then verify full-rate matches.

    A coarse autocorrelation nominates periods without a fixed 20s alignment.
    Full-rate comparisons at four distant positions prevent a recurring short
    musical motif being mistaken for a pasted whole backing track. This still
    cannot decide whether a newly played melody sounds monotonous.
    """
    from scipy.signal import correlate, find_peaks, resample_poly
    mono = samples.mean(axis=1).astype(np.float32)
    if len(mono) < 40 * sample_rate:
        return []
    down = max(1, sample_rate // 2205)
    coarse = resample_poly(mono, 1, down) if down > 1 else mono
    coarse -= float(np.mean(coarse))
    n = len(coarse)
    low, high = round(5 * sample_rate / down), n // 2
    if high <= low:
        return []
    correlation = correlate(coarse, coarse, mode="full", method="fft")[n - 1:]
    power = np.r_[0., np.cumsum(coarse.astype(np.float64) ** 2)]
    lags = np.arange(low, high + 1)
    scale = np.sqrt(power[n - lags] * (power[n] - power[lags]))
    values = correlation[lags] / np.maximum(scale, 1e-12)
    peaks, _ = find_peaks(np.r_[0., values, 0.], height=.70,
                          distance=max(1, round(.2 * sample_rate / down)))
    candidates = sorted((int(p - 1) for p in peaks), key=lambda p: values[p], reverse=True)[:12]
    width = 20 * sample_rate
    def similarity(start, lag):
        a = mono[start:start + width]
        b = mono[start + lag:start + lag + width]
        denominator = float(np.linalg.norm(a)) * float(np.linalg.norm(b))
        return float(np.dot(a, b)) / denominator if denominator > 1e-9 else 0.
    # Prefer the earliest verified return over a multiple of the same period.
    for candidate in sorted(candidates):
        estimate = int(lags[candidate]) * down
        offsets = range(max(1, estimate - down), estimate + down + 1)
        lag = max(offsets, key=lambda offset: similarity(width, offset))
        last = len(mono) - lag - width
        probes = [round(last * fraction) for fraction in (0, 1/3, 2/3, 1)]
        if min(similarity(start, lag) for start in probes) >= .9995:
            return [[0., lag / sample_rate]]
    return []


def longest_quiet_seconds(samples, sample_rate=RATE):
    """Count consecutive quiet 100ms frames, including the circular boundary."""
    hop = max(1, sample_rate // 10)
    count = len(samples) // hop
    if not count:
        return 0.
    blocks = samples[:count * hop].reshape(count, hop, 2)
    quiet = np.sqrt(np.mean(blocks ** 2, axis=(1, 2))) < .001
    longest = current = 0
    for value in np.r_[quiet, quiet]:
        current = current + 1 if value else 0
        longest = max(longest, current)
    return min(longest, count) * hop / sample_rate


def check_midi_performance(path, score, instrument):
    import mido
    midi = mido.MidiFile(str(path))
    tick, active, actual = 0, {}, []
    for message in midi.tracks[0]:
        tick += message.time
        if message.type == "note_on" and message.velocity:
            key = (message.channel, message.note)
            if key in active:
                raise ValueError("serialized MIDI overlaps a live same-channel note")
            active[key] = (tick, message.velocity)
        elif message.type == "note_off" or (message.type == "note_on" and not message.velocity):
            key = (message.channel, message.note)
            if key not in active:
                raise ValueError("serialized MIDI stops an inactive note")
            start, velocity = active.pop(key)
            actual.append((start, tick, message.note, velocity, message.channel))
    expected = [(round(when * 960), round((when + duration) * 960), note, velocity, channel)
                for when, duration, note, velocity, channel in focus_events(score, instrument)]
    if active or sorted(actual) != sorted(expected):
        raise ValueError("serialized MIDI disagrees with the authored note times or voices")
    return len(actual)


def render_forest_sample():
    """Produce an isolated candidate, never a release-manifest substitution."""
    score = load_focus_score()
    ledger = json.loads((FOCUS_ASSETS / "sources.json").read_text())
    sources = {name: checked_source(FOCUS_CACHE, ledger["sources"][name])
               for name in ("piano", "guitar", "birds", "stream")}
    output = ROOT / ".harness/view/background-sound-sample"
    output.mkdir(parents=True, exist_ok=True)
    cache = ROOT / ".cache/focus-audio-v4"
    manifest_hash = hashlib.sha256((ROOT / "assets/release-media.json").read_bytes()).hexdigest()
    with tempfile.TemporaryDirectory(prefix="render-", dir=cache) as temporary:
        folder = pathlib.Path(temporary)
        piano = render_focus_instrument(score, sources["piano"], "piano", folder)
        guitar = render_focus_instrument(score, sources["guitar"], "guitar", folder)
        # Fixed per-stem gain retains all the scored dynamics; no time-varying AGC.
        piano *= .055 / max(float(np.sqrt(np.mean(piano ** 2))), 1e-8)
        guitar *= .007 / max(float(np.sqrt(np.mean(guitar ** 2))), 1e-8)
        music = piano + guitar
        nature = render_focus_nature(score, sources)
        master = music + nature
        raw = folder / "mix.wav"
        if float(np.max(np.abs(master))) >= 1:
            raise ValueError("mix clips before mastering")
        write_wav(raw, master)
        integrated, _ = audio_loudness(raw)
        gain = 10 ** ((-23 - integrated) / 20)
        if float(np.max(np.abs(master))) * gain > .70:
            raise ValueError("target loudness exceeds peak headroom; revise performance")
        for name, samples in (("music", music), ("nature", nature), ("mix", master)):
            path = folder / ("forest-" + name + ".wav")
            write_wav(path, samples * gain)
            subprocess.run(["ffmpeg", "-hide_banner", "-loglevel", "error", "-y", "-i", str(path),
                "-c:a", "alac", "-sample_fmt", "s16p", "-movflags", "+faststart",
                str(folder / ("forest-" + name + ".m4a"))], check=True)
        # Verify the complete candidate before replacing any published audition file.
        report = check_forest_sample(folder, folder)
        for instrument in ("piano", "guitar"):
            shutil.copyfile(folder / (instrument + ".mid"), output / (instrument + ".mid"))
            shutil.copyfile(folder / (instrument + ".log"), output / (instrument + ".log"))
        for name in ("music", "nature", "mix"):
            # PCM masters remain authoring inputs, outside the release bundle.
            shutil.copyfile(folder / ("forest-" + name + ".wav"), cache / ("forest-" + name + ".wav"))
            # Browser audition needs PCM: this host cannot decode ALAC in HTML audio.
            shutil.copyfile(folder / ("forest-" + name + ".wav"), output / ("forest-" + name + ".wav"))
            (folder / ("forest-" + name + ".m4a")).replace(output / ("forest-" + name + ".m4a"))
    if hashlib.sha256((ROOT / "assets/release-media.json").read_bytes()).hexdigest() != manifest_hash:
        raise ValueError("formal media manifest changed during candidate rendering")
    report.update(scoreSHA256=hashlib.sha256((FOCUS_ASSETS / "forest-score.json").read_bytes()).hexdigest(),
                  sources=ledger, formalManifestSHA256=manifest_hash,
                  performanceNotes={name: len(focus_events(score, name)) for name in ("piano", "guitar")},
                  listening="Requires user listening approval before making the other five scenes.")
    (output / "sample-report.json").write_text(json.dumps(report, ensure_ascii=False, indent=2) + "\n")
    print(json.dumps(report, ensure_ascii=False, indent=2))


def check_forest_sample(output=None, masters=None):
    output = output or ROOT / ".harness/view/background-sound-sample"
    masters = masters or ROOT / ".cache/focus-audio-v4"
    score = load_focus_score()
    report = {"serializedMIDINotes": {instrument: check_midi_performance(output / (instrument + ".mid"), score, instrument)
                                      for instrument in ("piano", "guitar")}}
    for name in ("music", "nature", "mix"):
        path = output / ("forest-" + name + ".m4a")
        metadata = json.loads(subprocess.check_output(["ffprobe", "-v", "error", "-show_entries",
            "stream=codec_name,sample_rate,channels,bits_per_raw_sample", "-of", "json", str(path)], text=True))
        streams = metadata.get("streams", [])
        if len(streams) != 1 or any(streams[0].get(key) != value for key, value in
                (("codec_name", "alac"), ("sample_rate", "44100"), ("channels", 2), ("bits_per_raw_sample", "16"))):
            raise ValueError("candidate must be 44.1kHz/16-bit/stereo ALAC")
        data = subprocess.check_output(["ffmpeg", "-hide_banner", "-loglevel", "error", "-i", str(path), "-f", "s16le", "-"])
        with wave.open(str(masters / ("forest-" + name + ".wav"))) as original:
            pcm = original.readframes(original.getnframes())
        if pcm != data or len(data) != 600 * RATE * 2 * 2:
            raise ValueError("candidate is truncated or lossless comparison failed")
        with wave.open(str(output / ("forest-" + name + ".wav"))) as browser:
            if browser.getparams()[:3] != (2, 2, RATE) or browser.readframes(browser.getnframes()) != pcm:
                raise ValueError("browser audition differs from the verified PCM master")
        samples = np.frombuffer(data, dtype="<i2").reshape(-1, 2).astype(np.float32) / 32768
        seam = float(np.max(np.abs(samples[0] - samples[-1])))
        steps = float(np.quantile(np.abs(np.diff(samples, axis=0)), .999))
        if seam > steps * 1.5 + 2 / 32768:
            raise ValueError("candidate loop seam exceeds ordinary sample steps")
        report[name] = dict(seconds=600, bytes=path.stat().st_size, sha256=hashlib.sha256(path.read_bytes()).hexdigest(),
                            exactPCM=True, browserPCMExact=True, format=streams[0], seam=seam, ordinaryStepP999=steps)
        if name != "nature":
            duplicates = duplicate_audio_windows(samples)
            if duplicates:
                raise ValueError("candidate contains copied music windows")
            loudness, peak = audio_loudness(path)
            if name == "mix" and (not -24 <= loudness <= -22 or peak > -3):
                raise ValueError("candidate loudness or true peak outside the agreed range")
            quiet = longest_quiet_seconds(samples)
            if quiet >= 8:
                raise ValueError("candidate music contains a long silence hole")
            # Keep the broad energy diagnostic, but never use disjoint bins as the quiet-run gate.
            energy = np.sqrt(np.mean(samples.reshape(60, 10 * RATE, 2) ** 2, axis=(1, 2)))
            report[name].update(lufs=loudness, truePeakDBFS=peak, minimum10sRMS=float(np.min(energy)), longestQuietSeconds=quiet, repeatedMusicSegments=duplicates)
    return report


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--scene", choices=("forest", "beach", "meadow", "snow"))
    parser.add_argument("--all", action="store_true")
    parser.add_argument("--work", action="store_true", help="render the separate 120-second forest work bed")
    parser.add_argument("--nature", action="store_true", help="render the six continuous v3 scene soundtracks")
    parser.add_argument("--forest-sample", action="store_true", help="render the isolated ten-minute v4 recorded-instrument candidate")
    parser.add_argument("--check-forest-sample", action="store_true", help="decode and verify all three v4 candidate stems")
    args = parser.parse_args()
    if (args.forest_sample or args.check_forest_sample) and (args.nature or args.work or args.all or args.scene or (args.forest_sample and args.check_forest_sample)):
        parser.error("v4 candidate commands cannot be combined with other authoring modes")
    if args.forest_sample:
        render_forest_sample()
        return
    if args.check_forest_sample:
        print(json.dumps(check_forest_sample(), ensure_ascii=False, indent=2))
        return
    if args.nature:
        write_nature_music()
        return
    if args.work:
        write_wav(OUT / "forest-work.wav", render_work())
        print("forest work bed: 120 seconds, stereo PCM, local synthesis")
        return
    scenes = ("forest", "beach", "meadow", "snow") if args.all or not args.scene else (args.scene,)
    tracks = {}
    for scene in scenes:
        samples = render(scene)
        write_wav(OUT / (scene + ".wav"), samples)
        tracks[scene] = samples
        mono = samples.mean(axis=1)
        print("%s  %.3fs  peak %.2f dBFS  bands L/M/H %.3e %.3e %.3e" % (
            scene, len(samples) / RATE,
            20 * math.log10(max(1e-9, float(np.max(np.abs(samples))))),
            band_gain(mono, 200, 600), band_gain(mono, 1000, 4000), band_gain(mono, 4000, 8000)))
    names = list(tracks)
    for i in range(len(names)):
        for j in range(i + 1, len(names)):
            a, b = spectrum(tracks[names[i]]), spectrum(tracks[names[j]])
            print("corr %s/%s %.4f" % (names[i], names[j], float(np.dot(a, b))))


if __name__ == "__main__":
    sys.exit(main())
