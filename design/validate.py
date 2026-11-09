#!/usr/bin/env python3
"""Validate local design sources. Does not launch a browser or test macOS behavior."""
from hashlib import sha256
from html.parser import HTMLParser
from pathlib import Path
import re
import subprocess
import sys
from urllib.parse import unquote, urlsplit
import xml.etree.ElementTree as ET

ROOT = Path(__file__).resolve().parents[1]
PROTO = ROOT / "design/prototype"
failures = []


def check(label, action):
    try:
        detail = action()
        print(f"PASS {label}" + (f": {detail}" if detail else ""))
    except (AssertionError, OSError, ValueError, ET.ParseError) as error:
        failures.append(label)
        print(f"FAIL {label}: {error}")


def require(condition, message):
    if not condition:
        raise AssertionError(message)


def local_target(base, url):
    parsed = urlsplit(url)
    require(not parsed.scheme and not parsed.netloc, f"non-local resource: {url}")
    target = (base / unquote(parsed.path)).resolve()
    require(target.is_file(), f"missing resource: {target}")
    return target


class Page(HTMLParser):
    def __init__(self):
        super().__init__()
        self.ids, self.options, self.resources, self.csp = [], [], [], ""

    def handle_starttag(self, tag, attrs):
        attrs = dict(attrs)
        if "id" in attrs:
            self.ids.append(attrs["id"])
        if tag == "option":
            self.options.append(attrs.get("value"))
        if tag == "script":
            require("src" in attrs, "inline script is not permitted")
        for key in ("src", "href"):
            if key in attrs:
                self.resources.append(attrs[key])
        if tag == "meta" and attrs.get("http-equiv", "").lower() == "content-security-policy":
            self.csp = attrs.get("content", "")


def html_resources():
    page = Page()
    page.feed((PROTO / "index.html").read_text())
    require(len(page.ids) == len(set(page.ids)), "duplicate static IDs")
    for url in page.resources:
        local_target(PROTO, url)
    js = (PROTO / "prototype.js").read_text()
    for dom_id in re.findall(r"\$\('([^']+)'\)", js):
        # Input nodes are generated in the view templates, not in index.html.
        require(dom_id in page.ids or f'id="{dom_id}"' in js, f"unknown DOM ID: {dom_id}")
    states = {"start", "first", "focus", "overtime", "paused", "outside", "limited",
              "unverified", "history", "empty", "recovery", "media-error", "long", "multi"}
    require(set(page.options) == states, "review state inventory drifted")
    require("connect-src 'none'" in page.csp and "media-src 'none'" in page.csp,
            "prototype must block external connections and audio/video")
    return f"{len(page.resources)} resources; {len(states)} review states; unique static IDs"


def illustrations():
    for scene in ("forest", "beach", "snow", "meadow"):
        path = PROTO / "assets" / f"{scene}.svg"
        root = ET.parse(path).getroot()
        require(root.tag.endswith("svg"), f"not SVG: {path}")
        require("viewBox" in root.attrib, f"missing viewBox: {path}")
        for element in root.iter():
            require(not element.tag.endswith("script"), "script in illustration")
            for key, value in element.attrib.items():
                if key.endswith("href"):
                    require(value.startswith("#"), f"external SVG resource: {value}")
    css = (PROTO / "prototype.css").read_text()
    for url in re.findall(r"url\(['\"]?([^)'\"]+)", css):
        local_target(PROTO, url)
    return "4 original SVGs parsed; CSS image references exist"


def script_syntax():
    result = subprocess.run(["node", "--check", str(PROTO / "prototype.js")],
                            text=True, capture_output=True, check=False)
    require(result.returncode == 0, result.stderr.strip())
    # A source guard, not a sandbox audit or a substitute for runtime inspection.
    js = (PROTO / "prototype.js").read_text()
    forbidden = r"\b(?:fetch|XMLHttpRequest|WebSocket|EventSource|Audio|eval)\s*\(|\b(?:localStorage|sessionStorage|indexedDB)\b|navigator\.(?:sendBeacon|mediaDevices)|https?://"
    require(not re.search(forbidden, js), "unexpected network/storage/audio/evaluation API")
    return "node --check; no listed external or persistent APIs in source"


def documentation():
    doc = (ROOT / "docs/产品交互设计.md").read_text()
    for prefix, count in (("R", 16), ("NX", 12), ("S", 16)):
        found = set(re.findall(rf"\| ({prefix}\d\d)(?:\s|\|)", doc))
        required = {f"{prefix}{i:02d}" for i in range(1, count + 1)}
        require(found == required, f"{prefix} coverage rows missing: {sorted(required-found)}")
    files = [ROOT / "docs/产品交互设计.md", ROOT / "design/README.md", ROOT / "docs/苹果风格设计复核.md", ROOT / "docs/v0.2原型验证记录.md"]
    count = 0
    for path in files:
        for url in re.findall(r"(?<!!)\[[^\]]*\]\(([^)]+)\)", path.read_text()):
            if urlsplit(url).scheme or url.startswith("#"):
                continue
            local_target(path.parent, url)
            count += 1
    spec = (ROOT / ".harness/changes/2026-10-22-focus-design/spec.md").read_text()
    design = spec.split("## Design\n", 1)[1].split("\n## Related", 1)[0]
    require("方案 A" in design and "方案 B" in design and "Gate 2" in design,
            "design options or gate boundary missing")
    return f"R01–R16, NX01–NX12, S01–S16 present; {count} local document links exist (not opened)"


def original_unchanged():
    digest = sha256((ROOT / "产品原始需求.md").read_bytes()).hexdigest()
    require(digest == "5c4d9bd0305fef08efa58dfc47aa48e76d5c1bdc68ab52d412abe7aa074cf21c",
            "original requirements changed")
    return digest


def solid_text_contrast():
    """Check fallback solid colors only; not compositing, screenshots or system accents."""
    css = (PROTO / "tokens.css").read_text()
    base = css.split(":root {", 1)[1].split("}", 1)[0]
    dark = re.search(r':root\[data-theme="dark"\]\s*\{([^}]+)\}', css).group(1)

    def colors(block):
        return dict(re.findall(r"(--[\w-]+)\s*:\s*(#[0-9a-fA-F]{6})\s*;", block))

    def luminance(value):
        channels = [int(value[i:i + 2], 16) / 255 for i in (1, 3, 5)]
        linear = [c / 12.92 if c <= .04045 else ((c + .055) / 1.055) ** 2.4 for c in channels]
        return sum(c * weight for c, weight in zip(linear, (.2126, .7152, .0722)))

    results = []
    for mode, values in (("light", colors(base)), ("dark", colors(base) | colors(dark))):
        pairs = [(fg, bg) for fg in ("--text", "--secondary", "--tertiary")
                 for bg in ("--window", "--surface")]
        pairs += [("--accent-text", "--accent"), ("--scene-label-text", "--scene-label-background")]
        for fg, bg in pairs:
            a, b = sorted((luminance(values[fg]), luminance(values[bg])))
            ratio = (b + .05) / (a + .05)
            require(ratio >= 4.5, f"{mode} {fg}/{bg}: {ratio:.2f}:1 below 4.5:1")
            results.append(ratio)
    return f"{len(results)} fallback solid pairs; minimum {min(results):.2f}:1; no image/material/system-accent claim"


if __name__ == "__main__":
    check("HTML references and demo boundaries", html_resources)
    check("illustrations and CSS references", illustrations)
    check("JavaScript syntax and source guard", script_syntax)
    check("design document coverage and local links", documentation)
    check("original requirements preserved", original_unchanged)
    check("solid text color contrast", solid_text_contrast)
    print("Static source checks only. Browser interaction, screenshots, accessibility and native behavior remain unverified.")
    sys.exit(1 if failures else 0)
