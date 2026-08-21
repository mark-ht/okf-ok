package lint

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

func discover(root string) (Bundle, error) {
	rootInfo, err := os.Lstat(root)
	if err != nil {
		return Bundle{}, err
	}
	if rootInfo.Mode()&os.ModeSymlink != 0 {
		return Bundle{}, fmt.Errorf("bundle root must not be a symlink: %s", root)
	}
	info, err := os.Stat(root)
	if err != nil {
		return Bundle{}, err
	}
	if !info.IsDir() {
		return Bundle{}, fmt.Errorf("bundle root is not a directory: %s", root)
	}
	absolute, err := filepath.Abs(root)
	if err != nil {
		return Bundle{}, err
	}
	b := Bundle{Root: absolute, Files: map[string]struct{}{}, Dirs: map[string]struct{}{"": {}}}
	err = filepath.WalkDir(absolute, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if path == absolute {
			return nil
		}
		if entry.Type()&fs.ModeSymlink != 0 {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		rel, err := filepath.Rel(absolute, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if entry.IsDir() {
			b.Dirs[rel] = struct{}{}
			return nil
		}
		if entry.Type().IsRegular() {
			b.Files[rel] = struct{}{}
		}
		return nil
	})
	if err != nil {
		return Bundle{}, err
	}
	return b, nil
}

func isMarkdown(path string) bool { return strings.EqualFold(filepath.Ext(path), ".md") }
func isReserved(path string) bool {
	base := strings.ToLower(filepath.Base(path))
	return base == "index.md" || base == "log.md"
}
