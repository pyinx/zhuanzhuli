"""Offline score and source boundaries, independent of private download caches."""
import copy
import pathlib
import tempfile
import unittest

import numpy as np
import scene_music


class FocusMusicBoundaryTests(unittest.TestCase):
    def score(self):
        return scene_music.load_focus_score()

    def test_previous_two_minute_contract_is_rejected(self):
        score = self.score()
        score['seconds'] = 120
        with self.assertRaisesRegex(ValueError, '600'):
            scene_music.validate_focus_score(score)

    def test_nonfinite_or_sustained_drone_notes_are_rejected(self):
        for duration in (float('nan'), 0, 30):
            with self.subTest(duration=duration):
                score = self.score()
                score['sections'][0]['note_seconds'] = duration
                with self.assertRaises(ValueError):
                    scene_music.validate_focus_score(score)

    def test_copying_a_whole_section_is_not_a_new_variation(self):
        score = self.score()
        score['sections'][1]['melody'] = copy.deepcopy(score['sections'][0]['melody'])
        with self.assertRaisesRegex(ValueError, 'duplicate'):
            scene_music.validate_focus_score(score)

    def test_loop_tail_overlays_the_head_without_truncating_or_silencing_it(self):
        # A real short numerical signal tests the circular editing boundary.
        signal = np.array([[1., 2.], [3., 4.], [5., 6.], [7., 8.], [9., 10.]])
        folded = scene_music.fold_tail(signal, 3)
        np.testing.assert_array_equal(folded, [[8., 10.], [12., 14.], [5., 6.]])
        np.testing.assert_array_equal(signal[0], [1., 2.])

    def test_source_tampering_and_cache_escape_are_refused(self):
        import hashlib
        with tempfile.TemporaryDirectory() as directory:
            root = pathlib.Path(directory)
            source = root / 'instrument.sf2'
            source.write_bytes(b'original source')
            item = dict(path='instrument.sf2', sha256=hashlib.sha256(source.read_bytes()).hexdigest(),
                        license='CC0-1.0', author='fixture author', page='https://example.org/source')
            self.assertEqual(scene_music.checked_source(root, item), source.resolve())
            source.write_bytes(b'changed')
            with self.assertRaisesRegex(ValueError, 'hash'):
                scene_music.checked_source(root, item)
            item['path'] = '../outside.sf2'
            with self.assertRaisesRegex(ValueError, 'path'):
                scene_music.checked_source(root, item)

    def test_unknown_license_and_missing_attribution_are_refused(self):
        item = dict(path='file', sha256='0'*64, license='personal-use', author='author', page='https://example.org')
        with self.assertRaisesRegex(ValueError, 'license'):
            scene_music.checked_source(pathlib.Path('.'), item)
        item.update(license='CC-BY-4.0', author='')
        with self.assertRaisesRegex(ValueError, 'author'):
            scene_music.checked_source(pathlib.Path('.'), item)

    def test_long_file_made_by_pasting_a_short_waveform_is_detected(self):
        rng = np.random.default_rng(9)
        source = rng.normal(size=(12000, 2)).astype(np.float32)
        pasted = np.tile(source, (5, 1))
        self.assertIn([0, 120], scene_music.duplicate_audio_windows(pasted, sample_rate=100))
        changing = rng.normal(size=(60000, 2)).astype(np.float32)
        self.assertEqual(scene_music.duplicate_audio_windows(changing, sample_rate=100), [])

    def test_off_grid_repeats_and_gain_changes_are_detected(self):
        rng = np.random.default_rng(12)
        for seconds in (31, 120, 150, 300):
            with self.subTest(seconds=seconds):
                source = rng.normal(size=(seconds * 100, 2)).astype(np.float32)
                cycles = [source * (1 + .01 * i) for i in range(21)]
                pasted = np.concatenate(cycles)[:60000]
                self.assertTrue(scene_music.duplicate_audio_windows(pasted, sample_rate=100))

    def test_cross_window_silence_is_detected_but_short_rests_are_allowed(self):
        audio = np.full((6000, 2), .02, dtype=np.float32)
        audio[500:1500] = 0
        self.assertGreaterEqual(scene_music.longest_quiet_seconds(audio, 100), 10)
        audio[500:1500] = .02
        audio[500:700] = 0
        self.assertLess(scene_music.longest_quiet_seconds(audio, 100), 3)
        audio[-500:] = 0
        audio[:500] = 0
        self.assertGreaterEqual(scene_music.longest_quiet_seconds(audio, 100), 10)

    def test_same_pitch_overlaps_use_independent_channels(self):
        events = scene_music.focus_events(self.score(), 'piano')
        self.assertEqual({event[4] for event in events}, {0, 1})
        scene_music.validate_note_lifetimes(events)
        with self.assertRaisesRegex(ValueError, 'overlap'):
            scene_music.validate_note_lifetimes([(0, 2, 61, 45, 0), (1, 2, 61, 45, 0)])


if __name__ == '__main__':
    unittest.main()
