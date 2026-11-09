"""Ownership and evidence helpers for our disposable, local test applications."""
import hashlib, json, os, pathlib, plistlib, re, shutil, signal, subprocess, tempfile, time
from build import APP_NAME, BUNDLE_ID
def stage(prefix):
    p=pathlib.Path(tempfile.mkdtemp(prefix=prefix)).resolve()
    if any(c.isspace() for c in str(p)):
        p.rmdir()
        raise RuntimeError("isolated checks require a temporary path without whitespace for safe cleanup")
    p.chmod(0o700);return p
def copy_app(source,target,executable):
    shutil.copytree(source,target)
    relative=pathlib.Path("Contents/MacOS")/executable
    digest=lambda p:hashlib.sha256(p.read_bytes()).hexdigest()
    if digest(source/relative)!=digest(target/relative):raise RuntimeError("staged executable differs from build")
    info=target/"Contents/Info.plist";metadata=plistlib.loads(info.read_bytes())
    if metadata.get("CFBundleIdentifier")==BUNDLE_ID:
        # A relocated or wrapped ad-hoc build has a different code requirement.
        # Never let its TCC record replace the installed product's authorization.
        suffix=hashlib.sha256(str(target.resolve()).encode()).hexdigest()[:12]
        metadata.update(CFBundleIdentifier="local.focusprobe.acceptance.host."+suffix,
                        CFBundleName=APP_NAME+"验收",CFBundleDisplayName=APP_NAME+"验收")
        info.write_bytes(plistlib.dumps(metadata))
        subprocess.run(["codesign","--force","--sign","-","--identifier",metadata["CFBundleIdentifier"],str(target)],check=True,capture_output=True,text=True)
        subprocess.run(["codesign","--verify","--strict",str(target)],check=True,capture_output=True,text=True)
    return target
def stop_owned(app,data,launcher):
    """Signal only a process whose own identity and exact staged arguments match."""
    identity=data/"instance.json"
    if identity.is_file():
        r=json.loads(identity.read_text());pid=r["pid"]
        p=subprocess.run(["ps","-ww","-p",str(pid),"-o","args="],capture_output=True,text=True)
        args=p.stdout.strip()
        if p.returncode==0:
            # ps flattens argv. Ambiguous spaced paths are not evidence of the
            # selected store, so never signal them; managed stages reject them.
            if any(c.isspace() for c in str(app)+str(data)):
                raise RuntimeError("cannot safely identify remaining test application with spaced paths")
            metadata=plistlib.loads((app/"Contents/Info.plist").read_bytes())
            expected=str(app/"Contents/MacOS/Focus")+" --data-dir "+str(data)
            # Every caller puts this single argument pair first. Match its end
            # boundary and reject a later pair that could select another store.
            duplicate=re.search(r"(?:^|\s)--?data-dir(?:=|\s|$)",args[len(expected):])
            if r["path"]!=str(app.resolve()) or r["bundleID"]!=metadata["CFBundleIdentifier"] or duplicate or not (args==expected or args.startswith(expected+" ")):
                raise RuntimeError("cannot safely identify remaining test application")
            os.kill(pid,signal.SIGTERM)
    if launcher is not None and launcher.poll() is None:launcher.wait(timeout=15)
def save_evidence(data,destination):
    if data.exists():shutil.copytree(data,destination,dirs_exist_ok=True)
