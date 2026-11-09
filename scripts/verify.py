#!/usr/bin/env python3
"""Checks can fail; native construction is explicitly not visual/interaction proof."""
import argparse, fcntl, json, os, pathlib, plistlib, subprocess, tempfile, shutil, sys
from build import APP, APP_NAME, ROOT, VERSION, BUILD_NUMBER
from diagnostics import stage,copy_app,stop_owned,save_evidence
ENV=dict(os.environ,GOTOOLCHAIN="local",GOWORK="off",GOCACHE=str(ROOT/".cache/go-build"),
         GOMODCACHE=str(ROOT/"experiments/macos-capabilities/.cache/mod"))
def run(args,**kw):return subprocess.run(args,cwd=ROOT,check=True,env=ENV,**kw)
def validate_native(facts,normal):
    # Four destinations and explicit drafts replace the removed six-page checks.
    required=("repeatQuitRetainsPrompt","confirmationEscCancels","auditionStartStopPreservesConfiguration","defaultWindowSize","hiddenEntryQuitConfirmationVisible","entryQuitCancellationPreservesSession","fourPageNavigation","startTitleAndBoundary","nativeFields","oneWindowAndDrafts",
        "templateDurableSave","templateSelectionLoadsConfig","draftDoesNotWriteTemplate","explicitTemplateUpdate","newTemplateCancelKeepsPageAlive","nativeFullscreenOwnership",
        "appRowsCentered","appSelectionFitsViewport","pickerSelectionFitsViewport","rowRemovalUsesIdentity","darkAppearanceApplied","notificationDraftSurvivesThemeAndNavigation",
        "appearanceFailureRetainsValue","appearanceDurable","configuredSessionStarts","sessionConfigurationIsolated","natureSceneSelectionAndAudition","natureDraftScenesIndependent","natureAudioSurvivesEntry","naturePausedAudioStops","legacySceneNamesPreserved",
        "natureRetryRestoresPreview","natureUndecodablePreviewReported","natureRetryRestoresAudio","natureRepeatRetryKeepsAudio",
        "natureUndecodableAudioReported","natureRetryRetainsUndecodableAudio","natureReadyRetrySilentAfterRepair","independentAudioCombinationsAndZeroVolumes","natureUnselectedSceneRecovery",
        "sessionSurvivesNavigation","historyFirstPage","historyLastPage","historyDeletionClampsPage","historyUndoPreservesAll",
        "historySearchAllPages","historyDetailsAttached","historyEmptyResults","pickerAttached","pickerBlocksNavigation",
        "pickerCancelPreservesDraft","page0FitsMinimum","page1FitsMinimum","page2FitsMinimum","page3FitsMinimum",
        "mainCloseReopensSameWindow","noPendingEffects","recoveryControlsRemoved","startRecoveryLinkRemoved","saveAsPreservesOtherDraft","saveAsCancelPreservesDraft","quitChecksUnsavedDraft","preparationDetailsReachable","immersionPermissionHasSettingsAction","backgroundOffDoesNotShowEntryScene",
        "immersionDeniedBeforeCountdown","immersionPermissionRepeatKeepsSheet","immersionPermissionLocatesCurrentApp","immersionPermissionRefreshIsReadOnly","immersionPermissionRequiresExplicitStart",
        "immersionPermissionOrdinaryStartIsOneTime","immersionPermissionResumeStaysPaused","immersionFixturePreferencesRestored")
    failed=[name for name in required if facts.get(name) is not True]
    if facts.get("error"):failed.append(facts["error"])
    if failed:raise RuntimeError("native regression failed: "+", ".join(failed))
    if normal.get("eventLoopReturned") is not True or normal.get("pendingChanges")!=0:
        raise RuntimeError("native application did not exit cleanly")

def native(goarch, arch):
    from build import source_fingerprint
    with tempfile.TemporaryDirectory(prefix=f"focus-self-check-{arch}-") as data:
        run(["arch","-"+arch,str(APP/"Contents/MacOS/Focus"),"--self-check","--data-dir",data],timeout=30)
    parent=ROOT/".validation";parent.mkdir(mode=0o700,exist_ok=True)
    staging=stage(f"zhuanzhuli-native-{arch}-");d=staging/"data";d.mkdir(mode=0o700)
    app=copy_app(APP,staging/APP.name,"Focus");evidence=parent/staging.name
    log=staging/"app.log" # LaunchServices creates output files before the app starts.
    print("native check evidence:",evidence,flush=True);launcher=None;finished=False
    try:
        launcher=subprocess.Popen(["open","--arch",arch,"-n","-W",str(app),"--stdout",str(log),
            "--stderr",str(log),"--args","--data-dir",str(d),"--native-check"])
        if launcher.wait(timeout=45)!=0:
            raise RuntimeError("could not launch AppKit regression for "+arch)
        if (d/"quit-timeout.json").exists():raise RuntimeError("normal quit blocked; inspect native-check.json and app.log")
        facts=json.loads((d/"native-check.json").read_text());normal=json.loads((d/"normal-exit.json").read_text())
        print(json.dumps(facts,ensure_ascii=False))
        if facts.get("goarch")!=goarch or facts.get("buildID")!=source_fingerprint()[0]:
            raise RuntimeError("AppKit regression used an unexpected architecture or build")
        validate_native(facts,normal)
        (parent/f"native-check-{arch}.json").write_text(json.dumps(facts,ensure_ascii=False,indent=2))
        if goarch==subprocess.check_output(["go","env","GOHOSTARCH"],env=ENV,text=True).strip():
            (parent/"native-check.json").write_text(json.dumps(facts,ensure_ascii=False,indent=2))
        finished=True
    finally:
        try:stop_owned(app,d,launcher)
        finally:
            save_evidence(d,evidence)
            if log.exists():shutil.copyfile(log,evidence/"app.log")
        if finished:shutil.rmtree(staging)
def notifications(panel_close=False):
    parent=ROOT/".validation";parent.mkdir(mode=0o700,exist_ok=True)
    staging=stage("zhuanzhuli-notifications-");d=staging/"data";d.mkdir(mode=0o700)
    # macOS grants are tied to the application's code identity and location.
    # Use the authorized build with empty isolated data, never a relocated copy.
    app=APP;evidence=parent/staging.name
    log=staging/"app.log" # Launch Services creates this before the app validates its empty data directory.
    launcher=None;finished=False
    try:
        args=["open","-n","-W",str(app),"--stdout",str(log),
            "--stderr",str(log),"--args","--data-dir",str(d),"--notification-check"]
        if panel_close:args.append("--panel-close-probe")
        launcher=subprocess.Popen(args)
        launcher.wait(timeout=35)
        if not (d/"notification-check.json").exists() or not (d/"normal-exit.json").exists():
            raise RuntimeError("notification probe did not complete; raw launcher log is archived at "+str(evidence/"app.log"))
        facts=json.loads((d/"notification-check.json").read_text());normal=json.loads((d/"normal-exit.json").read_text())
        print(json.dumps(facts,ensure_ascii=False,indent=2),flush=True)
        if facts.get("pendingChanges") is not False or facts.get("requestedPermission") is not False or facts.get("switchedFocus") is not False or not normal.get("eventLoopReturned") or normal.get("pendingChanges")!=0:
            raise RuntimeError("notification audit boundary or cleanup failed")
        if not facts["accessibilityTrusted"] and (not facts.get("permissionRefused") or "辅助功能权限" not in facts.get("error","")):
            raise RuntimeError("read-only audit did not expose missing permission")
        (parent/("panel-close-probe.json" if panel_close else "notification-check.json")).write_text(json.dumps(facts,ensure_ascii=False,indent=2));finished=True
    finally:
        try:stop_owned(app,d,launcher)
        finally:
            save_evidence(d,evidence)
            if log.exists():shutil.copyfile(log,evidence/"app.log")
        if finished:shutil.rmtree(staging)
    if panel_close:
        if facts.get("mode")!="bounded-panel-close-read-only" or facts.get("panelClosedVerified") is not True or facts.get("escapePairsSent")!=1 or facts.get("focusPanelVerified") is not True or facts.get("error"):
            raise SystemExit(3)
        return
    # A successful read-only audit is not a successful notification capability test.
    if not facts.get("capabilityVerified"):raise SystemExit(3)
def media():
    from build import release_manifest
    from nature_entry import measure, recorded_entry
    from scene_programs import check_all
    manifest, targets = release_manifest()
    known = [x for x in json.loads((ROOT/"internal/media/scenes.json").read_text()) if not x.get("legacy")]
    if [(x["id"],x["name"]) for x in manifest["scenes"]] != [(x["id"],x["name"]) for x in known]:
        raise RuntimeError("release must contain exactly the six canonical scenes in order")
    report = check_all()
    programs = {item["id"]: item for item in report["scenes"]}
    for scene in manifest["scenes"]:
        if manifest['version']==2:
            video=recorded_entry(scene['id'])
            for role in ('entry','background','preview'):
                entry=scene[role]
                if video['outputs'].get(entry['source'])!=entry['sha256']:
                    raise RuntimeError('entry or final frame differs from verified footage')
        else:
            measure(scene["id"])
        item = programs[scene["id"]]
        expected = {key: item[key] for key in ("source", "path", "sha256")}
        if scene["work"] != expected:
            raise RuntimeError("release soundtrack differs from verified complete program: " + scene["id"])
    guide=ROOT/manifest["guide"]["source"]
    run(["ffmpeg","-hide_banner","-loglevel","error","-i",str(guide),"-f","null","-"],stdout=subprocess.DEVNULL)
    length=float(subprocess.check_output(["ffprobe","-v","error","-show_entries","format=duration","-of","default=noprint_wrappers=1:nokey=1",str(guide)],text=True))
    if not 0<length<=5:raise RuntimeError("entry guide exceeds the five-second movie")
    if manifest['version']==2:
        actual={x['id']:x['audio'] for x in manifest['ambiences']}
        expected={x['id']:{k:x[k] for k in ('source','path','sha256')} for x in report['ambiences']}
        if actual!=expected:raise RuntimeError('packaged natural recordings differ from licensed source rebuild')
        voice=json.loads((ROOT/'assets/media/v5/voice-source.json').read_text())
        import hashlib
        if hashlib.sha256(guide.read_bytes()).hexdigest()!=voice['sha256']:
            raise RuntimeError('guide differs from local voice provenance')
    print(json.dumps({item["id"]: {"seconds":item["seconds"], "lufs":item["lufs"],
        "sha256":item["sha256"]} for item in report["scenes"]},ensure_ascii=False))
    print("Six entries, full-source long programs, natural recordings, release hashes and guide verified; subjective listening remains separate.")

def history():
    staging=stage("zhuanzhuli-history-")
    app=copy_app(APP,staging/APP.name,"Focus")
    evidence=ROOT/".validation"/staging.name
    results=[]
    for count in (0,100,1000,10000):
        data=staging/str(count);data.mkdir(mode=0o700)
        log=staging/(str(count)+".log")
        launcher=None
        try:
            launcher=subprocess.Popen(["open","-n","-W",str(app),"--stdout",str(log),"--stderr",str(log),"--args","--data-dir",str(data),"--native-check","--history-benchmark",str(count)])
            launcher.wait(timeout=120)
            facts=json.loads((data/"history-benchmark.json").read_text())
            normal=json.loads((data/"normal-exit.json").read_text())
            if facts.get("error") or facts["historyCount"]!=count or not normal.get("eventLoopReturned") or normal.get("pendingChanges")!=0:
                raise RuntimeError("history benchmark did not complete normally")
            results.append(facts);print(json.dumps(facts,ensure_ascii=False),flush=True)
        finally:
            try:stop_owned(app,data,launcher)
            finally:
                save_evidence(data,evidence/str(count))
                if log.exists():shutil.copyfile(log,evidence/str(count)/"app.log")
    (ROOT/".validation/history-benchmark.json").write_text(json.dumps(results,ensure_ascii=False,indent=2))
    shutil.rmtree(staging)


def validate_ui_state(state,native):
    phase=native.get("session",{}).get("phase","")
    page=native.get("mainPage")
    pages={"ready":0,"working":0,"paused":0,"apps":0,"empty-apps":0,
        "environment":0,"notifications":3,"notification-draft":3,"recovery":3,"settings":3,"templates":1,"empty-templates":1,"template-draft":1,
        "history":2,"empty-history":2,"history-filter":2,"history-detail":2,"long-goal":0,"start-error":0}
    if state in pages and page!=pages[state]:raise RuntimeError("UI state has the wrong page: "+state)
    if state=="ready":valid=phase in ("idle","ended","interrupted")
    elif state=="working":valid=phase in ("active","overtime")
    elif state=="paused":valid=phase=="paused"
    elif state=="apps":valid=native.get("appList",{}).get("count",0)>0
    elif state=="empty-apps":valid=native.get("appList",{}).get("count",-1)==0
    elif state=="history":valid=native.get("historyCount",0)>0
    elif state=="empty-history":valid=native.get("historyCount",-1)==0
    elif state=="notification-draft":valid=native.get("notificationDraftDirty") is True
    elif state=="picker":valid=(native.get("mainModal") is True and native.get("navigation",{}).get("attachedSheet") is True and native.get("windows",{}).get("appPicker",{}).get("visible") is True)
    elif state=="entry":valid=phase=="entry" and native.get("windows",{}).get("entry",{}).get("visible") is True
    elif state=="long-goal":valid=native.get("draftGoalLength",0)>=180
    elif state=="start-error":valid=any(native.get("readyErrors",{}).values())
    elif state=="templates":valid=native.get("templateCount",0)>0
    elif state=="empty-templates":valid=native.get("templateCount",-1)==0
    elif state=="template-draft":valid=native.get("templateDraftDirty") is True
    elif state=="history-filter":valid=bool(native.get("historyQuery"))
    elif state=="history-detail":valid=native.get("mainModal") is True and native.get("windows",{}).get("detail",{}).get("visible") is True
    elif state in ("environment","notifications","recovery","settings"):valid=True
    else:raise RuntimeError("unknown UI state: "+state)
    if not valid:raise RuntimeError("UI label does not match actual native state: "+state)

def ui(manifest_path, suite):
    """Validate current native receipts and unmodified CUA PNG/JPEG captures."""
    import hashlib
    from PIL import Image
    from build import source_fingerprint
    evidence=json.loads(pathlib.Path(manifest_path).read_text())
    expected=source_fingerprint()[0]
    metadata=json.loads((APP/"Contents/Resources/build.json").read_text())
    if evidence.get("buildID")!=expected or metadata.get("buildID")!=expected:
        raise RuntimeError("UI evidence does not belong to the current source and build")
    facts=json.loads((ROOT/evidence["nativeFacts"]).read_text())
    normal=json.loads((ROOT/evidence["nativeExit"]).read_text())
    if facts.get("buildID")!=expected:raise RuntimeError("native regression belongs to another build")
    validate_native(facts,normal)
    seen=set();states=set();window_ids={}
    for shot in evidence["screenshots"]:
        path=(ROOT/shot["path"]).resolve()
        path.relative_to(ROOT)
        if hashlib.sha256(path.read_bytes()).hexdigest()!=shot["sha256"]:
            raise RuntimeError("screenshot evidence changed: "+str(path))
        with Image.open(path) as picture:
            if picture.format not in ("PNG","JPEG") or min(picture.size)<100:raise RuntimeError("invalid CUA screenshot")
            picture.verify()
        native=shot["nativeState"]
        if native.get("buildID")!=expected:raise RuntimeError("screenshot state belongs to another build")
        main=native["windows"]["main"]
        if shot["state"] not in ("entry","entry-countdown","outside-choice","due") and not main["visible"]:raise RuntimeError("ordinary page screenshot has no visible main window")
        appearance="dark" if "dark" in native["appearance"].lower() else "light"
        if appearance!=shot["appearance"]:raise RuntimeError("appearance does not match captured state")
        if native["mainPage"]!=shot["page"]:raise RuntimeError("page does not match captured state")
        if suite == "trial-feedback":
            state=shot["state"]
            phase=native.get("session",{}).get("phase")
            duration=native.get("duration",{})
            countdown=native.get("countdown",{})
            frame,screen=countdown.get("frame",{}),countdown.get("screen",{})
            centered=bool(frame and screen) and all(abs(frame[origin]+frame[size]/2-screen[origin]-screen[size]/2)<=1 for origin,size in (("X","W"),("Y","H")))
            valid={
                "duration": native["mainPage"] in (0,1) and 1<=duration.get("minutes",0)<=180 and duration.get("minutes")==duration.get("slider") and duration.get("input")==str(duration.get("minutes")),
                "duration-invalid": native["mainPage"] in (0,1) and duration.get("minutes")==0,
                "entry-countdown": phase=="entry" and native.get("entryNumber") in ("3","2","1") and centered and countdown.get("visible") is True and countdown.get("canBecomeKey") is False and not native.get("entryPresentation") and not native.get("videoAllocated"),
                "outside-choice": phase in ("active","overtime") and bool(native.get("pendingChoice")) and native.get("windows",{}).get("choice",{}).get("visible") is True,
                "focus-history": native["mainPage"]==2 and native.get("historyCount",0)>0,
                "saved-quick": native["mainPage"]==1 and bool(native.get("quickTemplate")),
                "due": phase=="overtime" and "时间到了" in native.get("counterOverlay",{}).get("text",""),
            }.get(state)
            if valid is not True:raise RuntimeError("trial UI label does not match native state: "+state)
        else:
            validate_ui_state(shot["state"],native)
        identity=window_ids.setdefault(shot["runID"],main["number"])
        if identity!=main["number"]:raise RuntimeError("ordinary navigation changed the main window")
        for row in native.get("appList",{}).get("rows",[]):
            if row["measured"]:
                i,t=row["icon"],row["text"]
                if abs(i["Y"]+i["H"]/2-t["Y"]-t["H"]/2)>1:raise RuntimeError("captured app row is not centered")
        seen.add((appearance,shot["page"],shot["state"]));states.add(shot["state"])
    if suite == "trial-feedback":
        missing={("light",0,"duration"),("dark",1,"duration")}-seen
        required={"duration","duration-invalid","entry-countdown","outside-choice","focus-history","saved-quick","due"}
    else:
        missing={(appearance,page) for appearance in ("light","dark") for page in range(4)}-{(a,p) for a,p,_ in seen}
        required={"ready","apps","templates","settings","history","history-detail","history-filter","picker","working","paused","empty-apps","empty-history","empty-templates","notification-draft","template-draft"}
    if missing:raise RuntimeError("missing native page screenshots: "+str(sorted(missing)))
    if required-states:raise RuntimeError("missing UI states: "+str(sorted(required-states)))
    print("Current build: "+expected+"; native regression validated; "+str(len(evidence["screenshots"]))+" CUA captures cover required states for "+suite+".")
    print("Capture provenance and native state verified; visual judgment remains with the independent reviewer.")

def main():
    from build import TARGETS
    p=argparse.ArgumentParser();p.add_argument("check",choices=["tests","media","native","package","notifications","panel-close","history","ui"]);p.add_argument("--manifest");p.add_argument("--ui-suite",choices=("release","trial-feedback"),default="release");p.add_argument("--arch",choices=TARGETS.values());a=p.parse_args()
    if a.manifest and a.check!="ui":p.error("--manifest is only valid for ui")
    if a.check=="ui" and not a.manifest:p.error("ui requires --manifest")
    if a.arch and a.check!="native":p.error("--arch is only valid for native")
    if a.check=="tests":
        run(["go","test","-count=1","./..."]);run(["go","vet","./..."])
        run([sys.executable,"-m","unittest","discover","-s","scripts","-p","*_test.py"])
        # Keep the original probe contract tests at their stable entry points,
        # exercising the same shared package across the module boundary.
        subprocess.run(["go","test","-count=1","./internal/journal","./internal/probe"],
            cwd=ROOT/"experiments/macos-capabilities",env=ENV,check=True)
    elif a.check=="media":media()
    elif a.check=="native":
        for goarch,arch in TARGETS.items():
            if a.arch is None or a.arch==arch:native(goarch,arch)
    elif a.check=="ui":ui(a.manifest,a.ui_suite)
    elif a.check=="history":history()
    elif a.check=="notifications":notifications()
    elif a.check=="panel-close":notifications(panel_close=True)
    else:
        run(["codesign", "--verify", "--strict", str(APP)])
        info = plistlib.loads((APP/"Contents/Info.plist").read_bytes())
        if (info.get("CFBundleName"), info.get("CFBundleDisplayName"), info.get("CFBundleShortVersionString"),
                info.get("CFBundleVersion"), info.get("CFBundleIdentifier")) != (
                APP_NAME, APP_NAME, VERSION, BUILD_NUMBER, "com.yunsiweilai.zhuanzhuli"):
            raise RuntimeError("bundle branding, version, or upgrade identity differs")
        if info.get("CFBundleDevelopmentRegion") != "zh-Hans" or info.get("CFBundleLocalizations") != ["zh-Hans"]:
            raise RuntimeError("native bundle language does not match the Chinese interface")
        if info.get("CFBundleIconFile") != "AppIcon.icns":
            raise RuntimeError("application icon is not declared in Info.plist")
        with tempfile.TemporaryDirectory(prefix="focus-icon-check-") as d:
            iconset = pathlib.Path(d)/"AppIcon.iconset"
            run(["iconutil", "-c", "iconset", str(APP/"Contents/Resources/AppIcon.icns"), "-o", str(iconset)])
            for size in (16, 32, 128, 256, 512):
                for scale in (1, 2):
                    suffix = "@2x" if scale == 2 else ""
                    image = iconset/f"icon_{size}x{size}{suffix}.png"
                    properties = subprocess.check_output(["sips", "-g", "pixelWidth", "-g", "pixelHeight", str(image)], text=True)
                    dimensions = {line.split(":")[0].strip(): line.split(":")[1].strip() for line in properties.splitlines() if line.strip().startswith(("pixelWidth:", "pixelHeight:"))}
                    if dimensions != {"pixelWidth": str(size*scale), "pixelHeight": str(size*scale)}:
                        raise RuntimeError("application icon has an invalid representation: " + image.name)
                    from icon import verify as verify_icon
                    verify_icon(image)
        from build import release_manifest, source_fingerprint, digest, verify_architectures
        executable=APP/"Contents/MacOS/Focus"
        verify_architectures(executable)
        manifest, targets = release_manifest()
        resources=APP/"Contents/Resources"
        actual={str(p.relative_to(resources)) for p in resources.rglob("*") if p.is_file()}
        if actual != set(targets)|{"AppIcon.icns","manifest.json","build.json"}:
            raise RuntimeError("bundle includes missing or unlisted resources: "+str(actual))
        for target,item in targets.items():
            if digest(resources/target)!=item["sha256"]:raise RuntimeError("bundle media differs from release manifest")
        metadata=json.loads((resources/"build.json").read_text());candidate=json.loads((APP.parent/"candidate.json").read_text())
        if metadata["buildID"]!=info.get("FocusBuildID") or candidate["buildID"]!=metadata["buildID"] or source_fingerprint()[0]!=metadata["buildID"]:
            raise RuntimeError("candidate is stale or build provenance differs")
        files={str(p.relative_to(APP)):digest(p) for p in APP.rglob("*") if p.is_file()}
        if files!=candidate["files"]:raise RuntimeError("signed bundle changed after build")
        print("application icon contains all ten 16–1024 pixel representations")
        with tempfile.TemporaryDirectory(prefix="focus-package-") as d:
            run([str(executable),"--self-check","--data-dir",d])
        rejected=[]
        for name,status in (("environment.json","pending"),("environment.json","applied"),("fullscreen.json","applied"),("notifications.json","pending")):
            with tempfile.TemporaryDirectory(prefix="focus-fixture-recovery-") as d:
                root=pathlib.Path(d);bundles=root/".build/bundles";bundles.mkdir(parents=True)
                data=root/"data";data.mkdir();original=data/name
                if name=="notifications.json":body=json.dumps(dict(version=1,status=status,profile="do-not-touch")).encode()
                else:body=json.dumps(dict(schemaVersion=1,entries=[dict(operationID="outside-fixture",status=status,ref=dict(label="范围外验证记录",pid=2147483647,launched=1,token="isolated-regression",bundleID="validation.outside",path="/nonexistent/OutsideFixture.app"))])).encode()
                original.write_bytes(body)
                result=subprocess.run([str(APP/"Contents/MacOS/Focus"),"--data-dir",str(data),"--fixture-ui",str(bundles)],env=ENV,capture_output=True,text=True,timeout=10)
                if result.returncode==0 or "out-of-scope record" not in result.stderr or original.read_bytes()!=body or (data/"instance.json").exists():
                    raise RuntimeError("interactive fixture opened foreign recovery state: "+name+"/"+status)
                rejected.append(dict(case="fixture-ui-foreign-"+name+"-"+status,exit=result.returncode,preserved=True))
        with tempfile.TemporaryDirectory(prefix="focus-diagnostic-existing-") as d:
            data=pathlib.Path(d)/"data.json";original=b'{"version":1}';data.write_bytes(original)
            result=subprocess.run([str(APP/"Contents/MacOS/Focus"),"--native-check","--data-dir",d],env=ENV,capture_output=True,text=True,timeout=25)
            facts=dict(exit=result.returncode,preserved=data.read_bytes()==original,files=sorted(p.name for p in pathlib.Path(d).iterdir()))
            if result.returncode==0 or "fresh empty data directory" not in result.stderr or not facts["preserved"] or facts["files"]!=["data.json"]:
                raise RuntimeError("native diagnostic did not reject existing data before startup: "+json.dumps(facts))
            rejected.append(dict(case="native-diagnostic-existing-data",**facts))
        for args in (["--native-check"],["--notification-check"],["--fixture-check","/nonexistent"]):
            for name,body in (("environment.json",b'{"version":1,"changes":[]}'),("notifications.json",b'{"version":1,"status":"applied","profile":"do-not-touch"}')):
                with tempfile.TemporaryDirectory(prefix="focus-diagnostic-recovery-") as d:
                    original=pathlib.Path(d)/name;original.write_bytes(body)
                    result=subprocess.run([str(APP/"Contents/MacOS/Focus"),*args,"--data-dir",d],env=ENV,capture_output=True,text=True,timeout=10)
                    if result.returncode==0 or "fresh empty data directory" not in result.stderr or original.read_bytes()!=body or sorted(p.name for p in pathlib.Path(d).iterdir())!=[name]:
                        raise RuntimeError("diagnostic opened existing recovery state: "+args[0]+" "+name)
                    rejected.append(dict(case=args[0]+"-existing-"+name,exit=result.returncode,preserved=True))
        for args in (["--history-benchmark","100"],["--native-check","--history-benchmark","1"]):
            with tempfile.TemporaryDirectory(prefix="focus-history-boundary-") as d:
                result=subprocess.run([str(APP/"Contents/MacOS/Focus"),"--data-dir",d,*args],env=ENV,capture_output=True,text=True,timeout=10)
                if result.returncode==0 or "requires native-check" not in result.stderr or list(pathlib.Path(d).iterdir()):raise RuntimeError("history benchmark escaped isolated flag boundary")
                rejected.append(dict(case="history-benchmark-flag-boundary",exit=result.returncode,message=result.stderr.strip()))
        with tempfile.TemporaryDirectory(prefix="focus-history-existing-") as d:
            original=pathlib.Path(d)/"data.json";original.write_bytes(b"preserve")
            result=subprocess.run([str(APP/"Contents/MacOS/Focus"),"--data-dir",d,"--native-check","--history-benchmark","100"],env=ENV,capture_output=True,text=True,timeout=10)
            if result.returncode==0 or original.read_bytes()!=b"preserve" or len(list(pathlib.Path(d).iterdir()))!=1:raise RuntimeError("history benchmark modified existing data")
            rejected.append(dict(case="history-benchmark-existing-data",exit=result.returncode,preserved=True))
        for name,body in (("corrupt",b"bad json"),("newer-version",b'{"version":999}'),
                          ("invalid-checkpoint",b'{"version":1,"current":{"id":"bad"}}')):
            with tempfile.TemporaryDirectory(prefix="focus-package-negative-") as d:
                data=pathlib.Path(d)/"data.json";data.write_bytes(body)
                result=subprocess.run([str(APP/"Contents/MacOS/Focus"),"--self-check","--data-dir",d],env=ENV,capture_output=True,text=True,timeout=10)
                if result.returncode==0 or data.read_bytes()!=body:raise RuntimeError("invalid data accepted or overwritten: "+name)
                rejected.append(dict(case=name,exit=result.returncode,preserved=True,message=result.stderr.strip()))
        with tempfile.TemporaryDirectory(prefix="focus-package-lock-") as d:
            with (pathlib.Path(d)/"app.lock").open("w") as lock:
                fcntl.flock(lock,fcntl.LOCK_EX|fcntl.LOCK_NB)
                result=subprocess.run([str(APP/"Contents/MacOS/Focus"),"--self-check","--data-dir",d],env=ENV,capture_output=True,text=True,timeout=10)
                if result.returncode==0 or "已有运行中" not in result.stderr:raise RuntimeError("second instance lock was not enforced")
                rejected.append(dict(case="second-instance",exit=result.returncode,message=result.stderr.strip()))
        with tempfile.TemporaryDirectory(prefix="focus-package-appearance-") as d:
            result=subprocess.run([str(APP/"Contents/MacOS/Focus"),"--data-dir",d,"--fixture-appearance","dark"],env=ENV,capture_output=True,text=True,timeout=10)
            if result.returncode==0 or "requires --fixture-ui" not in result.stderr or list(pathlib.Path(d).iterdir()):raise RuntimeError("test appearance escaped its fixture boundary")
            rejected.append(dict(case="appearance-fixture-boundary",exit=result.returncode,message=result.stderr.strip()))
        with tempfile.TemporaryDirectory(prefix="focus-panel-flag-") as d:
            result=subprocess.run([str(APP/"Contents/MacOS/Focus"),"--data-dir",d,"--panel-close-probe"],env=ENV,capture_output=True,text=True,timeout=10)
            if result.returncode==0 or "requires --notification-check" not in result.stderr or list(pathlib.Path(d).iterdir()):raise RuntimeError("panel probe escaped its flag boundary")
            rejected.append(dict(case="panel-probe-flag-boundary",exit=result.returncode,message=result.stderr.strip()))
        for name,body in (("notifications.json",b'{"version":1,"status":"applied","profile":"do-not-touch"}'),("data.json",b'{"version":1}')):
            with tempfile.TemporaryDirectory(prefix="focus-panel-data-") as d:
                data=pathlib.Path(d)/name;data.write_bytes(body)
                result=subprocess.run([str(APP/"Contents/MacOS/Focus"),"--data-dir",d,"--notification-check","--panel-close-probe"],env=ENV,capture_output=True,text=True,timeout=10)
                if result.returncode==0 or "fresh empty data directory" not in result.stderr or data.read_bytes()!=body or len(list(pathlib.Path(d).iterdir()))!=1:raise RuntimeError("panel probe accepted or modified existing data")
                rejected.append(dict(case="panel-probe-existing-"+name,exit=result.returncode,preserved=True,message=result.stderr.strip()))
        evidence=ROOT/".validation/package-negative.json";evidence.parent.mkdir(mode=0o700,exist_ok=True)
        evidence.write_text(json.dumps(rejected,ensure_ascii=False,indent=2))
        print("shipping entry point rejected invalid data, duplicate instance and test flag misuse, including panel probe leases; original data preserved")
if __name__=="__main__":main()
