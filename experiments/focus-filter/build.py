#!/usr/bin/env python3
"""Build an isolated native Focus filter sample; never launch or switch Focus."""
import datetime, json, pathlib, plistlib, secrets, subprocess

HERE = pathlib.Path(__file__).resolve().parent
ROOT = HERE.parents[1]
BUILD = ROOT / ".build/focus-filter"
EVIDENCE = ROOT / ".validation/focus-filter"

def main():
    BUILD.mkdir(parents=True, exist_ok=True)
    EVIDENCE.mkdir(parents=True, mode=0o700, exist_ok=True)
    run = datetime.datetime.now().strftime("%Y%m%dT%H%M%S") + "-" + secrets.token_hex(4)
    events = EVIDENCE / run
    events.mkdir(mode=0o700)
    info = dict(CFBundleExecutable="$(EXECUTABLE_NAME)", CFBundleIdentifier="$(PRODUCT_BUNDLE_IDENTIFIER)",
        CFBundleName="FocusFilterProbe", CFBundleDisplayName="Focus Filter 验证",
        CFBundlePackageType="APPL", CFBundleVersion="1", CFBundleShortVersionString="0.1",
        NSHighResolutionCapable=True, LSMinimumSystemVersion="15.0", NSPrincipalClass="NSApplication",
        ProbeEvidenceDirectory=str(events), ProbeRunID=run)
    plist = BUILD / "Info.plist"
    plist.write_bytes(plistlib.dumps(info))
    command = ["xcodebuild", "-project", str(HERE / "FocusFilterProbe.xcodeproj"),
        "-target", "FocusFilterProbe", "-configuration", "Debug", "-sdk", "macosx",
        "ARCHS=arm64", "ONLY_ACTIVE_ARCH=YES", "CODE_SIGN_IDENTITY=-", "CODE_SIGN_STYLE=Manual",
        "DEVELOPMENT_TEAM=", "INFOPLIST_FILE=" + str(plist), "SYMROOT=" + str(BUILD / "products"),
        "OBJROOT=" + str(BUILD / "intermediates"), "build"]
    print("building isolated FocusFilterProbe", flush=True)
    result = subprocess.run(command, cwd=HERE, text=True, stdout=subprocess.PIPE, stderr=subprocess.STDOUT)
    (events / "build.log").write_text(result.stdout)
    if result.returncode:
        print(result.stdout[-18000:])
        raise SystemExit(result.returncode)
    app = BUILD / "products/Debug/FocusFilterProbe.app"
    subprocess.run(["codesign", "--verify", "--strict", str(app)], check=True)
    metadata = list((app / "Contents").rglob("*.appintents"))
    manifest = dict(runID=run, application=str(app), evidence=str(events),
        metadata=[str(p.relative_to(app)) for p in metadata],
        built=True, systemDiscoveryVerified=False, systemCallbackVerified=False)
    (EVIDENCE / "latest.json").write_text(json.dumps(manifest, indent=2))
    print(json.dumps(manifest, indent=2))

if __name__ == "__main__":
    main()
