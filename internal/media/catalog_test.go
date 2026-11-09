package media

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestManifestRejectsIncompleteTamperedAndEscapingRoles(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "media"), 0700); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256([]byte("real fixture bytes"))
	f := File{Path: "media/forest", SHA256: hex.EncodeToString(sum[:])}
	if err := os.WriteFile(filepath.Join(root, f.Path), []byte("real fixture bytes"), 0600); err != nil {
		t.Fatal(err)
	}
	valid := Catalog{Version: 1, Guide: f, Scenes: []Scene{{ID: "forest", Name: "森林", Preview: f, Background: f, Entry: f, Work: f}}}
	write := func(c Catalog) {
		b, e := json.Marshal(c)
		if e != nil {
			t.Fatal(e)
		}
		if e = os.WriteFile(filepath.Join(root, "manifest.json"), b, 0600); e != nil {
			t.Fatal(e)
		}
	}
	write(valid)
	if _, err := Load(root); err != nil {
		t.Fatal(err)
	}
	for _, role := range []File{{Path: "../outside", SHA256: f.SHA256}, {Path: "/tmp/outside", SHA256: f.SHA256}, {Path: "media/../outside", SHA256: f.SHA256}, {Path: "media/forest", SHA256: "bad"}, {Path: "media/missing", SHA256: f.SHA256}, {}} {
		c := valid
		c.Guide = role
		write(c)
		if _, err := Load(root); err == nil {
			t.Fatalf("accepted invalid role %+v", role)
		}
	}
	write(valid)
	if err := os.WriteFile(filepath.Join(root, f.Path), []byte("modified"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(root); err == nil {
		t.Fatal("accepted modified bytes")
	}
}

func TestBadEntryDoesNotMakeOtherRolesOrRecoveryMetadataInaccessible(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "media"), 0700); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256([]byte("valid background"))
	good := File{Path: "media/background", SHA256: hex.EncodeToString(sum[:])}
	bad := File{Path: "media/missing-entry", SHA256: good.SHA256}
	if err := os.WriteFile(filepath.Join(root, good.Path), []byte("valid background"), 0600); err != nil {
		t.Fatal(err)
	}
	c := Catalog{Version: 1, Guide: good, Scenes: []Scene{{ID: "forest", Name: "森林", Preview: good, Background: good, Entry: bad, Work: good}}}
	body, _ := json.Marshal(c)
	if err := os.WriteFile(filepath.Join(root, "manifest.json"), body, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(root); err == nil {
		t.Fatal("complete self-check accepted missing entry")
	}
	available, err := Read(root)
	if err != nil {
		t.Fatal("role metadata must remain readable for recovery/skip", err)
	}
	scene, ok := available.Scene("forest")
	if !ok {
		t.Fatal("lost formal scene")
	}
	if _, err = Resolve(root, scene.Background); err != nil {
		t.Fatal("unrelated role unavailable", err)
	}
	if _, err = Resolve(root, scene.Entry); err == nil {
		t.Fatal("bad entry was silently substituted")
	}
}

func TestRejectedManifestNeverLeaksAUsablePartialCatalog(t *testing.T) {
	root := t.TempDir()
	for _, body := range []string{
		`{"version":1,"scenes":[]}`,
		`{"version":1,"scenes":[{"id":"pond","name":"森林"}]}`,
		`{"version":1,"scenes":[{"id":"invented","name":"不存在"}]}`,
		`{"version":2,"scenes":[{"id":"forest","name":"森林"}]}`,
		`{"version":1,"scenes":[{"id":"forest","name":"森林"},{"id":"forest","name":"重复"}]}`,
		`{"version":1,"scenes":[{"id":"forest","name":"森林"}],"guide":{"path":123}}`,
	} {
		if err := os.WriteFile(filepath.Join(root, "manifest.json"), []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
		catalog, err := Read(root)
		if err == nil || len(catalog.Scenes) != 0 {
			t.Fatalf("rejected manifest left a startable catalog: %+v %v", catalog, err)
		}
	}
}

func TestKnownIdentityDoesNotRequireInstalledAssetsOrRenameLegacySnow(t *testing.T) {
	for _, item := range descriptions {
		if item.ID == "" || item.Name == "" || !KnownScene(item.ID) {
			t.Fatal(item)
		}
	}
	if SceneName("snow") != "雪地" || SceneName("snow-mountain") != "雪山" || SceneName("beach") != "沙滩" {
		t.Fatal("legacy identity was renamed")
	}
	if SceneName("unknown") != "unknown" || KnownScene("unknown") {
		t.Fatal("unknown scene silently substituted")
	}
	if _, ok := (Catalog{}).Scene("forest"); ok {
		t.Fatal("known identity treated as installed assets")
	}
}

func TestAutomaticAmbienceMatchesScene(t *testing.T) {
	for scene, want := range map[string]string{"forest": "birds", "garden": "birds", "meadow": "birds", "mountain": "stream", "snow-mountain": "stream", "pond": "stream"} {
		got, ok := AutomaticAmbience(scene)
		if !ok || got != want {
			t.Fatal(scene, got, ok)
		}
	}
	for _, scene := range []string{"unknown", "snow", "beach"} {
		if _, ok := AutomaticAmbience(scene); ok {
			t.Fatal("unknown/legacy silently mapped", scene)
		}
	}
}

func TestAmbienceResourcesRejectMissingAndCorruptFiles(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "media"), 0700); err != nil {
		t.Fatal(err)
	}
	f := File{Path: "media/rain.m4a", SHA256: strings.Repeat("0", 64)}
	if _, err := Resolve(root, f); err == nil {
		t.Fatal("missing audio accepted")
	}
	os.WriteFile(filepath.Join(root, f.Path), []byte("corrupt"), 0600)
	if _, err := Resolve(root, f); err == nil {
		t.Fatal("altered audio accepted")
	}
}
