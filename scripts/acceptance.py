#!/usr/bin/env python3
"""Own-app UI acceptance staging. Computer actions are performed with CUA.

The shipping UI is unchanged; --fixture-ui restricts environment effects to these
three identity-checked fixtures and skips system Focus. No third-party apps are hidden.
"""
import json,datetime,secrets,subprocess,plistlib,shlex,time,shutil
from pathlib import Path
from effects import tech,APP,ROOT
from diagnostics import stage,copy_app,stop_owned,save_evidence
STATE=ROOT/'.validation/acceptance-stage.json'
def bind(p):
    tech.ROOT=Path(p);tech.BUILD=tech.ROOT/'.build';tech.RUNS=tech.ROOT/'.runs'
def wrapper(app,executable,args):
    # Reopening via CUA/LaunchServices must retain the isolated data/run boundary.
    f=app/'Contents/MacOS/AcceptanceLauncher';f.write_text('#!/bin/sh\nexec '+shlex.join([str(app/'Contents/MacOS'/executable),*args])+' "$@"\n');f.chmod(0o700)
    p=app/'Contents/Info.plist';d=plistlib.loads(p.read_bytes());d['CFBundleExecutable']=f.name;p.write_bytes(plistlib.dumps(d))
    # Changing the bundle entry invalidates its signature. Seal the disposable
    # launcher and modified Info.plist before asking LaunchServices to run it.
    # The original Mach-O also binds the parent Info.plist; re-seal it first.
    subprocess.run(['codesign','--force','--sign','-','--identifier',d['CFBundleIdentifier'],str(app/'Contents/MacOS'/executable)],check=True,capture_output=True,text=True)
    subprocess.run(['codesign','--force','--sign','-','--identifier',d['CFBundleIdentifier'],str(app)],check=True,capture_output=True,text=True)
    subprocess.run(['codesign','--verify','--strict',str(app)],check=True,capture_output=True,text=True)
def start(appearance=None):
    if STATE.exists():raise RuntimeError('acceptance staging already registered; inspect or stop it')
    if tech.live_runs():raise RuntimeError('another owned fixture run is live')
    tech.build();source=tech.BUILD;p=stage('zhuanzhuli-acceptance-');bind(p)
    data=p/'data';data.mkdir(mode=0o700)
    run='run-'+datetime.datetime.now().strftime('%Y%m%dT%H%M%S')+'-'+secrets.token_hex(4);token=secrets.token_hex(16)
    path=tech.RUNS/run;path.mkdir(parents=True,mode=0o700)
    tech.write(path/'manifest.json',dict(schemaVersion=1,runID=run,token=token,purpose='production-native-ui-acceptance'))
    s=dict(stage=str(p),data=str(data),run=run,token=token,app=str(p/APP.name))
    STATE.parent.mkdir(mode=0o700,exist_ok=True);tech.write(STATE,s)
    for name,label in [('FixtureA','A'),('FixtureB','B')]:
        app=copy_app(source/'bundles'/(name+'.app'),tech.bundle(name),name)
        wrapper(app,name,['--run',run,'--token',token,'--label',label])
        subprocess.Popen(['open','-n','-W',str(app),'--stdout',str(path/(name+'.app.log')),'--stderr',str(path/(name+'.app.log'))],stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL)
        tech.wait_file(path/(label+'.json'),lambda v:tech.handshake_matches(v,token,name),timeout=20)
    # The third owned fixture covers switching between two outside targets.
    # Reuse the same fixture program with its own exact path and bundle ID.
    app=copy_app(source/'bundles/FixtureB.app',tech.bundle('FixtureC'),'FixtureB')
    info=app/'Contents/Info.plist';metadata=plistlib.loads(info.read_bytes())
    metadata.update(CFBundleIdentifier='local.focusprobe.fixture.c',CFBundleName='FixtureC')
    info.write_bytes(plistlib.dumps(metadata))
    wrapper(app,'FixtureB',['--run',run,'--token',token,'--label','C'])
    subprocess.Popen(['open','-n','-W',str(app),'--stdout',str(path/'FixtureC.app.log'),'--stderr',str(path/'FixtureC.app.log')],stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL)
    tech.wait_file(path/'C.json',lambda v:v.get('ref',{}).get('token')==token and v['ref']['bundleID']=='local.focusprobe.fixture.c' and v['ref']['path']==str(app.resolve()) and v['ref']['pid']>0,timeout=20)
    args=['--data-dir',str(data),'--fixture-ui',str(tech.BUILD/'bundles')]
    if appearance:args+=['--fixture-appearance',appearance]
    app=copy_app(APP,Path(s['app']),'Focus');wrapper(app,'Focus',args)
    subprocess.Popen(['open','-n','-W',str(app),'--stdout',str(data/'app.log'),'--stderr',str(data/'app.log')],stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL)
    tech.wait_file(data/'instance.json',timeout=20)
    print(json.dumps({k:v for k,v in s.items() if k!='token'},ensure_ascii=False,indent=2))
def stop():
    s=json.loads(STATE.read_text());p=Path(s['stage']);bind(p);data=Path(s['data']);app=Path(s['app'])
    stop_owned(app,data,None)
    path=tech.RUNS/s['run'];tech.write(path/'stop.json',dict(token=s['token'],stop=True))
    deadline=time.monotonic()+12
    while True:
        live=[line for line in subprocess.check_output(['ps','-axo','args='],text=True).splitlines() if line.startswith(str(p)+'/') and '/Contents/MacOS/' in line]
        if not live:break
        if time.monotonic()>deadline:raise RuntimeError('owned app not stopped; keep staging and evidence')
        time.sleep(.1)
    dest=ROOT/'.validation'/p.name
    save_evidence(data,dest/'application');save_evidence(path,dest/'fixtures')
    shutil.rmtree(p);STATE.unlink();print('archived and stopped:',dest)
def status():
    s=json.loads(STATE.read_text());print(json.dumps({k:v for k,v in s.items() if k!='token'},ensure_ascii=False,indent=2))
    f=Path(s['data'])/'data.json'
    if f.exists():print(f.read_text())
if __name__=='__main__':
    import argparse
    p=argparse.ArgumentParser();p.add_argument('action',choices=['start','status','stop']);p.add_argument('--appearance',choices=['light','dark']);a=p.parse_args()
    if a.appearance and a.action!='start':p.error('--appearance is only valid for start')
    if a.action=='start':start(a.appearance)
    else:globals()[a.action]()
