import json, pathlib, subprocess, sys, tempfile, time, unittest, shutil
from unittest.mock import patch
import build, release

class ReleaseBoundaryTests(unittest.TestCase):
    def test_repeat_local_delivery_rejects_missing_or_damaged_attachments(self):
        # Exercise attachment integrity on real temporary files. ZIP decoding
        # and signed-bundle round trips are tested by the real packaging command.
        with tempfile.TemporaryDirectory() as directory:
            root=pathlib.Path(directory)
            archive=root/'local.zip';archive.write_bytes(b'archive-integrity-fixture')
            notice=root/'安装说明.txt';notice.write_text('local installation notice')
            checksum=root/'local.zip.sha256'
            sha=release.digest(archive)
            checksum.write_text(sha+'  local.zip\n')
            report=dict(candidateSHA256='candidate',sha256=sha,
                        installationNoticeSHA256=release.digest(notice))
            (root/'package.json').write_text(json.dumps(report))
            self.assertEqual(release.validate_local_delivery(root,'local.zip','candidate'),report)
            checksum.write_text('wrong checksum\n')
            with self.assertRaisesRegex(RuntimeError,'checksum'):
                release.validate_local_delivery(root,'local.zip','candidate')
            checksum.unlink()
            with self.assertRaisesRegex(RuntimeError,'missing'):
                release.validate_local_delivery(root,'local.zip','candidate')
            checksum.write_text(sha+'  local.zip\n')
            notice.write_text('changed installation notice')
            with self.assertRaisesRegex(RuntimeError,'artifacts'):
                release.validate_local_delivery(root,'local.zip','candidate')
            with self.assertRaisesRegex(RuntimeError,'candidate'):
                release.validate_local_delivery(root,'local.zip','different-candidate')

    def test_build_failure_cleans_staging_and_preserves_previous_candidate(self):
        icon=build.ROOT/build.ICON_SOURCE
        for failing in ('go','lipo','codesign'):
            with self.subTest(failing=failing), tempfile.TemporaryDirectory() as directory:
                root=pathlib.Path(directory);source=root/build.ICON_SOURCE
                source.parent.mkdir(parents=True);shutil.copyfile(icon,source)
                app=root/'.build/Focus.app';app.mkdir(parents=True)
                marker=app/'previous';marker.write_bytes(b'previous verified candidate')
                original_run=build.run
                def injected_run(args,**kwargs):
                    if args[0]==failing:raise subprocess.CalledProcessError(1,args)
                    if args[0]=='go':
                        # Isolated dependency stand-in, never published or executed.
                        pathlib.Path(args[args.index('-o')+1]).write_bytes(b'unit-test compiler output')
                    elif args[0]=='lipo':
                        pathlib.Path(args[-1]).write_bytes(b'unit-test merged output')
                    else:original_run(args,**kwargs)
                manifest=dict(version=1,scenes=[],guide=dict(path='media/guide',sha256='0'*64))
                with patch.object(build,'ROOT',root),patch.object(build,'APP',app),patch.object(build,'release_manifest',return_value=(manifest,{})),patch.object(build,'source_fingerprint',return_value=('unit-test',{})),patch.object(build,'run',side_effect=injected_run),patch.object(build,'verify_architectures'):
                    with self.assertRaises(subprocess.CalledProcessError):build.main()
                self.assertEqual(marker.read_bytes(),b'previous verified candidate')
                self.assertEqual(list(app.parent.glob('focus-build-*')),[])
    def test_build_and_release_entry_points_serialize_candidates(self):
        # Exercise the real entry points and OS lock in separate processes.
        # Only their expensive work is replaced; it must not start concurrently.
        with tempfile.TemporaryDirectory() as directory:
            root=pathlib.Path(directory)
            script="""import sys,pathlib;sys.path.insert(0,'scripts');import build,release
build.APP=pathlib.Path(sys.argv[1])/'Focus.app'
def work():
 pathlib.Path(sys.argv[2]).write_text('entered');sys.stdin.readline();return 0
build.build_candidate=work;release.release_candidate=work
(build.main if sys.argv[3]=='build' else release.main)()
"""
            first=subprocess.Popen([sys.executable,'-c',script,directory,str(root/'first'),'build'],stdin=subprocess.PIPE,text=True)
            second=None
            try:
                deadline=time.monotonic()+5
                while not (root/'first').exists() and time.monotonic()<deadline:time.sleep(.01)
                self.assertTrue((root/'first').exists())
                second=subprocess.Popen([sys.executable,'-c',script,directory,str(root/'second'),'release'],stdin=subprocess.PIPE,text=True)
                time.sleep(.15)
                self.assertFalse((root/'second').exists(),'release entered while build owns the candidate')
                first.communicate('\n',timeout=5)
                deadline=time.monotonic()+5
                while not (root/'second').exists() and time.monotonic()<deadline:time.sleep(.01)
                self.assertTrue((root/'second').exists())
                second.communicate('\n',timeout=5)
                self.assertEqual(first.returncode,0);self.assertEqual(second.returncode,0)
            finally:
                for child in (first,second):
                    if child is not None and child.poll() is None:child.kill();child.wait()
    def test_missing_actual_evidence_cannot_create_release(self):
        with tempfile.TemporaryDirectory() as directory:
            with patch.object(release,'APP',pathlib.Path(directory)/'Focus.app'):
                self.assertTrue(release.readiness(pathlib.Path(directory)/'missing.json'))

if __name__=='__main__':unittest.main()
