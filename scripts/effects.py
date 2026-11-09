#!/usr/bin/env python3
"""Exercise the shipping app's effects against only the two owned probe fixtures."""
import datetime, importlib.util, json, secrets, subprocess, shutil
from build import APP, ROOT
from diagnostics import stage,copy_app,stop_owned,save_evidence
spec=importlib.util.spec_from_file_location("focus_tech",ROOT/"experiments/macos-capabilities/tech.py")
tech=importlib.util.module_from_spec(spec);spec.loader.exec_module(tech)
def main():
    if tech.live_runs():raise RuntimeError("An owned fixture run is still alive; retain it until cleaned.")
    tech.build()
    source_build=tech.BUILD
    staging=stage("zhuanzhuli-effects-")
    tech.ROOT=staging;tech.BUILD=staging/".build";tech.RUNS=staging/".runs"
    for name in ("FixtureA","FixtureB"):
        copy_app(source_build/"bundles"/(name+".app"),tech.bundle(name),name)
    app=copy_app(APP,staging/APP.name,"Focus")
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
        launcher=subprocess.Popen(["open","-n","-W",str(app),"--stdout",str(log),
            "--stderr",str(log),"--args","--data-dir",str(data),"--fixture-check",str(tech.BUILD/"bundles")])
        result=tech.wait_file(data/"fixture-check.json",timeout=45)
        launcher.wait(timeout=20)
        normal=json.loads((data/"normal-exit.json").read_text())
        print(json.dumps(result,ensure_ascii=False,indent=2))
        keys=("pickerRepeatKeepsSelection","unavailableReturnPrompt","availableReturnActivated","alreadyActiveReturnPreserved","currentAllowedWorkPreserved","directReturnWithoutHostActivation","entryHandoffActivatedWork","entryAudioContinuesAfterHandoff","excludedHidden","allowedVisible","pauseRestores","outsideActivationShowsChoice","allowancePreservedOnResume",
              "outsideAlternateAppHidden","supersededHidePreservedVisible","countdownBeforeVideo","repeatCleanupReleased","originallyHiddenPreserved","externalChangePreserved","endDuringEffectRestores","noHideAfterEnd")
        valid=(all(result.get(k) is True for k in keys) and result.get("pendingAtEnd")==0
               and not any(result.get(k) for k in ("error","cleanupError","setupCleanupError"))
               and normal.get("eventLoopReturned") is True and normal.get("pendingChanges")==0)
        tech.write(parent/"app-effects.json",dict(result,fixtureRun=run,dataDirectory=str(evidence/"application"),valid=valid))
        if not valid:raise RuntimeError("Production app effects failed; inspect "+str(data))
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
if __name__=="__main__":main()
