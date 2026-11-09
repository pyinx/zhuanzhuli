# coding: utf-8
import hashlib
import json
import pathlib
import tempfile
import unittest
from unittest.mock import patch

import build


class ReleaseManifestNoticesTest(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory(prefix="focus-build-test-")
        self.root = pathlib.Path(self.temporary.name).resolve()
        (self.root / "assets/media").mkdir(parents=True)
        (self.root / "assets/release-media.json").parent.mkdir(parents=True, exist_ok=True)
        self.audio = self._write("assets/media/audio.bin", b"audio")
        self.notice = self._write("assets/media/NOTICE.txt", b"notice")

    def tearDown(self):
        self.temporary.cleanup()

    def _write(self, relative, body):
        path = self.root / relative
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_bytes(body)
        return {
            "source": relative,
            "path": "media/" + pathlib.PurePosixPath(relative).name,
            "sha256": hashlib.sha256(body).hexdigest(),
        }

    def _manifest(self, notices_marker=None):
        manifest = {
            "version": 1,
            "guide": dict(self.audio),
            "scenes": [{
                "id": "forest",
                "name": "森林",
                "preview": dict(self.audio),
                "background": dict(self.audio),
                "entry": dict(self.audio),
                "work": dict(self.audio),
            }],
        }
        if notices_marker is not None:
            manifest["notices"] = notices_marker
        (self.root / "assets/release-media.json").write_text(
            json.dumps(manifest, ensure_ascii=False)
        )
        return manifest

    def _load(self):
        with patch.object(build, "ROOT", self.root):
            return build.release_manifest()

    def test_legacy_manifest_without_notices_still_loads(self):
        self._manifest()
        _, targets = self._load()
        self.assertEqual(set(targets), {"media/audio.bin"})

    def test_four_independent_recordings_are_packaged_and_hash_checked(self):
        manifest = self._manifest()
        manifest['version'] = 2
        manifest['ambiences'] = [dict(id=key, name=key, audio=self._write('assets/media/'+key+'.m4a', key.encode())) for key in ('rain','birds','waves','stream')]
        (self.root/'assets/release-media.json').write_text(json.dumps(manifest))
        _, targets = self._load()
        self.assertEqual(len(targets), 5)
        runtime = build.runtime_manifest(manifest)
        self.assertNotIn('source', runtime['ambiences'][0]['audio'])
        (self.root/'assets/media/rain.m4a').write_bytes(b'corrupt')
        with self.assertRaisesRegex(RuntimeError, 'release media changed'):
            self._load()
        manifest['ambiences'].pop()
        (self.root/'assets/release-media.json').write_text(json.dumps(manifest))
        with self.assertRaisesRegex(RuntimeError, 'exactly four'):
            self._load()

    def test_notice_is_hash_checked_and_copied_but_not_exposed_at_runtime(self):
        manifest = self._manifest([dict(self.notice)])
        _, targets = self._load()
        self.assertEqual(set(targets), {"media/audio.bin", "media/NOTICE.txt"})
        runtime = build.runtime_manifest(manifest)
        self.assertNotIn("notices", runtime)
        self.assertNotIn("source", runtime["guide"])
        self.assertNotIn("source", runtime["scenes"][0]["work"])

    def test_notice_list_type_and_required_fields_are_rejected(self):
        self._manifest({})
        with self.assertRaisesRegex(RuntimeError, "notices must be a list"):
            self._load()
        broken = dict(self.notice)
        broken.pop("source")
        self._manifest([broken])
        with self.assertRaisesRegex(RuntimeError, "invalid release resource entry"):
            self._load()

    def test_notice_traversal_conflict_and_hash_change_are_rejected(self):
        escaped = dict(self.notice, path="media/../NOTICE.txt")
        self._manifest([escaped])
        with self.assertRaisesRegex(RuntimeError, "invalid release resource path"):
            self._load()

        conflict = dict(self.notice, path=self.audio["path"])
        self._manifest([conflict])
        with self.assertRaisesRegex(RuntimeError, "conflicting resource role"):
            self._load()

        bad_hash = dict(self.notice, sha256="0" * 64)
        self._manifest([bad_hash])
        with self.assertRaisesRegex(RuntimeError, "release media changed"):
            self._load()


if __name__ == "__main__":
    unittest.main()
