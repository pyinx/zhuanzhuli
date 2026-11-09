// Package media validates the one release manifest shared by packaging and playback.
package media

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type File struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
}
type Scene struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Preview    File   `json:"preview"`
	Background File   `json:"background"`
	Entry      File   `json:"entry"`
	Work       File   `json:"work"`
}
type Catalog struct {
	Version   int        `json:"version"`
	Scenes    []Scene    `json:"scenes"`
	Guide     File       `json:"guide"`
	Ambiences []Ambience `json:"ambiences,omitempty"`
}

type Ambience struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Audio File   `json:"audio"`
}

func (c Catalog) Ambience(id string) (Ambience, bool) {
	for _, v := range c.Ambiences {
		if v.ID == id {
			return v, true
		}
	}
	return Ambience{}, false
}

func (c Catalog) Files() []File {
	files := []File{c.Guide}
	for _, s := range c.Scenes {
		files = append(files, s.Preview, s.Background, s.Entry, s.Work)
	}
	for _, a := range c.Ambiences {
		files = append(files, a.Audio)
	}
	return files
}
func (c Catalog) Scene(id string) (Scene, bool) {
	for _, s := range c.Scenes {
		if s.ID == id {
			return s, true
		}
	}
	return Scene{}, false
}
func Read(root string) (c Catalog, err error) {
	defer func() {
		if err != nil {
			c = Catalog{}
		}
	}()
	canonical, canonicalErr := filepath.EvalSymlinks(root)
	if canonicalErr != nil {
		return c, canonicalErr
	}
	root = canonical
	body, err := os.ReadFile(filepath.Join(root, "manifest.json"))
	if err != nil {
		return c, err
	}
	if err = json.Unmarshal(body, &c); err != nil {
		return c, err
	}
	if (c.Version != 1 && c.Version != 2) || len(c.Scenes) == 0 {
		return c, errors.New("正式资源清单版本无效")
	}
	if c.Version == 2 {
		if len(c.Ambiences) != 4 {
			return c, errors.New("四类白噪音资源不完整")
		}
		ids := map[string]bool{}
		for _, a := range c.Ambiences {
			if AmbienceName(a.ID) == "" || a.Name != AmbienceName(a.ID) || ids[a.ID] {
				return c, errors.New("白噪音身份无效或重复")
			}
			ids[a.ID] = true
		}
	}
	seen := map[string]bool{}
	for _, s := range c.Scenes {
		if !KnownScene(s.ID) || s.Name != SceneName(s.ID) || seen[s.ID] {
			return c, errors.New("正式场景不完整或重复")
		}
		seen[s.ID] = true
	}
	for _, f := range c.Files() {
		if !validPath(f.Path) || len(f.SHA256) != 64 {
			return c, errors.New("资源路径越界")
		}
	}
	return c, nil
}
func validPath(path string) bool {
	return !filepath.IsAbs(path) && filepath.Clean(path) == path && strings.HasPrefix(path, "media/") && !strings.Contains(path, "\\")
}
func Resolve(root string, f File) (string, error) {
	if !validPath(f.Path) {
		return "", errors.New("资源路径越界")
	}
	canonical, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", err
	}
	root = canonical
	path := filepath.Join(root, f.Path)
	resolved, e := filepath.EvalSymlinks(path)
	if e != nil {
		return "", e
	}
	rel, e := filepath.Rel(root, resolved)
	if e != nil || rel != f.Path {
		return "", errors.New("资源不能通过链接替换")
	}
	info, e := os.Stat(path)
	if e != nil || !info.Mode().IsRegular() || info.Size() == 0 {
		return "", errors.New("正式资源缺失")
	}
	data, e := os.ReadFile(path)
	if e != nil {
		return "", e
	}
	sum := sha256.Sum256(data)
	if len(f.SHA256) != 64 || hex.EncodeToString(sum[:]) != f.SHA256 {
		return "", fmt.Errorf("正式资源校验失败：%s", f.Path)
	}
	return path, nil
}
func Load(root string) (Catalog, error) {
	c, err := Read(root)
	if err != nil {
		return c, err
	}
	for _, f := range c.Files() {
		if _, err = Resolve(root, f); err != nil {
			return c, err
		}
	}
	return c, nil
}
