package facts

import "strings"

// Names shows the IDs of one version the way ShortName does, except where two of them would show
// the same: each of those shows the shortest trailing path of its ID that no other one shares.
// src/routes/shops/[id].load and src/routes/orders/[id].load show as shops/[id].load and
// orders/[id].load instead of [id].load twice. The longer form still names the symbol in
// configuration, which matches a target against the trailing path of an ID.
type Names map[string]string

// Names collects the colliding IDs among the version's symbols, units and test call targets.
func (a *Analysis) Names() Names {
	ids := map[string]struct{}{}
	for _, s := range a.Symbols {
		ids[s.ID] = struct{}{}
	}
	for _, f := range a.Files {
		ids[f.Unit] = struct{}{} // a file of no unit adds "", which no other ID shows as
	}
	for _, t := range a.Tests {
		for _, c := range t.Calls {
			ids[c.Target] = struct{}{}
		}
	}
	groups := map[string][]string{}
	for id := range ids {
		short := ShortName(id)
		groups[short] = append(groups[short], id)
	}
	out := Names{}
	for _, group := range groups {
		if len(group) < 2 {
			continue
		}
		for _, id := range group {
			out[id] = distinct(id, group)
		}
	}
	return out
}

// Short is how a message shows an ID. A nil Names shows every ID by ShortName.
func (n Names) Short(id string) string {
	if s, ok := n[id]; ok {
		return s
	}
	return ShortName(id)
}

// distinct returns the shortest trailing path of id, of two path elements or more, that no other
// ID of the group ends with; the whole ID when none is shorter.
func distinct(id string, group []string) string {
	elems := strings.Split(id, "/")
	for k := 2; k < len(elems); k++ {
		suffix := strings.Join(elems[len(elems)-k:], "/")
		shared := false
		for _, other := range group {
			if other != id && (other == suffix || strings.HasSuffix(other, "/"+suffix)) {
				shared = true
				break
			}
		}
		if !shared {
			return suffix
		}
	}
	return id
}
