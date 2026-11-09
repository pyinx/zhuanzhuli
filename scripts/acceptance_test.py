import pathlib
import plistlib
import json
import subprocess
import tempfile
import unittest

import acceptance
from diagnostics import copy_app, stop_owned


class AcceptanceLauncherTests(unittest.TestCase):
    def make_bundle(self, root, identity):
        candidate = root / "Source.app"
        binary = candidate / "Contents/MacOS/Focus"
        binary.parent.mkdir(parents=True)
        source = root / "arguments.go"
        source.write_text('package main\nimport("fmt";"os";"time")\nfunc main(){for _,s:=range os.Args[1:]{fmt.Println(s)};if len(os.Args)>1&&os.Args[len(os.Args)-1]=="--wait"{time.Sleep(time.Hour)}}\n')
        subprocess.run(["go", "build", "-o", str(binary), str(source)],
                       check=True, capture_output=True, text=True)
        (candidate / "Contents/Info.plist").write_bytes(plistlib.dumps(dict(
            CFBundleIdentifier=identity, CFBundleExecutable=binary.name,
            CFBundlePackageType="APPL", CFBundleName="专注力")))
        subprocess.run(["codesign", "--force", "--sign", "-", str(candidate)],
                       check=True, capture_output=True, text=True)
        return candidate

    def test_product_copies_have_separate_authorization_identities(self):
        with tempfile.TemporaryDirectory(prefix="focus-copy-test-") as directory:
            root = pathlib.Path(directory)
            source = self.make_bundle(root, "com.yunsiweilai.zhuanzhuli")
            original = {p.relative_to(source): p.read_bytes() for p in source.rglob("*") if p.is_file()}
            identities = []
            for name in ("First.app", "Second.app"):
                target = copy_app(source, root / name, "Focus")
                info = plistlib.loads((target / "Contents/Info.plist").read_bytes())
                identities.append(info["CFBundleIdentifier"])
                self.assertNotEqual(info["CFBundleIdentifier"], "com.yunsiweilai.zhuanzhuli")
                self.assertEqual(info["CFBundleExecutable"], "Focus")
                self.assertIn("验收", info.get("CFBundleDisplayName", ""))
                subprocess.run(["codesign", "--verify", "--strict", str(target)], check=True, capture_output=True)
                result = subprocess.run([str(target / "Contents/MacOS/Focus"), "literal argument"],
                                        capture_output=True, text=True, timeout=10)
                self.assertEqual(result.returncode, 0, result.stderr)
                self.assertEqual(result.stdout, "literal argument\n")
            self.assertEqual(len(set(identities)), 2)
            self.assertEqual(original, {p.relative_to(source): p.read_bytes() for p in source.rglob("*") if p.is_file()})

    def test_fixture_identity_and_bytes_are_preserved(self):
        with tempfile.TemporaryDirectory(prefix="focus-fixture-copy-test-") as directory:
            root = pathlib.Path(directory)
            source = self.make_bundle(root, "local.focusprobe.fixture.a")
            target = copy_app(source, root / "FixtureA.app", "Focus")
            self.assertEqual({p.relative_to(source): p.read_bytes() for p in source.rglob("*") if p.is_file()},
                             {p.relative_to(target): p.read_bytes() for p in target.rglob("*") if p.is_file()})

    def test_cleanup_accepts_only_matching_bundle_identity(self):
        with tempfile.TemporaryDirectory(prefix="focus-owner-test-") as directory:
            root = pathlib.Path(directory)
            app = copy_app(self.make_bundle(root, "com.yunsiweilai.zhuanzhuli"), root / "Host.app", "Focus")
            data = root / "data"
            data.mkdir()
            identity = plistlib.loads((app / "Contents/Info.plist").read_bytes())["CFBundleIdentifier"]
            child = subprocess.Popen([str(app / "Contents/MacOS/Focus"), "--data-dir", str(data), "--wait"], stdout=subprocess.DEVNULL)
            try:
                record = dict(pid=child.pid, path=str(app.resolve()), bundleID="other.application")
                (data / "instance.json").write_text(json.dumps(record))
                with self.assertRaises(RuntimeError):
                    stop_owned(app, data, child)
                self.assertIsNone(child.poll())
                record["bundleID"] = identity
                (data / "instance.json").write_text(json.dumps(record))
                stop_owned(app, data, child)
                self.assertIsNotNone(child.poll())
            finally:
                if child.poll() is None:
                    child.kill()
                    child.wait()

    def test_cleanup_refuses_data_prefix_collision_and_duplicate_directory(self):
        with tempfile.TemporaryDirectory(prefix="focus-owner-args-test-") as directory:
            root = pathlib.Path(directory)
            app = copy_app(self.make_bundle(root, "com.yunsiweilai.zhuanzhuli"), root / "Host.app", "Focus")
            data = root / "data"
            data.mkdir()
            identity = plistlib.loads((app / "Contents/Info.plist").read_bytes())["CFBundleIdentifier"]
            for arguments in (["--data-dir", str(data) + "-other", "--wait"],
                              ["--data-dir", str(data), "--data-dir", str(root / "other"), "--wait"],
                              ["--data-dir", str(data), "--data-dir=" + str(root / "other"), "--wait"],
                              ["--data-dir", str(data), "-data-dir", str(root / "other"), "--wait"],
                              ["--data-dir", str(data), "-data-dir=" + str(root / "other"), "--wait"]):
                with self.subTest(arguments=arguments):
                    child = subprocess.Popen([str(app / "Contents/MacOS/Focus"), *arguments], stdout=subprocess.DEVNULL)
                    try:
                        (data / "instance.json").write_text(json.dumps(dict(pid=child.pid, path=str(app.resolve()), bundleID=identity)))
                        with self.assertRaises(RuntimeError):
                            stop_owned(app, data, child)
                        self.assertIsNone(child.poll())
                    finally:
                        if child.poll() is None:
                            child.kill()
                        child.wait()

    def test_cleanup_refuses_ambiguous_spaced_directory(self):
        with tempfile.TemporaryDirectory(prefix="focus-owner-space-test-") as directory:
            root = pathlib.Path(directory)
            app = copy_app(self.make_bundle(root, "com.yunsiweilai.zhuanzhuli"), root / "Host.app", "Focus")
            data = root / "data split"
            data.mkdir()
            identity = plistlib.loads((app / "Contents/Info.plist").read_bytes())["CFBundleIdentifier"]
            child = subprocess.Popen([str(app / "Contents/MacOS/Focus"), "--data-dir", str(root / "data"), "split", "--wait"], stdout=subprocess.DEVNULL)
            try:
                (data / "instance.json").write_text(json.dumps(dict(pid=child.pid, path=str(app.resolve()), bundleID=identity)))
                with self.assertRaises(RuntimeError):
                    stop_owned(app, data, child)
                self.assertIsNone(child.poll())
            finally:
                if child.poll() is None:
                    child.kill()
                child.wait()

    def test_wrapped_bundle_keeps_valid_signature_and_literal_arguments(self):
        # A real signed Mach-O exercises both seals without a previous app build.
        # This argument printer is only a disposable launcher test fixture.
        with tempfile.TemporaryDirectory(prefix="focus-launcher-test-") as directory:
            root = pathlib.Path(directory)
            candidate = self.make_bundle(root, "local.focusprobe.launcher-test").rename(root / "Launcher fixture.app")
            arguments = ["--data-dir", str(root / "isolated data"), "literal $(exit 17) `exit 19`"]
            acceptance.wrapper(candidate, "Focus", arguments)
            result = subprocess.run(["codesign", "--verify", "--strict", str(candidate)], capture_output=True, text=True)
            self.assertEqual(result.returncode, 0, result.stderr)
            result = subprocess.run([str(candidate / "Contents/MacOS/AcceptanceLauncher")], capture_output=True, text=True, timeout=15)
            self.assertEqual(result.returncode, 0, result.stderr)
            self.assertEqual(result.stdout, "\n".join(arguments) + "\n")


if __name__ == "__main__":
    unittest.main()
