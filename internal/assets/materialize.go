package assets

import (
	"embed"
	"errors"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// embeddedFS is the single source-of-truth tree of default assets, mirrored
// onto disk by materializeAssets.
//
//go:embed embed
var embeddedFS embed.FS

// materializeAssets mirrors the embedded default tree into dir. A file that
// already exists on disk is never touched — only missing files are written,
// so user edits and user-authored skills always survive.
func materializeAssets(dir string) error {
	return fs.WalkDir(embeddedFS, "embed", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel := strings.TrimPrefix(strings.TrimPrefix(p, "embed"), "/")
		if rel == "" {
			return nil
		}
		dst := filepath.Join(dir, filepath.FromSlash(rel))
		if d.IsDir() {
			return os.MkdirAll(dst, 0o755)
		}
		if _, statErr := os.Stat(dst); statErr == nil {
			return nil // already on disk — never overwrite
		} else if !errors.Is(statErr, fs.ErrNotExist) {
			return statErr
		}
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return err
		}
		body, err := embeddedFS.ReadFile(p)
		if err != nil {
			return err
		}
		return os.WriteFile(dst, body, 0o644)
	})
}

// embeddedBytes returns the embedded default for an asset path relative to the
// embed root (e.g. "system.md", "skills/design-binder.md").
func embeddedBytes(rel string) ([]byte, bool) {
	b, err := embeddedFS.ReadFile(path.Join("embed", rel))
	if err != nil {
		return nil, false
	}
	return b, true
}
