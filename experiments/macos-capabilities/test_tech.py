import json
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch
import tech


class EvidenceBoundary(unittest.TestCase):
    def test_live_run_permission_denial_still_requires_process_identity(self):
        with tempfile.TemporaryDirectory() as d:
            runs=Path(d)/'.runs';run='run-20261024T100000-54d04a87';p=runs/run;p.mkdir(parents=True)
            (p/'launch.json').write_text(json.dumps(dict(children=[dict(name='FixtureA',pid=123)])))
            (p/'manifest.json').write_text(json.dumps(dict(token='owned-token')))
            class Process:
                returncode=0
                stdout='/System/Library/UnrelatedAgent'
            with patch.object(tech,'RUNS',runs),patch.object(tech.os,'kill',side_effect=PermissionError) as signal,patch.object(tech.subprocess,'run',return_value=Process()):
                self.assertEqual(tech.live_runs(),[])
                signal.assert_called_once_with(123,0)
            Process.stdout=str(tech.bundle('FixtureA')/'Contents/MacOS/FixtureA')+' --run '+run+' --token owned-token'
            with patch.object(tech,'RUNS',runs),patch.object(tech.os,'kill',side_effect=PermissionError),patch.object(tech.subprocess,'run',return_value=Process()):
                self.assertEqual(tech.live_runs(),[run])
            Process.returncode=1;Process.stdout=''
            with patch.object(tech,'RUNS',runs),patch.object(tech.os,'kill',side_effect=PermissionError),patch.object(tech.subprocess,'run',return_value=Process()):
                with self.assertRaisesRegex(RuntimeError,'归属'):
                    tech.live_runs()
    def test_live_run_rejects_reused_pid_but_retains_matching_fixture(self):
        with tempfile.TemporaryDirectory() as d:
            runs=Path(d)/'.runs';run='run-20261023T092338-54d04a87';p=runs/run;p.mkdir(parents=True)
            (p/'launch.json').write_text(json.dumps(dict(children=[dict(name='FixtureA',pid=123)])))
            (p/'manifest.json').write_text(json.dumps(dict(token='owned-token')))
            class Process:
                returncode=0
                stdout='/System/Library/UnrelatedAgent'
            with patch.object(tech,'RUNS',runs),patch.object(tech.os,'kill'),patch.object(tech.subprocess,'run',return_value=Process()):
                self.assertEqual(tech.live_runs(),[])
            Process.stdout=str(tech.bundle('FixtureA')/'Contents/MacOS/FixtureA')+' --run '+run+' --token owned-token'
            with patch.object(tech,'RUNS',runs),patch.object(tech.os,'kill'),patch.object(tech.subprocess,'run',return_value=Process()):
                self.assertEqual(tech.live_runs(),[run])
    def test_local_shared_module_does_not_allow_replacing_the_framework(self):
        pin=dict(Path="github.com/egoist/mygo",Version="v0.6.2")
        self.assertTrue(tech.framework_pin_matches(pin))
        self.assertFalse(tech.framework_pin_matches(dict(pin,Replace=dict(Dir="/local/mygo"))))
        self.assertFalse(tech.framework_pin_matches(dict(pin,Version="v0.6.3")))
        self.assertFalse(tech.framework_pin_matches(dict(pin,Path="focus.local/zhuanzhuli")))
    def test_restoration_cannot_pass_when_one_owned_state_is_wrong(self):
        result = dict(originallyHiddenPreserved=True, repeatCleanupReleased=True, externalChangeAbandoned=True, externalVisiblePreserved=True, allowancePreservedOnResume=True, pendingAtEnd=0)
        self.assertTrue(tech.valid_restoration_observations(result))
        for key in ("originallyHiddenPreserved", "repeatCleanupReleased", "externalChangeAbandoned", "externalVisiblePreserved", "allowancePreservedOnResume"):
            self.assertFalse(tech.valid_restoration_observations(dict(result, **{key:False})))
        self.assertFalse(tech.valid_restoration_observations(dict(result,pendingAtEnd=1)))
        self.assertFalse(tech.valid_restoration_observations(dict(result,error="observation timeout")))
        self.assertFalse(tech.valid_restoration_observations(None))

    def test_panel_creation_is_not_visibility_proof(self):
        window = dict(visible=True, onActiveSpace=True, occlusionState=8194)
        row = dict(eventSource="NSWorkspace-real", hostActiveBefore=False, before=window, after=window)
        self.assertTrue(tech.valid_choice_observations([row] * 3))
        for changed in (dict(window, visible=False), dict(window, onActiveSpace=False), dict(window, occlusionState=8192)):
            self.assertFalse(tech.valid_choice_observations([dict(row, before=changed)] * 3))
        self.assertFalse(tech.valid_choice_observations([row]))
        self.assertFalse(tech.valid_choice_observations([dict(row, hostActiveBefore=True)] * 3))

    def test_run_id_and_symlink_cannot_escape(self):
        with tempfile.TemporaryDirectory() as directory, patch.object(tech, "RUNS", Path(directory)):
            for value in ("../x", "", "/tmp/run", "run-20261022T120000-aabbccdd/../x"):
                with self.assertRaises(ValueError):
                    tech.run_dir(value)
            name = "run-20261022T120000-aabbccdd"
            path = Path(directory) / name
            path.mkdir()
            # macOS aliases /var to /private/var. A trusted parent alias is
            # valid; a run directory that itself redirects elsewhere is not.
            self.assertEqual(tech.run_dir(name), path.resolve())
            path.rmdir()
            path.symlink_to(Path(directory))
            with self.assertRaises(ValueError):
                tech.run_dir(name)

    def test_latest_rejects_symlink_and_invalid_payload(self):
        with tempfile.TemporaryDirectory() as directory, patch.object(tech, "RUNS", Path(directory)):
            pointer = Path(directory) / "latest.json"
            pointer.write_text(json.dumps(dict(runID="../outside")))
            with self.assertRaises(ValueError):
                tech.run_dir("latest")
            pointer.unlink()
            pointer.symlink_to("not-a-run")
            with self.assertRaises(ValueError):
                tech.run_dir("latest")

    def test_atomic_evidence_roundtrip_and_mode(self):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "evidence.json"
            value = {"schemaVersion":1, "result":"pending", "text":"合成文字"}
            tech.write(path, value)
            self.assertEqual(tech.read(path), value)
            self.assertEqual(path.stat().st_mode & 0o777, 0o600)

    def test_launchservices_handshake_checks_app_identity_not_launcher_pid(self):
        state = dict(schemaVersion=1, ref=dict(token="token", pid=123, launched=100.5,
            bundleID="local.focusprobe.fixture.a", path=str(tech.bundle("FixtureA").resolve())))
        self.assertTrue(tech.handshake_matches(state, "token", "FixtureA"))
        for key, value in [("token","other"),("pid",0),("launched",0),("bundleID","wrong"),("path","/tmp/other.app")]:
            changed = dict(state, ref=dict(state["ref"], **{key:value}))
            self.assertFalse(tech.handshake_matches(changed, "token", "FixtureA"))

    def test_checker_rejects_invented_verdict_instead_of_treating_it_as_complete(self):
        with tempfile.TemporaryDirectory() as directory:
            p=Path(directory)
            tech.write(p/'manifest.json',dict(schemaVersion=1,runID='test'))
            matrix=tech.initial_matrix();matrix['channels'][0]['control']='definitely-protected'
            tech.write(p/'matrix.json',matrix)
            tech.write(p/'observations.json',dict(schemaVersion=1,items={}))
            tech.write(p/'probe-status.json',dict(schemaVersion=1,result='pending'))
            tech.write(p/'journal.json',dict(schemaVersion=1,entries=[]))
            tech.write(p/'automated.json',dict(cleanup=True))
            (p/'events.jsonl').write_text('')
            with self.assertRaises(ValueError):
                tech.check(p)

    def test_failed_manual_observation_is_failure_even_with_other_items_pending(self):
        with tempfile.TemporaryDirectory() as directory:
            p=Path(directory)
            tech.write(p/'manifest.json',dict(schemaVersion=1,runID='test'))
            tech.write(p/'matrix.json',tech.initial_matrix())
            items={name:dict(result='pending',evidenceKind='manual') for name in tech.MANUAL_ITEMS}
            items['outside-choice']=dict(result='failed',evidenceKind='manual',source='synthetic checker input')
            tech.write(p/'observations.json',dict(schemaVersion=1,items=items))
            tech.write(p/'probe-status.json',dict(schemaVersion=1,result='pending'))
            tech.write(p/'journal.json',dict(schemaVersion=1,entries=[]))
            tech.write(p/'automated.json',dict(cleanup=True,hideB=True,backgroundProgress=True,webReady=True))
            (p/'events.jsonl').write_text('')
            self.assertEqual(tech.check(p),1)


if __name__ == "__main__":
    unittest.main()
