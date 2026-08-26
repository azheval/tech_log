package discovery

import (
	"context"
	"fmt"
	"io/fs"
	"path"
	"path/filepath"
	"sort"
	"strings"
)

func Files(root, pattern string) ([]string, error) {
	return FilesContext(context.Background(), root, pattern)
}

// FilesContext returns matching files in deterministic order and stops walking
// as soon as ctx is cancelled.
func FilesContext(ctx context.Context, root, pattern string) ([]string, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	matches := make([]string, 0, 128)
	normalizedPattern := filepath.ToSlash(pattern)

	err := filepath.WalkDir(root, func(filePath string, d fs.DirEntry, walkErr error) error {
		if err := ctx.Err(); err != nil {
			return err
		}
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
