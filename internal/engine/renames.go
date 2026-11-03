package engine

import (
	"crypto/sha256"
	"io/fs"
)

// detectRenames finds files that moved with unchanged content: a path that exists only in base
// whose content hash equals a path that exists only in head. Renames the source already knows
// take precedence.
func detectRenames(base, head fs.FS, known map[string]string) (map[string]string, error) {
	baseFiles, err := hashes(base)
	if err != nil {
		return nil, err
	}
	headFiles, err := hashes(head)
	if err != nil {
		return nil, err
	}
	out := map[string]string{}
	taken := map[string]bool{}
	for from, to := range known {
		out[from], taken[to] = to, true
	}
	byHash := map[[32]byte][]string{}
	for p, h := range headFiles {
		if _, inBase := baseFiles[p]; !inBase && !taken[p] {
			byHash[h] = append(byHash[h], p)
		}
	}
	for p, h := range baseFiles {
		if _, inHead := headFiles[p]; inHead || out[p] != "" {
			continue
		}
		// Only an unambiguous match is a rename; two identical new files could be either.
		if c := byHash[h]; len(c) == 1 {
			out[p] = c[0]
			delete(byHash, h)
		}
	}
	return out, nil
}

func hashes(fsys fs.FS) (map[string][32]byte, error) {
	out := map[string][32]byte{}
	err := fs.WalkDir(fsys, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		b, err := fs.ReadFile(fsys, p)
		if err != nil {
			return err
		}
		out[p] = sha256.Sum256(b)
		return nil
	})
	return out, err
}
