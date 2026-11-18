#!/usr/bin/env python3
"""Build a universal macOS app bundle. No install, permission grants or publishing."""
import fcntl, hashlib, json, os, pathlib, plistlib, shutil, subprocess, sys, tempfile
from contextlib import contextmanager
from icon import verify as verify_icon
ROOT = pathlib.Path(__file__).resolve().parents[1]
APP_NAME = "归径"
BUNDLE_ID = "com.yunsiweilai.zhuanzhuli"
APP = ROOT / ".build" / f"{APP_NAME}.app"
ICON_SOURCE = pathlib.Path("assets/artwork/guijing-icon.svg")
VERSION = "0.4.0"
BUILD_NUMBER = "23"
TARGETS = {"arm64": "arm64", "amd64": "x86_64"}
PACKAGE_PLATFORM = "macOS-universal"
def digest(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()
def bundle_files(bundle):
    """The canonical bundle file set. The candidate manifest and every
    comparison against it must use this exact rule. Sorted so the manifest
    stays byte-stable across builds."""
    return {str(p.relative_to(bundle)):digest(p) for p in sorted(bundle.rglob("*")) if p.is_file()}
def release_manifest():
    manifest = json.loads((ROOT/"assets/release-media.json").read_text())
    if manifest.get("version") not in (1, 2) or not manifest.get("scenes"):
        raise RuntimeError("missing complete release scenes")
    notices = manifest.get("notices", [])
    if not isinstance(notices, list):
        raise RuntimeError("release notices must be a list")
    files = notices + [manifest["guide"]]
    if manifest["version"] == 2:
        ambiences = manifest.get("ambiences", [])
        if {item.get("id") for item in ambiences} != {"rain", "birds", "waves", "stream"} or len(ambiences) != 4:
            raise RuntimeError("release requires exactly four natural recordings")
        files += [item["audio"] for item in ambiences]
    for scene in manifest["scenes"]:
        files += [scene[role] for role in ("preview", "background", "entry", "work")]
    targets = {}
    for item in files:
        if (not isinstance(item, dict) or
                any(not isinstance(item.get(key), str) or not item[key] for key in ("source", "path", "sha256"))):
            raise RuntimeError("invalid release resource entry")
        source = ROOT / item["source"]
        relative = pathlib.PurePosixPath(item["path"])
        if relative.is_absolute() or ".." in relative.parts or relative.parts[0] != "media" or source.resolve() != source.absolute() or not source.is_relative_to(ROOT/"assets"):
            raise RuntimeError("invalid release resource path")
        if digest(source) != item["sha256"]:
            raise RuntimeError("release media changed: " + item["source"])
        if item["path"] in targets and targets[item["path"]] != item:
            raise RuntimeError("conflicting resource role")
        targets[item["path"]] = item
    return manifest, targets
def runtime_manifest(manifest):
    runtime=json.loads(json.dumps(manifest))
    # Notices are build-only resources; source locations never enter runtime.
    runtime.pop("notices",None)
    runtime["guide"].pop("source",None)
    for ambience in runtime.get("ambiences",[]):ambience["audio"].pop("source",None)
    for scene in runtime["scenes"]:
        for role in ("preview","background","entry","work"):scene[role].pop("source",None)
    return runtime
def source_fingerprint():
    files = [ROOT/"go.mod",ROOT/"go.sum",ROOT/"assets/release-media.json",ROOT/"internal/media/scenes.json"]
    for directory in ("cmd", "internal", "pkg", "scripts"):
        files += sorted(p for p in (ROOT/directory).rglob("*") if p.is_file() and p.suffix in (".go", ".py"))
    files.append(ROOT/ICON_SOURCE)
    sources = {str(p.relative_to(ROOT)):digest(p) for p in files}
    sources["toolchain"] = toolchain()
    return hashlib.sha256(json.dumps(sources,sort_keys=True).encode()).hexdigest(), sources
def toolchain():
    import PIL
    environment=dict(os.environ,GOTOOLCHAIN="local",GOWORK="off")
    version=subprocess.check_output(["go","env","-json","GOVERSION","GOHOSTARCH"],env=environment,text=True)
    return dict(go=json.loads(version),sdk=subprocess.check_output(["xcrun","--show-sdk-version"],text=True).strip(),python=sys.version.split()[0],pillow=PIL.__version__,target="darwin/universal",architectures=list(TARGETS.values()),cgo=False,trimpath=True)
def run(args, **kwargs):
    subprocess.run(args, cwd=ROOT, check=True, **kwargs)

def signing_profile(directory):
    path = directory / "signing-identity.json"
    if not path.exists():
        return None
    profile = json.loads(path.read_text())
    if not isinstance(profile, dict) or any(not isinstance(profile.get(key), str) or not profile[key]
                                              for key in ("identity", "requirement")) or profile["identity"] == "-":
        raise RuntimeError("invalid persistent signing identity profile")
    return profile

def signing_identity(profile):
    identity = (os.environ.get("FOCUS_CODESIGN_IDENTITY", "").strip()
                or (profile["identity"] if profile else "-"))
    if identity == "-" and (profile or os.environ.get("FOCUS_REQUIRE_STABLE_SIGNING") == "1"):
        raise RuntimeError("stable signing requires a valid Developer ID Application signing identity; ad-hoc is disabled")
    return identity

def verify_signing_requirement(app, requirement):
    run(["codesign", "--verify", "--strict", "--all-architectures", "-R", "=" + requirement, str(app)])

def designated_requirement(app, architecture, identifier):
    result = subprocess.run(["codesign", "--display", "--arch", architecture, "-r-", str(app)],
                            check=True, capture_output=True, text=True)
    requirements = [line.removeprefix("designated => ") for line in (result.stdout + result.stderr).splitlines()
                    if line.startswith("designated => ")]
    if (len(requirements) != 1 or f'identifier "{identifier}"' not in requirements[0]
            or "certificate " not in requirements[0] or "cdhash " in requirements[0]):
        raise RuntimeError("signature does not contain a stable certificate and bundle identifier requirement")
    return requirements[0]

def sign_app(app, identifier, identity, profile):
    args=["codesign", "--force", "--sign", identity, "--identifier", identifier]
    if identity != "-":
        args += ["--timestamp", "--options", "runtime"]
    run(args + [str(app)])
    run(["codesign", "--verify", "--strict", str(app)])
    if identity == "-":
        return None
    requirements = {designated_requirement(app, arch, identifier) for arch in TARGETS.values()}
    if len(requirements) != 1:
        raise RuntimeError("application architectures do not use the same designated requirement")
    requirement = requirements.pop()
    if profile and profile["requirement"] != requirement:
        raise RuntimeError("signing identity changed; refusing to replace the existing upgrade identity")
    verify_signing_requirement(app, requirement)
    return dict(identity=identity, requirement=requirement)
def verify_architectures(exe):
    actual = subprocess.check_output(["lipo","-archs",str(exe)],cwd=ROOT,text=True).split()
    if set(actual) != set(TARGETS.values()):
        raise RuntimeError("expected arm64 and x86_64 in the application executable; found " + ", ".join(actual))

def build_executable(exe,build_id,staging):
    values = {"main.buildVersion": VERSION, "main.buildNumber": BUILD_NUMBER, "main.buildID": build_id, "main.appDisplayName": APP_NAME}
    ldflags = " ".join(f"-X {key}={value}" for key, value in values.items())
    env = dict(os.environ, GOTOOLCHAIN="local", GOWORK="off", GOOS="darwin", CGO_ENABLED="0",
               GOARM64="v8.0", GOAMD64="v1",
               GOCACHE=str(ROOT/".cache/go-build"),
               GOMODCACHE=str(ROOT/"experiments/macos-capabilities/.cache/mod"))
    slices = []
    for arch in TARGETS:
        binary = str(staging / ("Focus-" + arch))
        run(["go","build","-trimpath","-ldflags",ldflags,"-o",binary,"./cmd/focus"], env=dict(env, GOARCH=arch))
        slices.append(binary)
    run(["lipo","-create",*slices,"-output",str(exe)])
    verify_architectures(exe)
def build_candidate():
    if sys.platform != "darwin":
        raise SystemExit("This build requires macOS.")
    profile = signing_profile(APP.parent)
    identity = signing_identity(profile)
    manifest, targets = release_manifest()
    build_id, sources = source_fingerprint()
    APP.parent.mkdir(exist_ok=True)
    with tempfile.TemporaryDirectory(prefix="focus-build-",dir=APP.parent) as temporary:
        build_at(pathlib.Path(temporary),manifest,targets,build_id,sources,identity,profile)
def build_at(staging,manifest,targets,build_id,sources,identity="-",profile=None):
    candidate = staging / APP.name
    icon = staging/"app-icon.png"
    run(["sips", "-s", "format", "png", str(ROOT/ICON_SOURCE), "--out", str(icon)], stdout=subprocess.DEVNULL)
    verify_icon(icon)
    exe = candidate / "Contents/MacOS/Focus"
    exe.parent.mkdir(parents=True, exist_ok=True)
    resources = candidate / "Contents/Resources"
    resources.mkdir(exist_ok=True)
    with tempfile.TemporaryDirectory(prefix="focus-icon-") as temporary:
        iconset = pathlib.Path(temporary) / "AppIcon.iconset"
        iconset.mkdir()
        for size in (16, 32, 128, 256, 512):
            for scale in (1, 2):
                pixels = str(size * scale)
                suffix = "@2x" if scale == 2 else ""
                run(["sips", "-z", pixels, pixels, str(icon), "--out",
                     str(iconset / f"icon_{size}x{size}{suffix}.png")], stdout=subprocess.DEVNULL)
        run(["iconutil", "-c", "icns", str(iconset), "-o", str(resources / "AppIcon.icns")])
    for target, item in targets.items():
        destination = resources/target
        destination.parent.mkdir(parents=True,exist_ok=True)
        shutil.copyfile(ROOT/item["source"],destination)
    runtime=runtime_manifest(manifest)
    (resources/"manifest.json").write_text(json.dumps(runtime,ensure_ascii=False,indent=2)+"\n")
    build_executable(exe,build_id,staging)
    info = dict(CFBundleExecutable="Focus", CFBundleIdentifier=BUNDLE_ID,
                CFBundleName=APP_NAME, CFBundleDisplayName=APP_NAME,
                CFBundleDevelopmentRegion="zh-Hans", CFBundleLocalizations=["zh-Hans"],
                CFBundleIconFile="AppIcon.icns",
                CFBundlePackageType="APPL", CFBundleShortVersionString=VERSION,
                CFBundleVersion=BUILD_NUMBER, FocusBuildID=build_id, LSMinimumSystemVersion="15.0", NSHighResolutionCapable=True,
                NSPrincipalClass="NSApplication", NSFocusStatusUsageDescription="显示专注状态；不表示全面通知保护已生效。")
    (candidate/"Contents/Info.plist").write_bytes(plistlib.dumps(info))
    (resources/"build.json").write_text(json.dumps(dict(version=VERSION,buildNumber=BUILD_NUMBER,buildID=build_id,sources=sources,resources={str(p.relative_to(resources)):digest(p) for p in sorted(resources.rglob("*")) if p.is_file()}),ensure_ascii=False,indent=2)+"\n")
    # Bind the bundle identifier, Info.plist and resources. The Go linker's
    # executable-only signature is not a valid signed application bundle.
    signed=sign_app(candidate,info["CFBundleIdentifier"],identity,profile)
    if source_fingerprint()[0]!=build_id:raise RuntimeError("source inputs changed during build; candidate not published")
    if signed:
        staged_profile=staging/"signing-identity.json"
        staged_profile.write_text(json.dumps(signed,indent=2)+"\n")
        os.replace(staged_profile,APP.parent/staged_profile.name)
    if APP.exists(): shutil.rmtree(APP)
    candidate.rename(APP)
    metadata=dict(version=VERSION,buildID=build_id,signing="certificate" if signed else "ad-hoc",files=bundle_files(APP))
    if signed:
        metadata["signingRequirement"]=signed["requirement"]
    (APP.parent/"candidate.json").write_text(json.dumps(metadata,ensure_ascii=False,indent=2)+"\n")
    print(APP)
@contextmanager
def candidate_lock():
    APP.parent.mkdir(exist_ok=True)
    with (APP.parent/"build.lock").open("a") as lock:
        fcntl.flock(lock,fcntl.LOCK_EX)
        yield
def main():
    with candidate_lock():
        build_candidate()
if __name__ == "__main__":
    main()
