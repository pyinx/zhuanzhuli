"""Whole-track program invariants, without production audio caches."""
import copy
import json
import unittest
import numpy as np
import scene_programs as programs


class WholeTrackProgramTests(unittest.TestCase):
    def setUp(self):
        self.plan = json.loads(programs.PLAN.read_text())
        self.catalog = json.loads(programs.CATALOG.read_text())['tracks']

    def test_reordering_same_tracks_is_rejected_as_another_scene(self):
        self.plan['scenes'][1]['tracks'] = self.plan['scenes'][0]['tracks'][::-1]
        with self.assertRaisesRegex(ValueError, 'same group'):
            programs.validate_plan(self.plan, self.catalog)

    def test_renamed_source_hash_is_not_a_distinct_original(self):
        a, b = self.plan['scenes'][0]['tracks'][:2]
        self.catalog[b]['sha256'] = self.catalog[a]['sha256']
        with self.assertRaisesRegex(ValueError, 'distinct complete'):
            programs.validate_plan(self.plan, self.catalog)

    def test_whole_tracks_cover_every_frame_and_preserve_input(self):
        a = np.tile([.1, 0], (12, 1)).astype(np.float32)
        b = np.tile([0, .2], (10, 1)).astype(np.float32)
        original = a.copy()
        mix, timeline = programs.whole.join_whole_tracks([a, b], 3)
        self.assertEqual(len(mix), 16)
        self.assertEqual(timeline[1]['source_coverage_frames'], [0, 10])
        np.testing.assert_allclose(mix[10], [.1 / np.sqrt(2), .2 / np.sqrt(2)])
        np.testing.assert_array_equal(original, a)

    def test_natural_excerpts_do_not_overlap_themselves_or_loop_join(self):
        for scene in self.plan['scenes']:
            events = programs.nature_events(scene, 900 * programs.RATE, .04)
            self.assertEqual(len(events), len(scene['nature_positions']))
            for a, b in zip(events, events[1:]):
                self.assertLessEqual(a['offset'] + a['seconds'], b['offset'])
            self.assertTrue(all(e['at'] >= 12 and e['at'] + e['seconds'] <= 888 for e in events))

    def test_natural_excerpt_cannot_extend_past_cycle(self):
        scene = copy.deepcopy(self.plan['scenes'][3])
        with self.assertRaisesRegex(ValueError, 'loop join'):
            programs.nature_events(scene, 120 * programs.RATE, .04)

    def test_mechanical_even_natural_event_spacing_is_rejected(self):
        self.plan['scenes'][0]['nature_positions'] = [25, 125, 225, 325]
        with self.assertRaisesRegex(ValueError, 'irregular'):
            programs.validate_plan(self.plan, self.catalog)


if __name__ == '__main__':
    unittest.main()
