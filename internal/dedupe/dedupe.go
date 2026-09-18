// Package dedupe replaces byte-identical attachments with symlinks to one original.
// The same photo forwarded to twenty chats is downloaded twenty times; only one copy
// needs to exist on disk, and the Markdown keeps working because every path still resolves.
package dedupe

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
)

// Report says what a run found and did (or, on a dry run, would have done).
type Report struct {
	Scanned int   // regular, non-empty files looked at
	Groups  int   // sets of identical files with more than one member
	Linked  int   // duplicates replaced by links
	Bytes   int64 // disk space those duplicates took
}

type file struct {
	rel  string // slash-separated, relative to root
	size int64
}

// Run walks root, groups files by size and then by SHA-256, and for each group keeps the
// member with the earliest dateOf(rel) (ties by path) as the original. Symlinks and empty
// files are ignored, so a run is idempotent. Links are relative, so the archive can move.
func Run(root string, dateOf func(rel string) string, dryRun bool) (Report, error) {
	var rep Report
	bySize := map[int64][]file{}
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.Type().IsRegular() { // directories, symlinks (already deduplicated)
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		if info.Size() == 0 { // a stub left by an interrupted download: nothing to share
			return nil
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return err
		}
		rep.Scanned++
		f := file{rel: filepath.ToSlash(rel), size: info.Size()}
		bySize[f.size] = append(bySize[f.size], f)
		return nil
	})
	if err != nil {
		return rep, err
	}

	for _, files := range bySize {
		if len(files) < 2 { // a unique size cannot have a twin; skip the hashing
			continue
		}
		byHash := map[string][]file{}
		for _, f := range files {
			h, err := hashFile(filepath.Join(root, filepath.FromSlash(f.rel)))
			if err != nil {
				return rep, err
			}
			byHash[h] = append(byHash[h], f)
		}
		for _, group := range byHash {
			if len(group) < 2 {
				continue
			}
			rep.Groups++
			sort.Slice(group, func(i, j int) bool {
				di, dj := dateOf(group[i].rel), dateOf(group[j].rel)
				if di != dj {
					if di == "" || dj == "" { // unknown dates sort last
						return dj == ""
					}
					return di < dj
				}
				return group[i].rel < group[j].rel
			})
			original := group[0]
			for _, dup := range group[1:] {
				rep.Linked++
				rep.Bytes += dup.size
				if dryRun {
					continue
				}
				if err := link(root, dup.rel, original.rel); err != nil {
					return rep, err
				}
			}
		}
	}
	return rep, nil
}

// link replaces root/dup with a relative symlink to root/original, atomically enough
// that a crash leaves either the old file or the new link, never nothing.
func link(root, dup, original string) error {
	dupAbs := filepath.Join(root, filepath.FromSlash(dup))
	target, err := filepath.Rel(filepath.Dir(dupAbs), filepath.Join(root, filepath.FromSlash(original)))
	if err != nil {
		return err
	}
	tmp := dupAbs + ".dedupe"
	os.Remove(tmp)
	if err := os.Symlink(target, tmp); err != nil {
		return err
	}
	return os.Rename(tmp, dupAbs)
}

func hashFile(p string) (string, error) {
	f, err := os.Open(p)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
