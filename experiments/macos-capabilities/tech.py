#!/usr/bin/env python3
"""Isolated, local-only capability driver. Nonzero/pending results are intentional evidence."""
import argparse
import datetime as dt
import hashlib
import json
import os
from pathlib import Path
import plistlib
import re
import secrets
import shlex
import shutil
import subprocess
import sys
import time

ROOT = Path(__file__).resolve().parent
RUNS = ROOT / ".runs"
BUILD = ROOT / ".build"
RUN_PATTERN = re.compile(r"run-\d{8}T\d{6}-[0-9a-f]{8}\Z")
MANUAL_ITEMS = ("notification-popup", "notification-sound", "notification-denied", "outside-choice", "lock", "sleep", "ime-native", "ime-web", "keyboard", "appearance", "voiceover")


def write(path, value):
    path.parent.mkdir(mode=0o700, parents=True, exist_ok=True)
    temp = path.with_name(path.name + ".tmp")
    with os.fdopen(os.open(temp, os.O_WRONLY | os.O_CREAT | os.O_TRUNC | os.O_NOFOLLOW, 0o600), "w") as f:
        json.dump(value, f, ensure_ascii=False, indent=2)
        f.write("\n")
        f.flush()
        os.fsync(f.fileno())
    temp.replace(path)


def environment():
    return dict(os.environ, GOTOOLCHAIN="local", GOOS="darwin", GOARCH="arm64",
                CGO_ENABLED="0", GOWORK="off", GOMODCACHE=str(ROOT / ".cache/mod"),
                GOCACHE=str(ROOT / ".cache/build"))


def command(args, *, capture=False, env=None):
    print("$ " + " ".join(map(str, args)), flush=True)
    proc = subprocess.run(args, cwd=ROOT, env=env or environment(), text=True,
                          stdout=subprocess.PIPE if capture else None, check=True)
    return proc.stdout.strip() if capture else None


def run_dir(name):
    if name == "latest":
        pointer = RUNS / "latest.json"
        if pointer.is_symlink():
            raise ValueError("symlink latest is forbidden")
        name = json.loads(pointer.read_text())["runID"]
    if not isinstance(name, str) or not RUN_PATTERN.fullmatch(name):
        raise ValueError("invalid run ID")
    path = RUNS.resolve() / name
    if path.resolve() != path or not path.is_dir():
        raise ValueError("run path must be a real directory under .runs")
    return path


def read(path):
    if path.is_symlink() or not path.is_file() or path.stat().st_size > 2_000_000:
        raise ValueError("invalid evidence file: " + path.name)
    return json.loads(path.read_text())


def bundle(name):
    return BUILD / "bundles" / (name + ".app")


def binary(name):
    return bundle(name) / "Contents/MacOS" / name


def handshake_matches(state, token, name):
    ref = state.get("ref", {})
    identifier = {"Host":"local.focusprobe.host", "FixtureA":"local.focusprobe.fixture.a", "FixtureB":"local.focusprobe.fixture.b"}[name]
    return (state.get("schemaVersion") == 1 and ref.get("token") == token
            and isinstance(ref.get("pid"), int) and ref["pid"] > 0
            and ref.get("launched", 0) > 0 and ref.get("bundleID") == identifier
            and ref.get("path") == str(bundle(name).resolve()))


def framework_pin_matches(info):
    return (info.get("Path")=="github.com/egoist/mygo" and info.get("Version")=="v0.6.2"
            and not info.get("Replace"))


def preflight():
    if sys.platform != "darwin":
        raise ValueError("only current macOS/arm64 is in this run scope")
    for name in ("go", "file", "xcrun"):
        if not shutil.which(name):
            raise ValueError("missing tool: " + name)
    # Read the user's effective defaults first, rather than hiding them with our target env.
    defaults = json.loads(command(["go", "env", "-json", "GOVERSION", "GOOS", "GOARCH", "GOHOSTARCH", "GOROOT"], capture=True, env=dict(os.environ, GOTOOLCHAIN="local", GOWORK="off")))
    if (defaults["GOVERSION"], defaults["GOOS"], defaults["GOARCH"], defaults["GOHOSTARCH"]) != ("go1.27.3", "darwin", "arm64", "arm64"):
        raise ValueError("toolchain baseline changed: " + json.dumps(defaults))
    module = json.loads(command(["go","list","-m","-json","github.com/egoist/mygo"],capture=True))
    if not framework_pin_matches(module):
        raise ValueError("framework must be pinned to v0.6.2 without replace")
    data = dict(schemaVersion=1, at=dt.datetime.now(dt.timezone.utc).isoformat(),
                go=defaults, system=command(["sw_vers"], capture=True),
                machine=command(["uname", "-m"], capture=True),
                goBinary=command(["file", str(Path(defaults["GOROOT"]) / "bin/go")], capture=True),
                sdk=command(["xcrun", "--show-sdk-version"], capture=True),
                framework="github.com/egoist/mygo@v0.6.2", target="darwin/arm64", cgo=False)
    write(BUILD / "preflight.json", data)
    print("PASS: local Go version/architecture and pinned declaration; no GUI/permission request")
    return 0


def live_runs():
    if not RUNS.exists():
        return []
    result = []
    for path in RUNS.glob("run-*"):
        if path.is_symlink():
            raise ValueError("symlink in run root")
        if not (path / "launch.json").exists():
            continue
        launch = read(path / "launch.json")
        manifest = read(path / "manifest.json")
        for child in launch["children"]:
            permission_denied = False
            try:
                os.kill(child["pid"], 0)
            except ProcessLookupError:
                continue
            except PermissionError:
                # A reused PID can belong to a protected system process.
                # Existence still requires the same identity checks below.
                permission_denied = True
            # PID existence is not ownership: an old run's PID can now name an
            # unrelated system process. Only inspect; never signal a reused PID.
            name = child["name"]
            if name not in ("Host", "FixtureA", "FixtureB"):
                raise ValueError("invalid fixture child")
            proc = subprocess.run(["ps", "-p", str(child["pid"]), "-o", "args="],capture_output=True,text=True)
            if proc.returncode != 0 or not proc.stdout.strip():
                if permission_denied:
                    raise RuntimeError("进程存在但归属不可读取，保留测试目录，未发送清理信号")
                continue
            argv = shlex.split(proc.stdout.strip())
            if not argv or Path(argv[0]).resolve() != (bundle(name)/"Contents/MacOS"/name).resolve():
                continue
            def option(key):
                return argv[argv.index(key)+1] if argv.count(key)==1 and argv.index(key)+1<len(argv) else None
            if option("--run")==path.name and option("--token")==manifest["token"]:
                result.append(path.name)
                break
    return result


def build():
    if live_runs():
        raise ValueError("stop existing test bundles before rebuilding")
    preflight()
    command(["go", "mod", "download"])
    command(["go", "mod", "verify"])
    artifacts = []
    for name, target, identifier in [("Host", "./cmd/host", "local.focusprobe.host"),
            ("FixtureA", "./cmd/fixture", "local.focusprobe.fixture.a"),
            ("FixtureB", "./cmd/fixture", "local.focusprobe.fixture.b")]:
        exe = binary(name)
        exe.parent.mkdir(parents=True, exist_ok=True)
        command(["go", "build", "-mod=readonly", "-trimpath", "-o", str(exe), target])
        info = dict(CFBundleIdentifier=identifier, CFBundleExecutable=name, CFBundleName=name,
                    CFBundlePackageType="APPL", CFBundleVersion="1", CFBundleShortVersionString="0.1",
                    LSMinimumSystemVersion="15.6", NSHighResolutionCapable=True)
        with (exe.parent.parent / "Info.plist").open("wb") as f:
            plistlib.dump(info, f)
        identity = command(["file", str(exe)], capture=True)
        modules = command(["go", "version", "-m", str(exe)], capture=True)
        if "arm64" not in identity or "github.com/egoist/mygo\tv0.6.2" not in modules:
            raise ValueError("wrong built target or framework")
        artifacts.append(dict(name=name, bundleID=identifier, sha256=hashlib.sha256(exe.read_bytes()).hexdigest(), file=identity, modules=modules))
    write(BUILD / "artifacts.json", dict(schemaVersion=1, artifacts=artifacts))
    print("PASS: three local arm64 bundles; no extra codesign, install or launch")
    return 0


def tests():
    command(["go", "test", "-count=1", "./..."])
    command(["go", "vet", "./..."])
    command([sys.executable, "-m", "unittest", "discover", "-p", "test_*.py", "-v"])
    return 0


def wait_file(path, predicate=lambda value: True, children=(), timeout=35):
    deadline = time.monotonic() + timeout
    while time.monotonic() < deadline:
        for proc in children:
            if proc.poll() is not None:
                raise RuntimeError("test process exited; inspect run process logs")
        if path.is_file():
            value = read(path)
            if predicate(value):
                return value
        time.sleep(0.1)  # Waiting on an explicit handshake condition, not UI timing evidence.
    raise TimeoutError("missing observed condition: " + path.name)


def initial_matrix():
    return dict(schemaVersion=1, channels=[dict(channel=name,
        control="insufficient", read="untested", effective="untested", restore="untested",
        popup="untested", sound="untested", evidenceKind="source",
        source="docs/技术准备核查.md", limitation=limit)
        for name, limit in [
            ("ordinary", "fixture notifications are a source, not cross-app suppression"),
            ("time-sensitive", "no approved special-channel trigger in this run"),
            ("communication", "no communication entitlement/provisioning"),
            ("system", "no system alert deliberately triggered"),
            ("critical", "no critical-alert entitlement requested"),
            ("in-app", "synthetic fixture prompt cannot prove third-party behavior")]])


def notification_audit():
    if live_runs():
        raise ValueError("stop existing owned test bundles before the read-only audit")
    artifacts = read(BUILD / "artifacts.json")
    for artifact in artifacts["artifacts"]:
        if hashlib.sha256(binary(artifact["name"]).read_bytes()).hexdigest() != artifact["sha256"]:
            raise ValueError("artifact changed since build")
    RUNS.mkdir(mode=0o700, exist_ok=True)
    run = "run-" + dt.datetime.now().strftime("%Y%m%dT%H%M%S") + "-" + secrets.token_hex(4)
    path = RUNS / run
    path.mkdir(mode=0o700)
    token = secrets.token_hex(16)
    write(path / "manifest.json", dict(read(BUILD / "preflight.json"), runID=run, token=token, kind="read-only-notification-audit", artifacts=artifacts))
    # An audit must not replace the full fixture suite's latest run.
    write(RUNS / "latest-notification.json", dict(runID=run))
    applications, children = {}, []
    for name, label in (("Host", "Host"), ("FixtureA", "A"), ("FixtureB", "B")):
        log_path = path / (label + "-NotificationAudit.app.log")
        argv = ["/usr/bin/open", "-n", "-W", str(bundle(name)), "--stdout", str(log_path), "--stderr", str(log_path), "--args", "--run", run, "--token", token, "--notification-audit"]
        if name != "Host":
            argv += ["--label", label]
        launcher = subprocess.Popen(argv, cwd=ROOT, env=environment(), stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
        data = wait_file(path / (label + "-notification-audit.json"), children=[launcher], timeout=25)
        if not handshake_matches(dict(schemaVersion=1, ref=data.get("ref", {})), token, name):
            raise ValueError("notification audit identity mismatch")
        children.append(dict(name=name, pid=data["ref"]["pid"], launcherPID=launcher.pid))
        write(path / "launch.json", dict(schemaVersion=1, children=children))
        try:
            launcher.wait(timeout=5)
        except subprocess.TimeoutExpired:
            print("PENDING: owned audit process has not exited")
            return 4
        if data.get("error") or data.get("facts", {}).get("scope") != "current-app":
            write(path / "notification-audit.json", dict(schemaVersion=1, applications=applications, error=data.get("error", "unexpected scope")))
            return 1
        applications[label] = data
    aggregate = dict(schemaVersion=1, evidenceKind="automated-real", applications=applications)
    write(path / "notification-audit.json", aggregate)
    print(json.dumps({label: dict(notifications=d["facts"]["notifications"]["authorization"], focus=d["facts"]["focus"], accessibilityTrusted=d["facts"]["accessibilityTrustedForThisProcess"]) for label,d in applications.items()}, ensure_ascii=False, indent=2))
    print("audit:", path)
    print("LIMITED: each app read its own settings; global suppression and Focus switching were not tested")
    return 3


def probe(suite, choice_diagnostic=False, restoration_diagnostic=False):
    if suite != "approved":
        raise ValueError("unknown suite")
    if live_runs():
        raise ValueError("a run is still alive; use its Stop and clean control")
    artifacts = read(BUILD / "artifacts.json")
    for art in artifacts["artifacts"]:
        if hashlib.sha256(binary(art["name"]).read_bytes()).hexdigest() != art["sha256"]:
            raise ValueError("artifact changed since build")
    RUNS.mkdir(mode=0o700, exist_ok=True)
    run = "run-" + dt.datetime.now().strftime("%Y%m%dT%H%M%S") + "-" + secrets.token_hex(4)
    path = RUNS / run
    path.mkdir(mode=0o700)
    token = secrets.token_hex(16)
    write(path / "manifest.json", dict(read(BUILD / "preflight.json"), runID=run, token=token, artifacts=artifacts))
    write(path / "matrix.json", initial_matrix())
    write(path / "observations.json", dict(schemaVersion=1, items={name:dict(result="pending", evidenceKind="manual") for name in MANUAL_ITEMS}))
    write(RUNS / "latest.json", dict(runID=run))
    children = []
    identities = []
    logs = []
    try:
        for name, label in [("FixtureA", "A"), ("FixtureB", "B")]:
            f = (path / (name + ".log")).open("w");logs.append(f)
            p = subprocess.Popen(["/usr/bin/open", "-n", "-W", str(bundle(name)), "--stdout", str(path / (name + ".app.log")), "--stderr", str(path / (name + ".app.log")), "--args", "--run", run, "--token", token, "--label", label], cwd=ROOT, env=environment(), stdout=f, stderr=subprocess.STDOUT)
            children.append(p)
            state = wait_file(path / (label + ".json"), lambda s: handshake_matches(s, token, name), children)
            identities.append(dict(name=name, pid=state["ref"]["pid"], launcherPID=p.pid))
            write(path / "launch.json", dict(schemaVersion=1, children=identities))
        f = (path / "Host.log").open("w");logs.append(f)
        p = subprocess.Popen(["/usr/bin/open", "-n", "-W", str(bundle("Host")), "--stdout", str(path / "Host.app.log"), "--stderr", str(path / "Host.app.log"), "--args", "--run", run, "--token", token, "--auto"] + (["--choice-diagnostic"] if choice_diagnostic else []) + (["--restoration-diagnostic"] if restoration_diagnostic else []), cwd=ROOT, env=environment(), stdout=f, stderr=subprocess.STDOUT)
        children.append(p)
        state = wait_file(path / "Host.json", lambda s: handshake_matches(s, token, "Host"), children)
        identities.append(dict(name="Host", pid=state["ref"]["pid"], launcherPID=p.pid))
        write(path / "launch.json", dict(schemaVersion=1, children=identities))
        result = wait_file(path / "automated.json", children=children, timeout=65)
        panel_ok = not choice_diagnostic or valid_choice_observations(read(path / "choice-diagnostic.json"))
        restore_ok = not restoration_diagnostic or valid_restoration_observations(result.get("restoration"))
        failed = bool(result.get("error") or not result.get("cleanup") or not panel_ok or not restore_ok)
        write(path / "probe-status.json", dict(schemaVersion=1, result="failed" if failed else "pending", automated=result))
        print(json.dumps(result, ensure_ascii=False, indent=2))
        report(path)
        print("PENDING: test windows remain available for manual steps; use ‘停止并清理测试程序’ when finished.")
        print("run:", path)
        if choice_diagnostic:
            print("Panel visibility:", "PASS (three actual workspace activations)" if panel_ok else "FAIL")
        return 1 if failed else 4
    except Exception as error:
        write(path / "probe-status.json", dict(schemaVersion=1, result="failed", error=str(error)))
        # Only stop processes created by this invocation; never resolve arbitrary PIDs to kill.
        write(path / "stop.json", dict(token=token, stop=True))
        for p in reversed(children):
            if p.poll() is None:
                p.terminate()
                try:
                    p.wait(timeout=5)
                except subprocess.TimeoutExpired:
                    print("cleanup pending for owned process", p.pid)
        report(path)
        raise
    finally:
        for f in logs:
            f.close()


def shot(path):
    if command([str(binary("Host")), "--capture-check"], capture=True) != "true":
        write(path / "screenshots.json", dict(schemaVersion=1, result="pending", reason="capture permission unavailable; no prompt requested", images=[]))
        print("PENDING: no window capture permission; no desktop fallback or permission prompt")
        return 4
    manifest = read(path / "manifest.json")
    images = []
    for label in ("Host", "A", "B"):
        state = read(path / (label + ".json"))
        if state["ref"]["token"] != manifest["token"]:
            raise ValueError("screenshot identity changed")
        for window in state["windows"]:
            if int(command([str(binary("Host")), "--owner", str(window)], capture=True)) != state["ref"]["pid"]:
                raise ValueError("window owner changed")
            image = path / (label + "-" + str(window) + ".png")
            command(["/usr/sbin/screencapture", "-x", "-l", str(window), str(image)])
            images.append(dict(window=window, fixture=label, path=image.name, sha256=hashlib.sha256(image.read_bytes()).hexdigest()))
    write(path / "screenshots.json", dict(schemaVersion=1, result="observed" if images else "pending", images=images))
    return 0 if images else 4


def report(path):
    manifest = read(path / "manifest.json")
    auto = read(path / "automated.json") if (path / "automated.json").is_file() else {"result":"pending"}
    observations = read(path / "observations.json")
    matrix = read(path / "matrix.json")
    journal = read(path / "journal.json") if (path / "journal.json").is_file() else {"entries":[]}
    pending = [x for x in journal["entries"] if x["status"] in ("pending", "applied")]
    lines = ["# 本地技术探针结果", "", "run: " + manifest["runID"], "", "自动执行与人工观察分开；未测不代表可行。", "", "## 实际自动采样", "", "```json", json.dumps(auto, ensure_ascii=False, indent=2), "```", "", "## 通知矩阵", "", "| 渠道 | 控制 | 读取 | 生效 | 恢复 | 弹窗 | 声音 |", "|---|---|---|---|---|---|---|"]
    for row in matrix["channels"]:
        lines.append("|" + "|".join(row[k] for k in ("channel","control","read","effective","restore","popup","sound")) + "|")
    lines += ["", "## 人工观察", ""]
    lines += [f"- {k}: {v['result']}" for k,v in observations["items"].items()]
    lines += ["", "## 清理", "", f"尚未释放的 journal 条目：{len(pending)}。", "停止并清理按钮只操作本次测试程序。", "", "通知全覆盖、真实锁屏/睡眠、IME、VoiceOver、系统外观仍需证据；不能以构建或上述自动采样替代。"]
    (path / "report.md").write_text("\n".join(lines) + "\n")


def valid_restoration_observations(result):
    return (isinstance(result, dict) and not result.get("error") and not result.get("setupCleanupError")
            and all(result.get(k) is True for k in ("originallyHiddenPreserved", "repeatCleanupReleased", "externalChangeAbandoned", "externalVisiblePreserved", "allowancePreservedOnResume"))
            and result.get("pendingAtEnd") == 0)


def valid_choice_observations(rows):
    if not isinstance(rows, list) or len(rows) != 3:
        return False
    for row in rows:
        if not isinstance(row, dict) or row.get("error") or row.get("eventSource") != "NSWorkspace-real" or row.get("hostActiveBefore") is not False:
            return False
        for key in ("before", "after"):
            window = row.get(key, {})
            if not isinstance(window, dict):
                return False
            occlusion = window.get("occlusionState")
            if window.get("visible") is not True or window.get("onActiveSpace") is not True or not isinstance(occlusion, int) or not occlusion & 2:
                return False
    return True


def check(path):
    for name in ("manifest.json","matrix.json","observations.json","probe-status.json","events.jsonl","journal.json"):
        p = path / name
        if p.is_symlink() or not p.is_file():
            raise ValueError("missing required evidence: " + name)
    if read(path / "manifest.json")["schemaVersion"] != 1:
        raise ValueError("unsupported manifest schema")
    matrix = read(path / "matrix.json")
    if [r["channel"] for r in matrix["channels"]] != [r["channel"] for r in initial_matrix()["channels"]]:
        raise ValueError("incomplete channel matrix")
    verdicts = {"untested","insufficient","limited","unavailable","observed","feasible"}
    for row in matrix["channels"]:
        for key in ("control","read","effective","restore","popup","sound"):
            if row.get(key) not in verdicts:
                raise ValueError("invalid verdict: " + row["channel"] + "/" + key)
        if row.get("evidenceKind") not in ("source","automated-real","automated-synthetic","manual"):
            raise ValueError("invalid evidence source")
    observation_file = read(path / "observations.json")
    if observation_file.get("schemaVersion") != 1 or set(observation_file.get("items", {})) != set(MANUAL_ITEMS):
        raise ValueError("incomplete manual observation matrix")
    for item in observation_file["items"].values():
        if item.get("result") not in ("pending","observed","limited","failed"):
            raise ValueError("invalid observation result")
        if item["result"] != "pending" and not item.get("source"):
            raise ValueError("observations require an attributable source")
    report(path)
    auto = read(path / "automated.json")
    if "restoration" in auto and not valid_restoration_observations(auto["restoration"]):
        print("FAIL: restoration observations incomplete or failed")
        return 1
    if (path / "choice-diagnostic.json").exists() and not valid_choice_observations(read(path / "choice-diagnostic.json")):
        print("FAIL: panel visibility observations incomplete or failed")
        return 1
    observations = read(path / "observations.json")["items"]
    if any(item['result']=='failed' for item in observations.values()):
        print("FAIL: an attributable manual observation failed")
        return 1
    pending = any(item["result"] == "pending" for item in observations.values())
    pending |= any(row[k] in ("untested","insufficient") for row in matrix["channels"] for k in ("control","read","effective","restore","popup","sound"))
    pending |= any(e["status"] in ("pending","applied") for e in read(path / "journal.json")["entries"])
    if auto.get("error") or not all(auto.get(k) is True for k in ("cleanup","hideB","backgroundProgress","webReady")):
        print("FAIL: automated sampling/cleanup failed")
        return 1
    print("PENDING: manual/system evidence incomplete" if pending else "PASS: evidence complete")
    return 4 if pending else 0


def main():
    p = argparse.ArgumentParser(description=__doc__)
    p.add_argument("command", choices=["preflight","build","test","probe","shot","check","stop","notifications"])
    p.add_argument("--suite", choices=["approved"])
    p.add_argument("--run", default="latest")
    p.add_argument("--own-windows", action="store_true")
    p.add_argument("--choice-diagnostic", action="store_true")
    p.add_argument("--restoration-diagnostic", action="store_true")
    args = p.parse_args()
    if args.command in ("preflight","build","test"):
        return {"preflight":preflight,"build":build,"test":tests}[args.command]()
    if args.command == "probe":
        return probe(args.suite, args.choice_diagnostic, args.restoration_diagnostic)
    if args.command == "notifications":
        return notification_audit()
    path = run_dir(args.run)
    if args.command == "stop":
        manifest = read(path / "manifest.json")
        write(path / "host-stop.json", dict(token=manifest["token"], stop=True))
        print("Requested owned host cleanup and normal fixture exit:", path.name)
        deadline = time.monotonic() + 15
        while time.monotonic() < deadline:
            if path.name not in live_runs():
                print("PASS: all three owned processes exited")
                return 0
            time.sleep(0.1)
        print("PENDING: owned cleanup has not finished; no process was force-terminated")
        return 4
    if args.command == "shot":
        if not args.own_windows:
            raise ValueError("only explicit own-window capture is allowed")
        return shot(path)
    return check(path)


if __name__ == "__main__":
    try:
        sys.exit(main())
    except subprocess.CalledProcessError as error:
        print("command failed:", error, file=sys.stderr)
        sys.exit(error.returncode)
    except (OSError, ValueError, KeyError, RuntimeError, TimeoutError) as error:
        print(type(error).__name__ + ": " + str(error), file=sys.stderr)
        sys.exit(2 if isinstance(error, ValueError) else 1)
