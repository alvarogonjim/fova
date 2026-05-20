package tools

import (
	"fmt"
	"path/filepath"
	"strings"
)

// ResolveWorkspacePath resolves p against the project workspace root.
//
//   - If p is empty, returns "" with no error (caller decides whether the
//     field is required).
//   - If p is absolute, it must lie inside root or the call fails with the
//     same `escapes the workspace` wording as SafeJoin.
//   - Otherwise, p is workspace-relative and joined onto root via SafeJoin,
//     which rejects traversal attempts (`../etc/passwd`).
//
// This mirrors the path-handling convention the fs.* tools use, so the
// design tools and fs.read share the same workspace-rooted view.
func ResolveWorkspacePath(root, p string) (string, error) {
	if p == "" {
		return "", nil
	}
	if filepath.IsAbs(p) {
		cleanRoot := filepath.Clean(root)
		cleanP := filepath.Clean(p)
		if cleanP == cleanRoot || strings.HasPrefix(cleanP, cleanRoot+string(filepath.Separator)) {
			return cleanP, nil
		}
		return "", fmt.Errorf("path %q escapes the workspace", p)
	}
	return SafeJoin(root, p)
}
