//go:build darwin

package macos

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// Directory traversal runs off the UI thread. Bundle metadata is read on AppKit.
func InstalledAppPaths() ([]string, []string) {
	home, err := os.UserHomeDir()
	issues := []string{}
	if err != nil {
		issues = append(issues, err.Error())
	}
	roots := []string{"/Applications", "/System/Applications"}
	if home != "" {
		roots = append(roots, filepath.Join(home, "Applications"))
	}
	paths := []string{}
	for _, root := range roots {
		if _, err := os.Stat(root); os.IsNotExist(err) {
			continue
		}
		filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				issues = append(issues, path+"："+err.Error())
				if d != nil && d.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
			if strings.EqualFold(filepath.Ext(path), ".app") {
				paths = append(paths, path)
				if d.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
			if d.IsDir() && strings.HasPrefix(d.Name(), ".") {
				return filepath.SkipDir
			}
			return nil
		})
	}
	return paths, issues
}
