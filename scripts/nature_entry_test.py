"""Recorded-entry subprocess failures must not leave a blocked decoder."""
import pathlib
import subprocess
import sys
import unittest
from unittest.mock import patch

import nature_entry


class RecordedEntryFailureTests(unittest.TestCase):
    def test_encoder_launch_failure_reaps_the_live_decoder(self):
        decoder = subprocess.Popen(
            [sys.executable, '-c', 'import time; time.sleep(30)'],
            stdout=subprocess.PIPE)
        try:
            self.assertIsNone(decoder.poll())
            with patch.object(nature_entry.subprocess, 'Popen',
                              side_effect=[decoder, OSError('encoder launch denied')]):
                with self.assertRaisesRegex(OSError, 'encoder launch denied'):
                    nature_entry.render_recorded_frames(
                        pathlib.Path('unused-source'), 0, pathlib.Path('unused-output'), 192)
            self.assertIsNotNone(decoder.poll(), 'live decoder leaked after encoder launch failure')
            self.assertTrue(decoder.stdout.closed, 'decoder pipe leaked')
        finally:
            if decoder.poll() is None:
                decoder.kill(); decoder.wait()
            decoder.stdout.close()


if __name__ == '__main__':
    unittest.main()
