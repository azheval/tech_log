package discovery

import (
	"fmt"
	"io/fs"
	"path"
	"path/filepath"
	"sort"
	"strings"
)

func Files(root, pattern string) ([]string, error) {
	matches := make([]string, 0, 128)
	normalizedPattern := filepath.ToSlash(pattern)

	err := filepath.WalkDir(root, func(filePath string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil
		}

		rel, err := filepath.Rel(root, filePath)
		if err != nil {
			return err
		}

		normalizedRel := filepath.ToSlash(rel)

		ok, err := path.Match(normalizedPattern, normalizedRel)
		if err != nil {
			return fmt.Errorf("invalid glob %q: %w", pattern, err)
		}
		if ok || strings.EqualFold(path.Base(normalizedRel), strings.TrimPrefix(normalizedPattern, "*/")) {
			matches = append(matches, filePath)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	sort.Strings(matches)
	return matches, nil
}
