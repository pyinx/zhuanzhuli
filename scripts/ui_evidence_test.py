import copy, unittest
from verify import validate_ui_state

class UIEvidenceStateTests(unittest.TestCase):
    def setUp(self):
        self.ready=dict(mainPage=0,session={"phase":"idle"},historyCount=0,
            appList={},mainModal=False,notificationDraftDirty=False,
            navigation={"attachedSheet":False},windows={"main":{"visible":True}})

    def test_actual_idle_is_ready_but_missing_phase_is_rejected(self):
        validate_ui_state("ready",self.ready)
        data=copy.deepcopy(self.ready);data["session"]={}
        with self.assertRaises(RuntimeError):validate_ui_state("ready",data)

    def test_relabeling_ready_as_other_states_is_rejected(self):
        for state in ("working","paused","picker","empty-apps","empty-history","notification-draft"):
            with self.subTest(state=state), self.assertRaises(RuntimeError):
                validate_ui_state(state,self.ready)

    def test_matching_page_is_insufficient_without_actual_data(self):
        for label,page in (("apps",0),("history",2),("notification-draft",3)):
            data=copy.deepcopy(self.ready);data["mainPage"]=page
            with self.subTest(label=label), self.assertRaises(RuntimeError):validate_ui_state(label,data)

    def test_real_phase_and_modal_signals_are_required(self):
        active=copy.deepcopy(self.ready);active["session"]["phase"]="active"
        validate_ui_state("working",active)
        active["session"]["phase"]="paused";validate_ui_state("paused",active)
        picker=copy.deepcopy(self.ready);picker["mainModal"]=True
        picker["windows"]["appPicker"]={"visible":True}
        with self.assertRaises(RuntimeError):validate_ui_state("picker",picker)
        picker["navigation"]["attachedSheet"]=True;validate_ui_state("picker",picker)

    def test_empty_and_draft_states_have_explicit_evidence(self):
        data=copy.deepcopy(self.ready);data["mainPage"]=0;data["appList"]={"count":0};validate_ui_state("empty-apps",data)
        data["mainPage"]=2;validate_ui_state("empty-history",data)
        data["mainPage"]=3;data["notificationDraftDirty"]=True;validate_ui_state("notification-draft",data)

if __name__=="__main__":unittest.main()
