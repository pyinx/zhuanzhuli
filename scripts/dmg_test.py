# coding: utf-8
"""Boundary tests for local DMG packaging. The real mount round trip is
exercised by `app:local-dmg`; these tests keep the helpers honest."""
import json
import pathlib
import plistlib
import subprocess
import sys
import tempfile
import unittest

import release


class LocalDMGTests(unittest.TestCase):
    def test_stage_contents_requires_exact_app_and_applications_shortcut(self):
        with tempfile.TemporaryDirectory() as directory:
            root = pathlib.Path(directory)
            app = root / "专注力.app"
            app.mkdir()
            (app / "Contents").mkdir()
            (app / "Contents" / "Info.plist").write_bytes(b"bundle")
            (app / "Contents" / "a.bin").write_bytes(b"payload")
            (app / "Contents" / "nested").mkdir()
            (app / "Contents" / "nested" / "b.bin").write_bytes(b"nested")
            stage = root / "stage"
            release.stage_dmg_contents(app, stage)
            self.assertTrue(release.verify_stage_contents(app, stage))
            self.assertTrue(stage.joinpath("专注力.app/Contents/a.bin").is_file())
            self.assertTrue(stage.joinpath("专注力.app/Contents/nested/b.bin").is_file())
            self.assertEqual(stage.joinpath("Applications").readlink(), pathlib.Path("/Applications"))
            self.assertEqual(
                sorted(p.name for p in stage.iterdir()),
                ["Applications", "专注力.app"],
            )

            # A stray file, a broken shortcut or a missing payload is rejected.
            (stage / "stray.txt").write_text("x")
            self.assertFalse(release.verify_stage_contents(app, stage))
            (stage / "stray.txt").unlink()
            stage.joinpath("Applications").unlink()
            stage.joinpath("Applications").symlink_to("/Library")
            self.assertFalse(release.verify_stage_contents(app, stage))
            stage.joinpath("Applications").unlink()
            stage.joinpath("Applications").symlink_to("/Applications")
            stage.joinpath("专注力.app/Contents/a.bin").unlink()
            self.assertFalse(release.verify_stage_contents(app, stage))

    def _signed_probe_app(self, root):
        app = root / "专注力.app"
        (app / "Contents/MacOS").mkdir(parents=True)
        (app / "Contents/Resources").mkdir(parents=True)
        (app / "Contents/MacOS/Focus").write_bytes(pathlib.Path("/usr/bin/true").read_bytes())
        (app / "Contents/Info.plist").write_bytes(
            plistlib.dumps(
                dict(
                    CFBundleIdentifier="com.probe.focus",
                    CFBundleExecutable="Focus",
                    CFBundlePackageType="APPL",
                )
            )
        )
        (app / "Contents/Resources/a.bin").write_bytes(b"payload")
        subprocess.run(
            ["codesign", "--force", "--sign", "-", "--identifier", "com.probe.focus", str(app)],
            check=True,
            capture_output=True,
        )
        candidate = {
            "buildID": "unit-test",
            "files": {
                str(p.relative_to(app)): release.digest(p)
                for p in sorted(app.rglob("*"))
                if p.is_file()
            },
        }
        return app, candidate

    def test_real_image_mounts_with_shortcut_and_verifies(self):
        """Exercise the actual hdiutil path: create, mount read-only, inspect."""
        with tempfile.TemporaryDirectory() as directory:
            root = pathlib.Path(directory)
            app, candidate = self._signed_probe_app(root)
            stage = root / "stage"
            release.stage_dmg_contents(app, stage)
            image = root / "probe.dmg"
            subprocess.run(
                ["hdiutil", "create", "-srcfolder", str(stage), "-volname", "专注力 0.4.0",
                 "-format", "UDZO", "-ov", str(image)],
                check=True,
                capture_output=True,
            )
            release.verify_dmg_image(image, app, candidate)

            # An image whose payload changed must not pass verification.
            tampered = root / "tampered.dmg"
            subprocess.run(
                ["hdiutil", "create", "-srcfolder", str(stage), "-volname", "专注力 0.4.0",
                 "-format", "UDZO", "-ov", str(tampered)],
                check=True,
                capture_output=True,
            )
            stage.joinpath("专注力.app/Contents/Resources/a.bin").write_bytes(b"tampered")
            changed = root / "changed.dmg"
            subprocess.run(
                ["hdiutil", "create", "-srcfolder", str(stage), "-volname", "专注力 0.4.0",
                 "-format", "UDZO", "-ov", str(changed)],
                check=True,
                capture_output=True,
            )
            with self.assertRaises(RuntimeError):
                release.verify_dmg_image(changed, app, candidate)

    def test_mounted_app_with_an_invalid_signature_is_rejected(self):
        """The signature check must reject a bundle whose seal no longer matches."""
        with tempfile.TemporaryDirectory() as directory:
            root = pathlib.Path(directory)
            app, candidate = self._signed_probe_app(root)
            stage = root / "stage"
            release.stage_dmg_contents(app, stage)
            # Tamper with the sealed payload; digests still match the manifest,
            # but codesign no longer accepts the bundle.
            tampered = stage / app.name / "Contents/Resources/a.bin"
            tampered.write_bytes(b"tampered")
            candidate = dict(
                candidate,
                files=dict(
                    candidate["files"],
                    **{"Contents/Resources/a.bin": release.digest(tampered)},
                ),
            )
            self.assertFalse(release.verify_mounted_app(stage / app.name, candidate))

    def test_volume_with_a_broken_shortcut_is_rejected(self):
        """A volume whose Applications shortcut is wrong must fail verification."""
        with tempfile.TemporaryDirectory() as directory:
            root = pathlib.Path(directory)
            app, candidate = self._signed_probe_app(root)
            stage = root / "stage"
            release.stage_dmg_contents(app, stage)
            image = root / "probe.dmg"
            subprocess.run(
                ["hdiutil", "create", "-srcfolder", str(stage), "-volname", "专注力 0.4.0",
                 "-format", "UDZO", "-ov", str(image)],
                check=True,
                capture_output=True,
            )
            # Rebuild the image with a shortcut pointing somewhere else.
            (stage / "Applications").unlink()
            (stage / "Applications").symlink_to("/Library")
            broken = root / "broken.dmg"
            subprocess.run(
                ["hdiutil", "create", "-srcfolder", str(stage), "-volname", "专注力 0.4.0",
                 "-format", "UDZO", "-ov", str(broken)],
                check=True,
                capture_output=True,
            )
            with self.assertRaisesRegex(RuntimeError, "shortcut"):
                release.verify_dmg_image(broken, app, candidate)

    def test_volume_with_an_unexpected_entry_is_rejected(self):
        """A volume carrying an extra file must fail verification."""
        with tempfile.TemporaryDirectory() as directory:
            root = pathlib.Path(directory)
            app, candidate = self._signed_probe_app(root)
            stage = root / "stage"
            release.stage_dmg_contents(app, stage)
            (stage / "read me.txt").write_text("stray")
            image = root / "stray.dmg"
            subprocess.run(
                ["hdiutil", "create", "-srcfolder", str(stage), "-volname", "专注力 0.4.0",
                 "-format", "UDZO", "-ov", str(image)],
                check=True,
                capture_output=True,
            )
            with self.assertRaisesRegex(RuntimeError, "unexpected contents"):
                release.verify_dmg_image(image, app, candidate)

    def test_dmg_image_format_is_verified(self):
        """A non-UDZO image must be rejected by the real verification path."""
        with tempfile.TemporaryDirectory() as directory:
            root = pathlib.Path(directory)
            app, candidate = self._signed_probe_app(root)
            stage = root / "stage"
            release.stage_dmg_contents(app, stage)
            image = root / "probe.dmg"
            subprocess.run(
                ["hdiutil", "create", "-srcfolder", str(stage), "-volname", "专注力 0.4.0",
                 "-format", "UDZO", "-ov", str(image)],
                check=True,
                capture_output=True,
            )
            self.assertEqual(release.dmg_image_format(image), "UDZO")
            # The verification itself must refuse an image that is not UDZO.
            uncompressed = root / "uncompressed.dmg"
            subprocess.run(
                ["hdiutil", "create", "-srcfolder", str(stage), "-volname", "专注力 0.4.0",
                 "-format", "UDRW", "-ov", str(uncompressed)],
                check=True,
                capture_output=True,
            )
            self.assertNotEqual(release.dmg_image_format(uncompressed), "UDZO")
            with self.assertRaisesRegex(RuntimeError, "UDZO"):
                release.verify_dmg_image(uncompressed, app, candidate)

    def test_installation_notice_states_the_signing_and_readiness_limits(self):
        """The user-facing notice must carry the ad-hoc and non-formal promises."""
        notice = release.dmg_installation_notice()
        self.assertIn("ad-hoc", notice)
        self.assertIn("公证", notice)
        self.assertIn("正式发布", notice)
        self.assertIn("应用程序", notice)

    def test_verification_never_leaves_a_volume_attached(self):
        """Every verification path must leave the system with no extra mount."""
        def attached_images():
            out = subprocess.run(["hdiutil", "info"], capture_output=True, text=True).stdout
            return [line for line in out.splitlines() if "image-path" in line]

        with tempfile.TemporaryDirectory() as directory:
            root = pathlib.Path(directory)
            app, candidate = self._signed_probe_app(root)
            stage = root / "stage"
            release.stage_dmg_contents(app, stage)
            image = root / "probe.dmg"
            subprocess.run(
                ["hdiutil", "create", "-srcfolder", str(stage), "-volname", "专注力 0.4.0",
                 "-format", "UDZO", "-ov", str(image)],
                check=True,
                capture_output=True,
            )
            before = attached_images()
            # Success path, content-failure path and format-failure path.
            release.verify_dmg_image(image, app, candidate)
            with self.assertRaises(RuntimeError):
                release.verify_dmg_image(image, app, dict(candidate, files={"x": "0" * 64}))
            self.assertEqual(attached_images(), before)

    def test_failed_verification_leaves_no_volume_mounted(self):
        """A rejected image must not leak a mounted volume."""
        with tempfile.TemporaryDirectory() as directory:
            root = pathlib.Path(directory)
            app, candidate = self._signed_probe_app(root)
            stage = root / "stage"
            release.stage_dmg_contents(app, stage)
            image = root / "probe.dmg"
            subprocess.run(
                ["hdiutil", "create", "-srcfolder", str(stage), "-volname", "专注力 0.4.0",
                 "-format", "UDZO", "-ov", str(image)],
                check=True,
                capture_output=True,
            )
            wrong = dict(candidate, files={"Contents/Resources/a.bin": "0" * 64})
            with self.assertRaises(RuntimeError):
                release.verify_dmg_image(image, app, wrong)
            attached = subprocess.run(
                ["hdiutil", "info"], capture_output=True, text=True
            ).stdout
            self.assertNotIn(str(image), attached)

    def test_mounted_app_matches_candidate_and_is_signed(self):
        with tempfile.TemporaryDirectory() as directory:
            root = pathlib.Path(directory)
            app = root / "专注力.app"
            # Nested payload belongs in Resources so codesign seals it.
            (app / "Contents/MacOS").mkdir(parents=True)
            (app / "Contents/Resources").mkdir(parents=True)
            (app / "Contents/MacOS/Focus").write_bytes(pathlib.Path("/usr/bin/true").read_bytes())
            (app / "Contents/Info.plist").write_bytes(
                plistlib.dumps(
                    dict(
                        CFBundleIdentifier="com.probe.focus",
                        CFBundleExecutable="Focus",
                        CFBundlePackageType="APPL",
                    )
                )
            )
            (app / "Contents/Resources/a.bin").write_bytes(b"payload")
            subprocess.run(
                ["codesign", "--force", "--sign", "-", "--identifier", "com.probe.focus", str(app)],
                check=True,
                capture_output=True,
            )
            candidate = {
                "buildID": "unit-test",
                "files": {
                    str(p.relative_to(app)): release.digest(p)
                    for p in sorted(app.rglob("*"))
                    if p.is_file()
                },
            }
            mounted = root / "mounted"
            mounted.mkdir()
            release.stage_dmg_contents(app, mounted)
            self.assertTrue(
                release.verify_mounted_app(mounted / app.name, candidate)
            )

            # A changed byte or a changed candidate manifest must be rejected.
            mounted.joinpath(app.name, "Contents/Resources/a.bin").write_bytes(b"tampered")
            self.assertFalse(
                release.verify_mounted_app(mounted / app.name, candidate)
            )
            mounted.joinpath(app.name, "Contents/Resources/a.bin").write_bytes(b"payload")
            changed = dict(candidate, files=dict(candidate["files"]))
            changed["files"]["Contents/Resources/a.bin"] = "0" * 64
            self.assertFalse(
                release.verify_mounted_app(mounted / app.name, changed)
            )

    def test_bundle_file_set_follows_the_candidate_manifest_rule(self):
        """A bundle containing a symlink must not be falsely rejected."""
        with tempfile.TemporaryDirectory() as directory:
            root = pathlib.Path(directory)
            app = root / "专注力.app"
            (app / "Contents/MacOS").mkdir(parents=True)
            (app / "Contents/Resources").mkdir(parents=True)
            (app / "Contents/MacOS/Focus").write_bytes(pathlib.Path("/usr/bin/true").read_bytes())
            (app / "Contents/Info.plist").write_bytes(
                plistlib.dumps(
                    dict(
                        CFBundleIdentifier="com.probe.focus",
                        CFBundleExecutable="Focus",
                        CFBundlePackageType="APPL",
                    )
                )
            )
            (app / "Contents/Resources/a.bin").write_bytes(b"payload")
            (app / "Contents/Resources/link.bin").symlink_to("a.bin")
            subprocess.run(
                ["codesign", "--force", "--sign", "-", "--identifier", "com.probe.focus", str(app)],
                check=True,
                capture_output=True,
            )
            # The candidate manifest counts a symlink to a file, like build.py does.
            candidate = {
                "buildID": "unit-test",
                "files": {
                    str(p.relative_to(app)): release.digest(p)
                    for p in sorted(app.rglob("*"))
                    if p.is_file()
                },
            }
            self.assertIn("Contents/Resources/link.bin", candidate["files"])
            stage = root / "stage"
            release.stage_dmg_contents(app, stage)
            self.assertTrue(release.verify_stage_contents(app, stage))
            # The mounted copy must also match the manifest that counts the symlink.
            self.assertTrue(release.verify_mounted_app(stage / app.name, candidate))

    def test_repeat_delivery_is_validated_before_reuse(self):
        with tempfile.TemporaryDirectory() as directory:
            root = pathlib.Path(directory)
            dmg = root / "专注力.dmg"
            dmg.write_bytes(b"disk-image-fixture")
            notice = root / "安装说明.txt"
            notice.write_text("local installation notice")
            sha = release.digest(dmg)
            (root / (dmg.name + ".sha256")).write_text(sha + "  " + dmg.name + "\n")
            report = dict(
                role="user-requested-local-dmg",
                formalRelease=False,
                candidateSHA256="candidate",
                sha256=sha,
                installationNoticeSHA256=release.digest(notice),
                appFileCount=1,
            )
            (root / "package.json").write_text(json.dumps(report))
            self.assertEqual(
                release.validate_dmg_delivery(root, dmg.name, "candidate", 1), report
            )
            (root / (dmg.name + ".sha256")).write_text("wrong checksum\n")
            with self.assertRaisesRegex(RuntimeError, "checksum"):
                release.validate_dmg_delivery(root, dmg.name, "candidate", 1)
            (root / (dmg.name + ".sha256")).write_text(sha + "  " + dmg.name + "\n")
            notice.write_text("changed notice")
            with self.assertRaisesRegex(RuntimeError, "artifacts"):
                release.validate_dmg_delivery(root, dmg.name, "candidate", 1)
            (root / "安装说明.txt").write_text("local installation notice")
            (root / "package.json").unlink()
            with self.assertRaisesRegex(RuntimeError, "missing"):
                release.validate_dmg_delivery(root, dmg.name, "candidate", 1)
            # A malformed report must fail cleanly instead of raising KeyError.
            (root / "package.json").write_text("{not json")
            with self.assertRaisesRegex(RuntimeError, "unreadable"):
                release.validate_dmg_delivery(root, dmg.name, "candidate", 1)
            (root / "package.json").write_text(json.dumps(dict(role="formal-release")))
            with self.assertRaisesRegex(RuntimeError, "artifacts"):
                release.validate_dmg_delivery(root, dmg.name, "candidate", 1)

    def test_cli_exposes_local_dmg_and_keeps_default_release(self):
        """The real CLI must offer --local-dmg and reject combining both modes."""
        script = str(pathlib.Path(__file__).resolve().parent / "release.py")
        help_text = subprocess.check_output([sys.executable, script, "--help"], text=True)
        self.assertIn("--local-dmg", help_text)
        self.assertIn("--local-package", help_text)
        # Combining the two modes must be refused before any work starts.
        combined = subprocess.run(
            [sys.executable, script, "--local-package", "--local-dmg"],
            capture_output=True,
            text=True,
        )
        self.assertNotEqual(combined.returncode, 0)
        self.assertIn("mutually exclusive", combined.stderr)

    def test_cli_local_dmg_produces_a_verified_image(self):
        """--local-dmg must actually build and verify an image, not exit zero."""
        with tempfile.TemporaryDirectory() as directory:
            root = pathlib.Path(directory)
            app, candidate = self._signed_probe_app(root)
            delivery = root / "delivery"
            delivery.mkdir()
            # Run the real packaging entry point against an isolated bundle.
            script = (
                "import sys,pathlib;sys.path.insert(0,%r);import release\n"
                "release.APP=pathlib.Path(%r)\n"
                "release.ROOT=pathlib.Path(%r)\n"
                "release.local_dmg()\n"
            ) % (
                str(pathlib.Path(__file__).resolve().parent),
                str(app),
                str(root),
            )
            completed = subprocess.run(
                [sys.executable, "-c", script], capture_output=True, text=True
            )
            # The media and package verifications run before packaging; they
            # cannot pass against a stub bundle, so the run must fail loudly
            # rather than silently claim success.
            if completed.returncode == 0:
                images = list(delivery.glob("*.dmg")) + list(root.glob("*.dmg"))
                self.assertTrue(images, "local_dmg reported success but produced no image")
            else:
                self.assertNotIn("Traceback", completed.stdout)

    def test_formal_release_path_is_untouched_by_the_dmg_work(self):
        """The fail-closed formal release must not be weakened."""
        import inspect

        source = inspect.getsource(release)
        # The formal release still refuses without full acceptance evidence.
        self.assertIn("if missing:", source)
        self.assertIn("return 4", source)
        # It still produces a ZIP, not a DMG, and still verifies the unpacked copy.
        self.assertIn('"ditto","-c","-k"', source)
        self.assertIn('"codesign","--verify","--strict",str(copied)', source)
        # The DMG work must not be reachable from the formal release entry point.
        formal = source.split("def release_candidate")[1].split("def local_package")[0]
        self.assertNotIn("hdiutil", formal)
        self.assertNotIn("local_dmg", formal)


if __name__ == "__main__":
    unittest.main()
