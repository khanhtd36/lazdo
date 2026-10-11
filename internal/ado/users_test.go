package ado

import (
	"fmt"
	"testing"
)

func TestProjectUsers(t *testing.T) {
	contributors := Group{Descriptor: "g-contrib", Name: "Contributors"}
	readers := Group{Descriptor: "g-readers", Name: "Readers"}
	valid := Group{Descriptor: "g-valid", Name: "Project Valid Users"}
	sam := Member{Name: "Sam Rivera", Detail: "sam@contoso.com", Descriptor: "u-sam"}
	jo := Member{Name: "Jo Tanaka", Detail: "jo@contoso.com", Descriptor: "u-jo"}
	priya := Member{Name: "Priya Shah", Detail: "priya@contoso.com", Descriptor: "u-priya"}
	devs := Member{Name: "Developers", IsGroup: true, Descriptor: "aad-devs"}
	inner := Member{Name: "Interns", IsGroup: true, Descriptor: "aad-interns"}
	memberships := map[string][]Member{
		"g-contrib":   {sam, devs},
		"g-readers":   {sam, jo},
		"g-valid":     {{Name: "Contributors", IsGroup: true, Descriptor: "g-contrib"}, {Name: "Readers", IsGroup: true, Descriptor: "g-readers"}},
		"aad-devs":    {jo, inner},
		"aad-interns": {priya},
	}
	users, err := projectUsers([]Group{contributors, readers, valid}, func(desc string) ([]Member, error) {
		ms, ok := memberships[desc]
		if !ok {
			return nil, fmt.Errorf("unknown group %s", desc)
		}
		return ms, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, u := range users {
		got[u.Name] = fmt.Sprintf("%v via %v", len(u.Groups), u.Via)
		if u.Name == "Sam Rivera" && (u.Groups[0].Name != "Contributors" || u.Groups[1].Name != "Readers") {
			t.Errorf("Sam's groups = %v", u.Groups)
		}
	}
	want := map[string]string{
		"Jo Tanaka":  "1 via [Developers]", // in Readers, and in Contributors through Developers
		"Priya Shah": "0 via [Developers]", // two levels down
		"Sam Rivera": "2 via []",
	}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("users = %v, want %v", got, want)
	}
	if users[0].Name != "Jo Tanaka" {
		t.Fatalf("users should sort by name: %v", users[0].Name)
	}
}
