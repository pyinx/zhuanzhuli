#!/usr/bin/env python3
"""Exercise the shipping app's effects against only the two owned probe fixtures."""
import argparse, datetime, importlib.util, json, os, pathlib, plistlib, secrets, subprocess, shutil, tempfile
from build import APP, BUNDLE_ID, ROOT, digest
from diagnostics import stage,copy_app,stop_owned,save_evidence
spec=importlib.util.spec_from_file_location("focus_tech",ROOT/"experiments/macos-capabilities/tech.py")
tech=importlib.util.module_from_spec(spec);spec.loader.exec_module(tech)
def existing_authorized_app(path):
    app=pathlib.Path(path).resolve(strict=True)
    if any(c.isspace() for c in str(app)):
        raise RuntimeError("authorized fixture application path must not contain whitespace")
    exe=app/"Contents/MacOS/Focus"
    plist=plistlib.loads((app/"Contents/Info.plist").read_bytes())
    bundle_id=plist.get("CFBundleIdentifier","")
    if bundle_id!=BUNDLE_ID and not bundle_id.startswith("local.focusprobe.acceptance.host."):
        raise RuntimeError("authorized fixture application identity is not owned by this project")
    if plist.get("CFBundleExecutable")!="Focus":
        raise RuntimeError("authorized fixture application must be the exact current build")
    subprocess.run(["codesign","--verify","--strict",str(app)],check=True)
    expected=APP/"Contents/MacOS/Focus"
    if digest(exe)!=digest(expected):
        # A distinct, signed fixture identity has a different embedded
        # signature. Compare executable bytes on disposable unsigned copies.
        with tempfile.TemporaryDirectory(prefix="guijing-compare-executables-") as tmp:
            copies=[pathlib.Path(tmp)/name for name in ("authorized","expected")]
            for original,copy in zip((exe,expected),copies):
                shutil.copyfile(original,copy)
                subprocess.run(["codesign","--remove-signature",str(copy)],check=True)
            if digest(copies[0])!=digest(copies[1]):
                raise RuntimeError("authorized fixture application must be the exact current build")
    # Do not start another instance alongside a user's normal session.
    procs=subprocess.check_output(["ps","-axo","args="],text=True).splitlines()
    if any(line==str(exe) or line.startswith(str(exe)+" ") for line in procs):
        raise RuntimeError("the selected app is already running; use a separate authorized copy")
    return app

def main(immersive=False, authorized_app=None, expect_denied=False, outside_join=False):
    if tech.live_runs():raise RuntimeError("An owned fixture run is still alive; retain it until cleaned.")
    authorized=existing_authorized_app(authorized_app) if authorized_app else None
    tech.build()
    source_build=tech.BUILD
    staging=stage("zhuanzhuli-effects-")
    tech.ROOT=staging;tech.BUILD=staging/".build";tech.RUNS=staging/".runs"
    for name in ("FixtureA","FixtureB"):
        copy_app(source_build/"bundles"/(name+".app"),tech.bundle(name),name)
    app=authorized or copy_app(APP,staging/APP.name,"Focus")
    run="run-"+datetime.datetime.now().strftime("%Y%m%dT%H%M%S")+"-"+secrets.token_hex(4)
    path=tech.RUNS/run;path.mkdir(parents=True,mode=0o700);token=secrets.token_hex(16)
    tech.write(path/"manifest.json",dict(schemaVersion=1,runID=run,token=token,purpose="production-app-effects"))
    parent=ROOT/".validation";parent.mkdir(mode=0o700,exist_ok=True)
    data=staging/"data";data.mkdir(mode=0o700);evidence=parent/staging.name
    log=staging/"app.log"
    children=[];identities=[];launcher=None;finished=False
    try:
        for name,label in (("FixtureA","A"),("FixtureB","B")):
            p=subprocess.Popen(["open","-n","-W",str(tech.bundle(name)),"--stdout",str(path/(name+".app.log")),
                "--stderr",str(path/(name+".app.log")),"--args","--run",run,"--token",token,"--label",label])
            children.append(p)
            state=tech.wait_file(path/(label+".json"),lambda s:tech.handshake_matches(s,token,name),children)
            identities.append(dict(name=name,pid=state["ref"]["pid"],launcherPID=p.pid))
            tech.write(path/"launch.json",dict(schemaVersion=1,children=identities))
        args=["open","-n","-W",str(app),"--stdout",str(log),
            "--stderr",str(log),"--args","--data-dir",str(data),"--fixture-check",str(tech.BUILD/"bundles")]
        if immersive:args.append("--fixture-immersion")
        if outside_join:args.append("--fixture-outside-join")
        launcher=subprocess.Popen(args)
        result=tech.wait_file(data/"fixture-check.json",timeout=60 if immersive else 45)
        launcher.wait(timeout=20)
        normal=json.loads((data/"normal-exit.json").read_text())
        print(json.dumps(result,ensure_ascii=False,indent=2))
        keys=("pickerRepeatKeepsSelection","unavailableReturnPrompt","availableReturnActivated","alreadyActiveReturnPreserved","currentAllowedWorkPreserved","directReturnWithoutHostActivation","entryHandoffActivatedWork","entryAudioContinuesAfterHandoff","excludedHidden","allowedVisible","pauseRestores","outsideActivationShowsChoice","allowancePreservedOnResume",
              "outsideAlternateAppHidden","supersededHidePreservedVisible","countdownBeforeVideo","repeatCleanupReleased","originallyHiddenPreserved","externalChangePreserved","endDuringEffectRestores","noHideAfterEnd")
        if immersive:keys=("accessibilityTrusted","workStartedAfterHandoff","bothWindowsFullscreen","pauseRestoredBoth")
        if outside_join:
            keys=("immersiveOutsideJoinPreserved",)
            screenshot=data/'immersive-outside-join.png'
            if not screenshot.is_file():
                result['screenshotPending']=True
        clean=(result.get("pendingAtEnd")==0 and not any(result.get(k) for k in ("error","cleanupError","setupCleanupError"))
               and normal.get("eventLoopReturned") is True and normal.get("pendingChanges")==0)
        denied_ok=clean and all(result.get(k) is True for k in ("permissionDenied","permissionDeniedWorkStarted",
            "permissionDeniedWorkResumed","permissionDeniedNoFullscreenRequests","permissionErrorLogged"))
        valid=denied_ok if expect_denied else clean and all(result.get(k) is True for k in keys)
        name="app-outside-join.json" if outside_join else ("app-immersive-effects-denied.json" if expect_denied else ("app-immersive-effects.json" if immersive else "app-effects.json"))
        tech.write(parent/name,dict(result,fixtureRun=run,dataDirectory=str(evidence/"application"),valid=valid))
        if not valid:
            if immersive and denied_ok:
                print("Best-effort start and resume passed; authorized fullscreen verification still requires a grant.")
                finished=True
                return 3
            raise RuntimeError("Production app effects failed; inspect "+str(evidence))
        finished=True
    finally:
        tech.write(path/"stop.json",dict(token=token,stop=True))
        cleanup_errors=[]
        for p in children:
            try:p.wait(timeout=12)
            except subprocess.TimeoutExpired:cleanup_errors.append("owned fixture did not exit")
        try:stop_owned(app,data,launcher)
        except Exception as error:cleanup_errors.append(str(error))
        tech.write(path/"cleanup.json",dict(fixturesExited=all(p.poll() is not None for p in children),
            applicationExited=launcher is not None and launcher.poll() is not None,errors=cleanup_errors))
        save_evidence(data,evidence/"application");save_evidence(path,evidence/"fixtures")
        if log.exists():shutil.copyfile(log,evidence/"application"/"app.log")
        print("evidence:",evidence)
        if cleanup_errors:raise RuntimeError("; ".join(cleanup_errors))
        if finished:shutil.rmtree(staging)
    return 0
if __name__=="__main__":
    parser=argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--immersive",action="store_true")
    parser.add_argument("--authorized-app",default=os.environ.get("FOCUS_EFFECTS_AUTHORIZED_APP"))
    opts=parser.parse_args()
    suite=os.environ.get("FOCUS_EFFECTS_SUITE")
    expect_denied=suite=="immersive-untrusted"
    immersive=opts.immersive or suite in ("immersive","immersive-untrusted")
    if opts.authorized_app and (not immersive or expect_denied):
        parser.error("--authorized-app requires the authorized immersive suite")
    outside_join=suite=="outside-join"
    if immersive and outside_join:parser.error("choose only one fixture suite")
    raise SystemExit(main(immersive,opts.authorized_app,expect_denied,outside_join))
