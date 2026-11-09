#!/usr/bin/env python3
"""Fail-closed final release packaging. A developer build is never a readiness claim."""
import argparse, hashlib, json, os, pathlib, plistlib, shutil, subprocess, sys, tempfile
from build import ROOT, APP, APP_NAME, VERSION, BUILD_NUMBER, PACKAGE_PLATFORM, TARGETS, digest, source_fingerprint, candidate_lock, bundle_files

CRITERIA = ["AC"+str(n) for n in range(1,14)]
CHECKS = ["test", "native", "effects", "media", "package", "full-session", "notification-effect", "audio-listening", "accessibility", "display-lifecycle", "review-spec", "review-code"]
EVIDENCE = ROOT/".validation/release-evidence.json"

def readiness(path=EVIDENCE):
    candidate_path=APP.parent/"candidate.json"
    if not candidate_path.is_file():return ["缺少候选构建清单"]
    candidate=json.loads(candidate_path.read_text())
    missing=[]
    if candidate["buildID"]!=source_fingerprint()[0]:missing.append("候选源码已变化，必须重建和复验")
    files=bundle_files(APP)
    if files!=candidate["files"]:missing.append("签名候选内容发生变化")
    if not path.is_file():return missing+["缺少同构建验收："+", ".join(CRITERIA+CHECKS)]
    evidence=json.loads(path.read_text())
    if evidence.get("buildID")!=candidate["buildID"] or evidence.get("candidateSHA256")!=digest(candidate_path):missing.append("验收与当前候选不一致")
    for key in CRITERIA+CHECKS:
        item=evidence.get("results",{}).get(key,{})
        if item.get("status")!="pass" or not item.get("artifacts") or not item.get("method"):
            missing.append(key+" 缺少实际通过证据");continue
        if key.startswith("review-") and (item.get("independent") is not True or not item.get("reviewer")):
            missing.append(key+" 缺少独立审查身份")
        for artifact in item["artifacts"]:
            relative=pathlib.PurePosixPath(artifact.get("path",""))
            target=ROOT/relative
            if not relative.parts or relative.is_absolute() or ".." in relative.parts or not target.is_file() or target.resolve()!=target.absolute() or digest(target)!=artifact.get("sha256"):
                missing.append(key+" 证据文件缺失、越界或已改变")
    return missing

def archive_candidate(archive, candidate, unpack):
    subprocess.run(["codesign","--verify","--strict",str(APP)],check=True)
    subprocess.run(["ditto","-c","-k","--keepParent",str(APP),str(archive)],check=True)
    subprocess.run(["ditto","-x","-k",str(archive),str(unpack)],check=True)
    copied=unpack/APP.name
    if bundle_files(copied)!=candidate["files"]:
        raise RuntimeError("ZIP differs from verified candidate")
    subprocess.run(["codesign","--verify","--strict",str(copied)],check=True)

def release_candidate():
    missing=readiness()
    if missing:
        print("未生成正式交付 ZIP：\n"+"\n".join("- "+m for m in missing));return 4
    subprocess.run([sys.executable,"scripts/verify.py","package"],cwd=ROOT,check=True)
    candidate=json.loads((APP.parent/"candidate.json").read_text())
    output=APP.parent/f"{APP_NAME}-{VERSION}-{candidate['buildID'][:12]}-{PACKAGE_PLATFORM}.zip"
    with tempfile.TemporaryDirectory(prefix="focus-release-") as d:
        archive=pathlib.Path(d)/output.name
        archive_candidate(archive,candidate,pathlib.Path(d)/"unpack")
        archive.replace(output)
    output.with_suffix(".sha256").write_text(digest(output)+"  "+output.name+"\n")
    print(output);return 0

def local_package():
    """User-requested compiled bundle; full release readiness remains separate."""
    subprocess.run([sys.executable,"scripts/verify.py","media"],cwd=ROOT,check=True)
    subprocess.run([sys.executable,"scripts/verify.py","package"],cwd=ROOT,check=True)
    candidate_path=APP.parent/"candidate.json"
    candidate=json.loads(candidate_path.read_text())
    candidate_sha=digest(candidate_path)
    name=f"{APP_NAME}-{VERSION}-build{BUILD_NUMBER}-{candidate['buildID'][:12]}-{candidate_sha[:8]}-{PACKAGE_PLATFORM}"
    delivery=APP.parent/name
    output=delivery/(name+".zip")
    report_path=delivery/"package.json"
    if delivery.exists():
        report=validate_local_delivery(delivery, output.name, candidate_sha)
        with tempfile.TemporaryDirectory(prefix="focus-local-recheck-") as d:
            subprocess.run(["ditto","-x","-k",str(output),d],check=True)
            copied=pathlib.Path(d)/APP.name
            if bundle_files(copied)!=candidate['files']:
                raise RuntimeError("existing local package differs from signed application")
            subprocess.run(["codesign","--verify","--strict",str(copied)],check=True)
        print(output);return 0
    with tempfile.TemporaryDirectory(prefix="focus-local-package-",dir=APP.parent) as d:
        temporary=pathlib.Path(d);publication=temporary/'publication';publication.mkdir()
        archive=publication/output.name
        archive_candidate(archive,candidate,temporary/'unpack')
        if digest(candidate_path)!=candidate_sha or source_fingerprint()[0]!=candidate['buildID']:
            raise RuntimeError("candidate or source changed while packaging")
        sha=digest(archive)
        notice=local_installation_notice(candidate.get('signing','ad-hoc'))
        report=dict(role='user-requested-local-compiled-bundle',formalRelease=False,
                    version=VERSION,buildNumber=BUILD_NUMBER,buildID=candidate['buildID'],
                    candidateSHA256=candidate_sha,path=str(output),sha256=sha,bytes=archive.stat().st_size,
                    installationNoticeSHA256=hashlib.sha256(notice.encode()).hexdigest(),
                    exactSignedBundleRoundTrip=True,remainingReadiness=readiness())
        (publication/(output.name+'.sha256')).write_text(sha+'  '+output.name+'\n')
        (publication/'package.json').write_text(json.dumps(report,ensure_ascii=False,indent=2)+'\n')
        (publication/'安装说明.txt').write_text(notice)
        validate_local_delivery(publication,output.name,candidate_sha)
        os.replace(publication,delivery)
    print(output);return 0

def signing_notice(signing):
    return '本地 ad-hoc 签名' if signing == 'ad-hoc' else '已使用证书签名'

ACCESSIBILITY_NOTICE = ('沉浸模式开始前会核对辅助功能权限。若系统开关已开但仍被拒绝，请通过归径的权限面板定位当前副本，'
                        '在系统设置移除旧条目后重新添加这份应用。返回归径会更新状态，计时不会自动开始或恢复。\n')

def local_installation_notice(signing='ad-hoc'):
    manifest=json.loads((ROOT/'assets/release-media.json').read_text())
    notice=next(item['path'] for item in manifest['notices']
                if pathlib.PurePosixPath(item['path']).name=='THIRD-PARTY-NOTICES.txt')
    return (f'{APP_NAME} {VERSION}（macOS 通用版）\n\n'
            '适用于可运行 macOS 15 或更高版本的 Apple 芯片和 Intel Mac。\n'
            f'解压 ZIP，将「{APP.name}」拖入应用程序即可。本次没有自动安装。\n'
            '若已安装旧版「专注力」，请先退出旧版；更名后不会自动替换旧应用，本地记录沿用。\n'
            +ACCESSIBILITY_NOTICE+
            '六个场景均使用超过十分钟的完整曲目组；署名与许可在应用包内 Contents/Resources/'+notice+'。\n'
            +signing_notice(signing)+'，未做 Apple 公证。完整听感和连续两轮原生播放仍需实际验收。\n')

def validate_local_delivery(delivery, archive_name, candidate_sha):
    archive=delivery/archive_name
    checksum=delivery/(archive_name+'.sha256')
    notice=delivery/'安装说明.txt'
    report_path=delivery/'package.json'
    if not all(path.is_file() for path in (archive,checksum,notice,report_path)):
        raise RuntimeError('local delivery is missing a required artifact')
    report=json.loads(report_path.read_text())
    if (report['candidateSHA256']!=candidate_sha or digest(archive)!=report['sha256'] or
            digest(notice)!=report['installationNoticeSHA256']):
        raise RuntimeError('local delivery differs from candidate or recorded artifacts')
    if checksum.read_text()!=report['sha256']+'  '+archive_name+'\n':
        raise RuntimeError('local delivery checksum is missing or changed')
    return report

def dmg_installation_notice(architecture=None, signing='ad-hoc'):
    hardware = {'arm64': 'Apple 芯片 Mac', 'x86_64': 'Intel Mac'}.get(architecture, 'Apple 芯片和 Intel Mac')
    variant = f'macOS {architecture} DMG' if architecture else 'macOS 通用 DMG'
    return (f'{APP_NAME} {VERSION}（{variant}）\n\n'
            f'适用于可运行 macOS 15 或更高版本的 {hardware}。\n'
            f'双击 DMG 挂载后，将「{APP.name}」拖到同窗口的「应用程序」快捷方式即可完成安装。\n'
            '若已安装旧版「专注力」，请先退出旧版；更名后不会自动替换旧应用，本地记录沿用。\n'
            +ACCESSIBILITY_NOTICE+
            '六场景资源、署名与许可均在应用包内 Contents/Resources。\n'
            f'{signing_notice(signing)}，未做 Apple 公证，也不是已完成全部验收的正式发布。\n')

def stage_dmg_contents(app, stage):
    """The mounted volume holds the signed bundle plus the /Applications shortcut."""
    stage.mkdir(parents=True,exist_ok=True)
    subprocess.run(['ditto',str(app),str(stage/app.name)],check=True)
    (stage/'Applications').symlink_to('/Applications')


def verify_stage_contents(app, stage):
    """Reject a stray, missing or changed file before an image is created."""
    if not stage.is_dir():return False
    shortcut=stage/'Applications'
    if not shortcut.is_symlink() or shortcut.readlink()!=pathlib.Path('/Applications'):return False
    if sorted(p.name for p in stage.iterdir())!=sorted([app.name,'Applications']):return False
    staged=stage/app.name
    if not staged.is_dir():return False
    return bundle_files(app)==bundle_files(staged)

def verify_mounted_app(mounted, candidate):
    """The mounted copy must equal the verified candidate and stay signed."""
    if not mounted.is_dir():return False
    if bundle_files(mounted)!=candidate['files']:return False
    if candidate.get('architecture'):
        arches=subprocess.check_output(['lipo','-archs',str(mounted/'Contents/MacOS/Focus')],text=True).split()
        if arches!=[candidate['architecture']]:return False
    if subprocess.run(['codesign','--verify','--strict',str(mounted)],capture_output=True).returncode!=0:
        return False
    return True

def dmg_image_format(image):
    """The image format the delivery claims, read from the image itself."""
    out=subprocess.check_output(['hdiutil','imageinfo',str(image),'-plist'])
    try:
        info=plistlib.loads(out)
    except ValueError as exc:
        raise RuntimeError('cannot read the DMG image information') from exc
    return info.get('Format')

def verify_dmg_image(image, app, candidate):
    """Verify format and checksum, mount the image read-only and inspect the volume."""
    if dmg_image_format(image)!='UDZO':
        raise RuntimeError('DMG is not in the expected UDZO format')
    try:
        subprocess.run(['hdiutil','verify',str(image)],check=True,
                       stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL)
    except subprocess.CalledProcessError as exc:
        raise RuntimeError('DMG checksum verification failed') from exc
    mount=pathlib.Path(tempfile.mkdtemp(prefix='focus-dmg-mount-'))
    attached=False
    try:
        try:
            subprocess.run(['hdiutil','attach',str(image),'-nobrowse','-readonly',
                            '-mountpoint',str(mount)],check=True,stdout=subprocess.DEVNULL,
                           stderr=subprocess.DEVNULL)
        except subprocess.CalledProcessError as exc:
            raise RuntimeError('could not mount the DMG volume') from exc
        attached=True
        try:
            if sorted(p.name for p in mount.iterdir())!=sorted([app.name,'Applications']):
                raise RuntimeError('DMG volume has unexpected contents')
            if not (mount/'Applications').is_symlink() or (mount/'Applications').readlink()!=pathlib.Path('/Applications'):
                raise RuntimeError('DMG Applications shortcut is missing or changed')
            if not verify_mounted_app(mount/app.name,candidate):
                raise RuntimeError('mounted application differs from candidate or is not validly signed')
        finally:
            detach=subprocess.run(['hdiutil','detach',str(mount)],capture_output=True)
            if detach.returncode!=0:
                forced=subprocess.run(['hdiutil','detach',str(mount),'-force'],capture_output=True)
                if forced.returncode!=0:
                    raise RuntimeError('the DMG volume is still attached at '+str(mount))
            attached=False
    finally:
        if not attached:
            shutil.rmtree(mount,ignore_errors=True)

def validate_dmg_delivery(delivery, image_name, candidate_sha, app_file_count, architecture=None):
    image=delivery/image_name
    checksum=delivery/(image_name+'.sha256')
    notice=delivery/'安装说明.txt'
    report_path=delivery/'package.json'
    if not all(path.is_file() for path in (image,checksum,notice,report_path)):
        raise RuntimeError('local DMG delivery is missing a required artifact')
    try:
        report=json.loads(report_path.read_text())
    except ValueError as exc:
        raise RuntimeError('local DMG delivery report is unreadable') from exc
    expected=dict(role='user-requested-local-dmg',formalRelease=False,
                  candidateSHA256=candidate_sha,sha256=digest(image),
                  installationNoticeSHA256=digest(notice),appFileCount=app_file_count)
    if architecture:
        expected['architecture']=architecture
    if any(report.get(key)!=value for key,value in expected.items()):
        raise RuntimeError('local DMG delivery differs from candidate or recorded artifacts')
    if checksum.read_text()!=report['sha256']+'  '+image_name+'\n':
        raise RuntimeError('local DMG checksum is missing or changed')
    return report

def stage_architecture(app, architecture):
    """Copy one already-signed slice, retaining its signature and the bundle seal."""
    executable=app/'Contents/MacOS/Focus'
    output=executable.with_suffix('.thin')
    subprocess.run(['lipo',str(executable),'-thin',architecture,'-output',str(output)],check=True)
    output.chmod(executable.stat().st_mode & 0o777)
    os.replace(output,executable)
    subprocess.run(['codesign','--verify','--strict',str(app)],check=True)

def local_dmg(*architectures):
    """A real, mountable local DMG. It never claims formal release readiness."""
    subprocess.run([sys.executable,'scripts/verify.py','media'],cwd=ROOT,check=True)
    subprocess.run([sys.executable,'scripts/verify.py','package'],cwd=ROOT,check=True)
    for architecture in architectures or (None,):
        package_dmg(architecture)
    return 0

def package_dmg(architecture):
    candidate_path=APP.parent/'candidate.json'
    candidate=json.loads(candidate_path.read_text())
    candidate_sha=digest(candidate_path)
    platform=f'macOS-{architecture}' if architecture else PACKAGE_PLATFORM
    name=f"{APP_NAME}-{VERSION}-build{BUILD_NUMBER}-{candidate['buildID'][:12]}-{candidate_sha[:8]}-{platform}"
    image_name=name+'.dmg'
    delivery=APP.parent/(name+'-dmg')
    volname=f"{APP_NAME} {VERSION}"
    with tempfile.TemporaryDirectory(prefix='focus-local-dmg-',dir=APP.parent) as d:
        temporary=pathlib.Path(d);publication=temporary/'publication';publication.mkdir()
        stage=temporary/'stage'
        stage_dmg_contents(APP,stage)
        if not verify_stage_contents(APP,stage):
            raise RuntimeError('staged application differs from the signed bundle')
        payload=candidate
        if architecture:
            staged=stage/APP.name
            stage_architecture(staged,architecture)
            files=bundle_files(staged)
            allowed={'Contents/MacOS/Focus'}
            if {k:v for k,v in files.items() if k not in allowed}!={k:v for k,v in candidate['files'].items() if k not in allowed}:
                raise RuntimeError('architecture staging changed unrelated bundle contents')
            payload=dict(candidate,architecture=architecture,files=files)
        if delivery.exists():
            validate_dmg_delivery(delivery,image_name,candidate_sha,len(payload['files']),architecture)
            verify_dmg_image(delivery/image_name,APP,payload)
            print(delivery/image_name);return
        image=publication/image_name
        subprocess.run(['hdiutil','create','-srcfolder',str(stage),'-volname',volname,
                        '-format','UDZO','-ov',str(image)],check=True,stdout=subprocess.DEVNULL)
        if digest(candidate_path)!=candidate_sha or source_fingerprint()[0]!=candidate['buildID']:
            raise RuntimeError('candidate or source changed while packaging DMG')
        verify_dmg_image(image,APP,payload)
        sha=digest(image)
        notice=dmg_installation_notice(architecture,candidate.get('signing','ad-hoc'))
        report=dict(role='user-requested-local-dmg',formalRelease=False,
                    version=VERSION,buildNumber=BUILD_NUMBER,buildID=candidate['buildID'],
                    candidateSHA256=candidate_sha,path=str(delivery/image_name),sha256=sha,
                    bytes=image.stat().st_size,volumeName=volname,format='UDZO',
                    installMethod='drag-to-Applications-shortcut',
                    installationNoticeSHA256=hashlib.sha256(notice.encode()).hexdigest(),
                    appFileCount=len(payload['files']),exactSignedBundleRoundTrip=True,
                    hdiutilVerifyAndCodesignVerified=True,remainingReadiness=readiness())
        if architecture:
            report['architecture']=architecture
        (publication/(image_name+'.sha256')).write_text(sha+'  '+image_name+'\n')
        (publication/'package.json').write_text(json.dumps(report,ensure_ascii=False,indent=2)+'\n')
        (publication/'安装说明.txt').write_text(notice)
        validate_dmg_delivery(publication,image_name,candidate_sha,len(payload['files']),architecture)
        os.replace(publication,delivery)
    print(delivery/image_name)

def main():
    with candidate_lock():
        return release_candidate()
if __name__=="__main__":
    parser=argparse.ArgumentParser()
    parser.add_argument('--local-package',action='store_true',help='package the user-requested local compiled bundle without claiming full release readiness')
    parser.add_argument('--local-dmg',action='store_true',help='create a mountable local DMG alongside the existing ZIP package; never a formal release claim')
    parser.add_argument('--arch',action='append',default=[],choices=TARGETS.values(),help='thin a local DMG to this architecture; repeat for both')
    args=parser.parse_args()
    if args.local_package and args.local_dmg:
        parser.error('--local-package and --local-dmg are mutually exclusive')
    if args.arch and not args.local_dmg:
        parser.error('--arch is only valid with --local-dmg')
    if args.local_package:
        with candidate_lock():sys.exit(local_package())
    if args.local_dmg:
        with candidate_lock():sys.exit(local_dmg(*args.arch))
    sys.exit(main())
