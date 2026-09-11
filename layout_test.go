package dqex

import (
	"os"
	"path/filepath"
	"testing"
)

func writeTree(t *testing.T, root string, files map[string]string) {
	t.Helper()
	for rel, content := range files {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func readTree(t *testing.T, root string) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return err
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		out[filepath.ToSlash(rel)] = string(b)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func TestRelayoutDir(t *testing.T) {
	src := t.TempDir()
	writeTree(t, src, map[string]string{
		"camunda.sql":          "-- camunda",
		"rpa_cs.sql":           "-- rpa_cs",
		"data_entity.sql":      "-- data_entity",
		"flow/camunda.sql":     "-- flow camunda",
		"flow/data_entity.sql": "-- flow data_entity",
		"panel/bi_panel.sql":   "-- panel",
		"manifest.json":        "{}",
	})
	dst := t.TempDir()
	rules := []LayoutRule{
		{Match: "camunda.sql", Dest: "1_coe/camunda.sql"},
		{Match: "rpa_cs.sql", Dest: "1_coe/rpa_cs.sql"},
		{Match: "data_entity.sql", Dest: "4_datatable/data_entity.sql"},
		{Match: "flow/*", Dest: "5_flow/*"},
		{Match: "panel/*", Dest: "6_panel/*"},
	}
	if err := RelayoutDirWithMap(src, dst, rules, true, nil); err != nil {
		t.Fatal(err)
	}
	got := readTree(t, dst)
	want := map[string]string{
		"1_coe/camunda.sql":           "-- camunda",
		"1_coe/rpa_cs.sql":            "-- rpa_cs",
		"4_datatable/data_entity.sql": "-- data_entity",
		"5_flow/camunda.sql":          "-- flow camunda",
		"5_flow/data_entity.sql":      "-- flow data_entity",
		"6_panel/bi_panel.sql":        "-- panel",
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("file %q = %q, want %q", k, got[k], v)
		}
	}
	if len(got) != len(want) {
		t.Errorf("unexpected extra files: %v", got)
	}
	// manifest.json 应被 dropUnmatched 丢弃
	if _, ok := got["manifest.json"]; ok {
		t.Error("manifest.json should be dropped")
	}
}

func TestRelayoutDirKeepUnmatched(t *testing.T) {
	src := t.TempDir()
	writeTree(t, src, map[string]string{
		"a.sql":        "A",
		"meta/info.db": "M",
	})
	dst := t.TempDir()
	rules := []LayoutRule{{Match: "a.sql", Dest: "sub/a.sql"}}
	if err := RelayoutDir(src, dst, rules, false); err != nil {
		t.Fatal(err)
	}
	got := readTree(t, dst)
	if got["sub/a.sql"] != "A" || got["meta/info.db"] != "M" {
		t.Errorf("unexpected tree: %v", got)
	}
}

func TestRelayoutDirInPlace(t *testing.T) {
	src := t.TempDir()
	writeTree(t, src, map[string]string{
		"flow/x.sql": "X",
		"root.sql":   "R",
		"junk.db":    "J",
	})
	rules := []LayoutRule{
		{Match: "flow/*", Dest: "5_flow/*"},
	}
	if err := RelayoutDir(src, src, rules, true); err != nil {
		t.Fatal(err)
	}
	got := readTree(t, src)
	want := map[string]string{
		"5_flow/x.sql": "X", // 未命中文件（root.sql/junk.db）+ dropUnmatched=true 均被丢弃
	}
	if len(got) != len(want) {
		t.Errorf("tree = %v, want %v", got, want)
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("file %q = %q, want %q", k, got[k], v)
		}
	}
}

// dstDir 位于 srcDir 内部：应报错而非把产物再次复制进自身
func TestRelayoutDirDstInsideSrc(t *testing.T) {
	src := t.TempDir()
	writeTree(t, src, map[string]string{"a.sql": "A"})
	if err := RelayoutDir(src, filepath.Join(src, "out"), nil, true); err == nil {
		t.Fatal("expected error for dstDir inside srcDir, got nil")
	}
}

func TestMatchGlob(t *testing.T) {
	cases := []struct {
		pattern, name string
		wantCaps      []string
		wantOK        bool
	}{
		{"camunda.sql", "camunda.sql", nil, true},
		{"camunda.sql", "other.sql", nil, false},
		{"flow/*", "flow/x.sql", []string{"x.sql"}, true},
		{"flow/*", "flow/sub/x.sql", nil, false}, // 单级
		{"flow/*", "flowx.sql", nil, false},
		{"*.sql", "a.sql", []string{"a"}, true},
		{"1_coe/*.sql", "1_coe/camunda.sql", []string{"camunda"}, true},
	}
	for _, c := range cases {
		caps, ok := matchGlob(c.pattern, c.name)
		if ok != c.wantOK {
			t.Errorf("matchGlob(%q, %q) ok = %v, want %v", c.pattern, c.name, ok, c.wantOK)
			continue
		}
		if ok && len(caps) != len(c.wantCaps) {
			t.Errorf("matchGlob(%q, %q) caps = %v, want %v", c.pattern, c.name, caps, c.wantCaps)
		}
	}
}
