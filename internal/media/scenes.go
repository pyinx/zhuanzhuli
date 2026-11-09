package media

import (
	_ "embed"
	"encoding/json"
)

// Scene identities are independent of installed asset availability. Both the
// offline authoring scripts and the app read this same descriptor source.
//
//go:embed scenes.json
var sceneDescriptions []byte

type SceneDescription struct {
	ID                string  `json:"id"`
	Name              string  `json:"name"`
	Sound             string  `json:"sound"`
	Legacy            bool    `json:"legacy"`
	Ambience          string  `json:"ambience,omitempty"`
	LegacyNatureRatio float64 `json:"legacyNatureRatio,omitempty"`
}

var descriptions = func() []SceneDescription {
	var items []SceneDescription
	if err := json.Unmarshal(sceneDescriptions, &items); err != nil {
		panic(err)
	}
	return items
}()

func DescribeScene(id string) (SceneDescription, bool) {
	for _, item := range descriptions {
		if item.ID == id {
			return item, true
		}
	}
	return SceneDescription{}, false
}

func KnownScene(id string) bool { _, ok := DescribeScene(id); return ok }

func AmbienceName(id string) string {
	switch id {
	case "rain":
		return "下雨"
	case "birds":
		return "鸟叫"
	case "waves":
		return "海浪"
	case "stream":
		return "流水"
	}
	return ""
}

func AutomaticAmbience(scene string) (string, bool) {
	d, ok := DescribeScene(scene)
	return d.Ambience, ok && !d.Legacy && AmbienceName(d.Ambience) != ""
}

func SceneName(id string) string {
	if item, ok := DescribeScene(id); ok {
		return item.Name
	}
	return id
}
