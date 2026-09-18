package dedupe

import (
	"os"
	"path/filepath"
	"testing"
)

func write(t *testing.T, root, rel, content string) {
	t.Helper()
	p := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestDedupeKeepsEarliestAndLinksTheRest(t *testing.T) {
	root := t.TempDir()
	write(t, root, "anna-1/10.jpg", "same-bytes")
	write(t, root, "bob-2/20.jpg", "same-bytes")
	write(t, root, "carl-3/30.jpg", "same-bytes")
	write(t, root, "anna-1/11.jpg", "other-bytes") // unique, same size as above: must survive
	write(t, root, "anna-1/12.jpg", "")            // empty: never touched
	write(t, root, "bob-2/21.jpg", "")
	dates := map[string]string{
		"anna-1/10.jpg": "2026-03-01", "bob-2/20.jpg": "2025-01-01", "carl-3/30.jpg": "2026-06-01",
	}
	rep, err := Run(root, func(rel string) string { return dates[rel] }, false)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Groups != 1 || rep.Linked != 2 || rep.Bytes != int64(2*len("same-bytes")) {
		t.Fatalf("report = %+v, want 1 group, 2 linked, %d bytes", rep, 2*len("same-bytes"))
	}
	// bob's is the earliest message, so it stays a real file
	if fi, _ := os.Lstat(filepath.Join(root, "bob-2/20.jpg")); fi.Mode()&os.ModeSymlink != 0 {
		t.Error("canonical file was replaced by a link")
	}
	for _, rel := range []string{"anna-1/10.jpg", "carl-3/30.jpg"} {
		p := filepath.Join(root, rel)
		fi, _ := os.Lstat(p)
		if fi.Mode()&os.ModeSymlink == 0 {
			t.Errorf("%s is not a symlink", rel)
			continue
		}
		target, _ := os.Readlink(p)
		if filepath.IsAbs(target) {
			t.Errorf("%s links to an absolute path %q", rel, target)
		}
		b, err := os.ReadFile(p)
		if err != nil || string(b) != "same-bytes" {
			t.Errorf("%s does not resolve to the content: %v", rel, err)
		}
	}
	if fi, _ := os.Lstat(filepath.Join(root, "anna-1/11.jpg")); fi.Mode()&os.ModeSymlink != 0 {
		t.Error("a unique file of equal size was linked")
	}
	if fi, _ := os.Lstat(filepath.Join(root, "anna-1/12.jpg")); fi.Mode()&os.ModeSymlink != 0 {
		t.Error("an empty file was linked")
	}
	// second run finds nothing left to do
	again, err := Run(root, func(string) string { return "" }, false)
	if err != nil || again.Linked != 0 {
		t.Errorf("second run linked %d files, want 0 (%v)", again.Linked, err)
	}
}

func TestDedupeDryRunChangesNothing(t *testing.T) {
	root := t.TempDir()
	write(t, root, "a/1.jpg", "same-bytes")
	write(t, root, "b/2.jpg", "same-bytes")
	rep, err := Run(root, func(string) string { return "" }, true)
	if err != nil || rep.Linked != 1 {
		t.Fatalf("dry run report = %+v (%v), want 1 would-be link", rep, err)
	}
	for _, rel := range []string{"a/1.jpg", "b/2.jpg"} {
		if fi, _ := os.Lstat(filepath.Join(root, rel)); fi.Mode()&os.ModeSymlink != 0 {
			t.Errorf("dry run replaced %s", rel)
		}
	}
}
