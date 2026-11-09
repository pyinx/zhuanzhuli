#!/usr/bin/env python3
"""Direct MiniMax-H3 image-to-video client.

Calls api.minimaxi.com/v2/video_generation with the API key that the local
Magpie provider config already stores. No Seedance, no Ark, no plugin.
"""
import argparse, base64, io, json, os, pathlib, sys, time, urllib.error, urllib.request

ROOT = pathlib.Path(__file__).resolve().parents[1]
PROVIDERS = pathlib.Path(os.path.expanduser("~/.config/magpie/providers.json"))
BASE = "https://api.minimaxi.com"
MAX_UPLOAD_EDGE = 1920


def api_key():
    """Return the MiniMax (China) API key from the local provider config."""
    config = json.loads(PROVIDERS.read_text())
    for provider in config.get("providers", []):
        if provider.get("id") == "minimax-cn" and provider.get("key"):
            return provider["key"]
    raise RuntimeError("no minimax-cn key in " + str(PROVIDERS))


def data_url(path):
    """Encode a local image as a data URL, downscaling the long edge if needed."""
    from PIL import Image

    image = Image.open(path)
    if image.mode != "RGB":
        image = image.convert("RGB")
    if max(image.size) > MAX_UPLOAD_EDGE:
        scale = MAX_UPLOAD_EDGE / max(image.size)
        image = image.resize((round(image.width * scale), round(image.height * scale)), Image.LANCZOS)
    buffer = io.BytesIO()
    image.save(buffer, format="PNG")
    return "data:image/png;base64," + base64.b64encode(buffer.getvalue()).decode()


def request(path, payload=None):
    """Issue one authenticated JSON request and return the decoded body."""
    url = BASE + path
    body = None if payload is None else json.dumps(payload).encode()
    req = urllib.request.Request(
        url,
        data=body,
        headers={"Authorization": "Bearer " + api_key(), "Content-Type": "application/json"},
        method="GET" if payload is None else "POST",
    )
    try:
        with urllib.request.urlopen(req, timeout=180) as response:
            return json.loads(response.read().decode())
    except urllib.error.HTTPError as error:
        detail = error.read().decode(errors="replace")
        raise RuntimeError("MiniMax HTTP %s %s: %s" % (error.code, url, detail[:800])) from None


def submit(prompt, first_frame, last_frame=None, model="MiniMax-H3", resolution="768P",
           duration=4, ratio="adaptive"):
    """Create a video generation task and return its task id."""
    content = [{"type": "text", "text": prompt}]
    if first_frame:
        content.append({"type": "image_url", "image_url": {"url": data_url(first_frame)},
                        "role": "first_frame"})
    if last_frame:
        content.append({"type": "image_url", "image_url": {"url": data_url(last_frame)},
                        "role": "last_frame"})
    payload = {"model": model, "content": content, "resolution": resolution,
               "duration": duration, "ratio": ratio}
    body = request("/v2/video_generation", payload)
    if body.get("base_resp", {}).get("status_code") not in (0, None):
        raise RuntimeError("MiniMax rejected task: %s" % json.dumps(body.get("base_resp"), ensure_ascii=False))
    task_id = body.get("task_id")
    if not task_id:
        raise RuntimeError("MiniMax returned no task_id: %s" % json.dumps(body, ensure_ascii=False)[:800])
    return task_id, payload


def task_status(body):
    """The task status, which lives under task.status in the v2 query answer."""
    task = body.get("task") or {}
    return str(body.get("status") or task.get("status") or "Unknown").lower()


def poll(task_id, timeout=1800, interval=15):
    """Poll a task until it succeeds, fails, or the timeout expires."""
    deadline = time.time() + timeout
    while time.time() < deadline:
        body = request("/v2/query/video_generation/" + task_id)
        status = task_status(body)
        if status in ("success", "succeeded"):
            return body
        if status in ("fail", "failed"):
            return body
        time.sleep(interval)
    raise RuntimeError("MiniMax task %s timed out" % task_id)


def download(url, dest):
    """Save the generated video file."""
    dest = pathlib.Path(dest)
    dest.parent.mkdir(parents=True, exist_ok=True)
    with urllib.request.urlopen(url, timeout=300) as response:
        dest.write_bytes(response.read())
    return dest


def generate(prompt, first_frame, last_frame=None, dest=None, **kw):
    """Submit, wait, and download one clip. Returns (path, task_id, payload)."""
    task_id, payload = submit(prompt, first_frame, last_frame, **kw)
    print("task", task_id, "submitted", flush=True)
    body = poll(task_id)
    if task_status(body) not in ("success", "succeeded"):
        raise RuntimeError("MiniMax task failed: %s" % json.dumps(body.get("file_id", body), ensure_ascii=False)[:800])
    url = (body.get("task", {}).get("content") or {}).get("url")
    if not url:
        raise RuntimeError("MiniMax task produced no url: %s" % json.dumps(body, ensure_ascii=False)[:800])
    path = download(url, dest)
    print("saved", path, flush=True)
    return path, task_id, payload


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--prompt", required=True)
    parser.add_argument("--first", required=True)
    parser.add_argument("--last")
    parser.add_argument("--out", required=True)
    parser.add_argument("--model", default="MiniMax-H3")
    parser.add_argument("--resolution", default="768P")
    parser.add_argument("--duration", type=int, default=4)
    parser.add_argument("--ratio", default="adaptive")
    args = parser.parse_args()
    path, task_id, payload = generate(args.prompt, args.first, args.last, args.out,
                                      model=args.model, resolution=args.resolution,
                                      duration=args.duration, ratio=args.ratio)
    print(json.dumps({"path": str(path), "task_id": task_id,
                      "payload": {k: v for k, v in payload.items() if k != "content"}},
                     ensure_ascii=False))


if __name__ == "__main__":
    sys.exit(main())
