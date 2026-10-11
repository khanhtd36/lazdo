package prefs

import (
	"path/filepath"
	"testing"
)

func TestUseKeepsMostRecentFirstAndSaves(t *testing.T) {
	dir := t.TempDir()
	saved := file
	defer func() { file = saved }()
	file = func() (string, error) { return filepath.Join(dir, "config.json"), nil }

	p := Load().Use(Pair{Org: "contoso"}).Use(Pair{Org: "fabrikam", Account: "sam@contoso.com"}).Use(Pair{Org: "contoso", Tenant: "t1"})
	if err := p.Save(); err != nil {
		t.Fatal(err)
	}
	got := Load().Recent
	if len(got) != 2 || got[0] != (Pair{Org: "contoso", Tenant: "t1"}) || got[1].Org != "fabrikam" {
		t.Fatalf("recent = %+v", got)
	}
	if r, ok := Load().For("fabrikam"); !ok || r.Account != "sam@contoso.com" {
		t.Fatalf("For = %+v", r)
	}
	if got := Load().Forget("sam@contoso.com").Recent; len(got) != 1 {
		t.Fatalf("after Forget %+v", got)
	}
}
