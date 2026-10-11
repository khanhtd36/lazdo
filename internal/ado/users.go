package ado

import (
	"context"
	"slices"
	"sort"
	"strings"
	"sync"
)

// ProjectUser is a person with access to a project through its groups.
type ProjectUser struct {
	Name, Email, Descriptor string
	// Groups are the project groups the user is directly in.
	Groups []Group
	// Via are the groups outside the project, such as an Azure AD group,
	// that bring the user into a project group.
	Via []string
}

// maxNesting is how deep groups inside project groups are followed.
const maxNesting = 3

// ProjectUsers lists the people in the project's groups: direct members,
// and members of the groups nested in them.
func (c *Client) ProjectUsers(ctx context.Context, groups []Group) ([]ProjectUser, error) {
	var mu sync.Mutex
	cache := map[string][]Member{}
	members := func(desc string) ([]Member, error) {
		mu.Lock()
		ms, ok := cache[desc]
		mu.Unlock()
		if ok {
			return ms, nil
		}
		ms, err := c.GroupMembers(ctx, desc)
		if err == nil {
			mu.Lock()
			cache[desc] = ms
			mu.Unlock()
		}
		return ms, err
	}
	// Each project group's direct members, read in parallel.
	var wg sync.WaitGroup
	errs := make([]error, len(groups))
	for i, g := range groups {
		wg.Go(func() { _, errs[i] = members(g.Descriptor) })
	}
	wg.Wait()
	for _, err := range errs {
		if err != nil {
			return nil, err
		}
	}
	return projectUsers(groups, members)
}

// projectUsers gathers users from the groups' memberships; members lists a
// group's direct members by descriptor.
func projectUsers(groups []Group, members func(desc string) ([]Member, error)) ([]ProjectUser, error) {
	inProject := map[string]bool{}
	for _, g := range groups {
		inProject[g.Descriptor] = true
	}
	users := map[string]*ProjectUser{}
	user := func(m Member) *ProjectUser {
		u := users[m.Descriptor]
		if u == nil {
			u = &ProjectUser{Name: m.Name, Email: m.Detail, Descriptor: m.Descriptor}
			users[m.Descriptor] = u
		}
		return u
	}
	// via follows a group outside the project, crediting its users to it.
	var via func(desc, name string, depth int, seen map[string]bool) error
	via = func(desc, name string, depth int, seen map[string]bool) error {
		if depth > maxNesting || seen[desc] {
			return nil
		}
		seen[desc] = true
		ms, err := members(desc)
		if err != nil {
			return err
		}
		for _, m := range ms {
			switch {
			case m.IsGroup && !inProject[m.Descriptor]:
				if err := via(m.Descriptor, name, depth+1, seen); err != nil {
					return err
				}
			case !m.IsGroup:
				if u := user(m); !slices.Contains(u.Via, name) {
					u.Via = append(u.Via, name)
				}
			}
		}
		return nil
	}
	for _, g := range groups {
		ms, err := members(g.Descriptor)
		if err != nil {
			return nil, err
		}
		for _, m := range ms {
			switch {
			case !m.IsGroup:
				u := user(m)
				u.Groups = append(u.Groups, g)
			case !inProject[m.Descriptor]:
				if err := via(m.Descriptor, m.Name, 1, map[string]bool{}); err != nil {
					return nil, err
				}
			}
		}
	}
	out := make([]ProjectUser, 0, len(users))
	for _, u := range users {
		sort.Slice(u.Groups, func(i, j int) bool { return strings.ToLower(u.Groups[i].Name) < strings.ToLower(u.Groups[j].Name) })
		sort.Strings(u.Via)
		out = append(out, *u)
	}
	sort.Slice(out, func(i, j int) bool { return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name) })
	return out, nil
}
