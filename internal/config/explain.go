package config

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// SchemaID is the identifier of the published schema. It names the file in the repository; hadome
// never fetches it.
const SchemaID = "https://github.com/rerorets/hadome/blob/main/schema/hadome.schema.json"

// JSONSchema returns the JSON Schema of `.hadome.yml`, generated from Spec.
func JSONSchema() []byte {
	s := schemaOf(Spec)
	s["$schema"] = "https://json-schema.org/draft/2020-12/schema"
	s["$id"] = SchemaID
	s["title"] = ".hadome.yml"
	out, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		panic(err) // only maps, slices, strings and numbers: cannot fail
	}
	return append(out, '\n')
}

func schemaOf(k Key) map[string]any {
	s := map[string]any{}
	if k.Doc != "" {
		s["description"] = k.Doc
	}
	switch k.Kind {
	case KindObject:
		if len(k.Variants) == 0 {
			objectSchema(s, k.Fields, nil)
		} else {
			s["oneOf"] = variantSchemas(k.Variants)
		}
	case KindList:
		s["type"] = "array"
		s["items"] = schemaOf(*k.Elem)
		if k.MinItems > 0 {
			s["minItems"] = k.MinItems
		}
	case KindInt:
		intSchema(s, k)
	case KindAny:
	default:
		s["type"] = "string"
		if k.Default != nil {
			s["default"] = k.Default
		}
		if len(k.Allowed) > 0 {
			s["enum"] = names(k.Allowed)
		}
		if k.Pattern != "" {
			s["pattern"] = k.Pattern
		}
	}
	return s
}

func variantSchemas(variants []Variant) []any {
	var one []any
	for _, v := range variants {
		vs := map[string]any{"title": v.Name}
		objectSchema(vs, v.Fields, v.Discriminator)
		if len(v.ExactlyOne) > 0 {
			var alts []any
			for _, name := range v.ExactlyOne {
				alts = append(alts, map[string]any{"required": []string{name}})
			}
			vs["oneOf"] = alts
		}
		one = append(one, vs)
	}
	return one
}

func intSchema(s map[string]any, k Key) {
	s["type"] = "integer"
	if k.Min > 0 {
		s["minimum"] = k.Min
	}
	if k.Default != nil {
		s["default"] = k.Default
	}
	if len(k.Allowed) > 0 {
		var enum []int
		for _, a := range k.Allowed {
			i, _ := strconv.Atoi(a.Name)
			enum = append(enum, i)
		}
		s["enum"] = enum
	}
}

func objectSchema(s map[string]any, fields []Key, discriminator any) {
	props := map[string]any{}
	var required []string
	if d, ok := discriminator.(string); ok {
		required = append(required, d)
	}
	for _, f := range fields {
		props[f.Name] = schemaOf(f)
		if f.Required && f.Name != discriminator {
			required = append(required, f.Name)
		}
	}
	s["type"] = "object"
	s["properties"] = props
	s["additionalProperties"] = false
	if len(required) > 0 {
		s["required"] = required
	}
}

func names(vs []Value) []string {
	out := make([]string, len(vs))
	for i, v := range vs {
		out[i] = v.Name
	}
	return out
}

// Explain describes one key (by dotted path, such as `invariants.target`) or one allowed value
// (such as the invariant type `idempotent`). It reports false when the name is unknown.
func Explain(name string) (string, bool) {
	if k, ok := lookup(Spec, strings.Split(name, ".")); ok {
		return explainKey(name, k), true
	}
	for _, k := range allKeys(Spec, "") {
		for _, v := range k.key.Allowed {
			if v.Name == name {
				return fmt.Sprintf("%s — a value of `%s`\n\n%s\n", name, k.path, v.Doc), true
			}
		}
	}
	return "", false
}

func lookup(k Key, path []string) (Key, bool) {
	if len(path) == 0 || path[0] == "" {
		return Key{}, false
	}
	if k.Kind == KindList {
		k = *k.Elem
	}
	fields := k.Fields
	for _, v := range k.Variants {
		fields = append(fields, v.Fields...)
	}
	for _, f := range fields {
		if f.Name != path[0] {
			continue
		}
		if len(path) == 1 {
			return f, true
		}
		return lookup(f, path[1:])
	}
	return Key{}, false
}

type located struct {
	path string
	key  Key
}

func allKeys(k Key, path string) []located {
	if k.Kind == KindList {
		return allKeys(*k.Elem, path)
	}
	out := []located{{path, k}}
	fields := k.Fields
	for _, v := range k.Variants {
		fields = append(fields, v.Fields...)
	}
	for _, f := range fields {
		out = append(out, allKeys(f, joinPath(path, f.Name))...)
	}
	return out
}

func explainKey(name string, k Key) string {
	var b strings.Builder
	kinds := map[Kind]string{KindObject: "a mapping", KindList: "a list", KindString: "a string", KindInt: "an integer", KindAny: "any value"}
	req := "optional"
	if k.Required {
		req = "required"
	}
	fmt.Fprintf(&b, "%s — %s, %s\n\n%s\n", name, kinds[k.Kind], req, k.Doc)
	allowedFrom := k
	if k.Kind == KindList {
		allowedFrom = *k.Elem
	}
	if len(allowedFrom.Allowed) > 0 {
		b.WriteString("\nAllowed values:\n")
		for _, v := range allowedFrom.Allowed {
			fmt.Fprintf(&b, "  %-12s %s\n", v.Name, v.Doc)
		}
	}
	switch k.Looser {
	case LooserWhenRemoved:
		b.WriteString("\nRemoving an entry that the base branch has needs a person's review (exit code 3).\n")
	case LooserWhenChanged:
		b.WriteString("\nRemoving or changing what the base branch has needs a person's review (exit code 3).\n")
	case LooserWhenHigher:
		b.WriteString("\nRaising it above the base branch's value needs a person's review (exit code 3).\n")
	case LooserWhenAdded:
		b.WriteString("\nAdding an entry that the base branch does not have needs a person's review (exit code 3).\n")
	case LooserWhenLower:
		b.WriteString("\nLowering it below the base branch's value needs a person's review (exit code 3).\n")
	case LooserAlways:
		b.WriteString("\nAny change, adding or removing an entry, needs a person's review (exit code 3).\n")
	}
	if k.Example != "" {
		b.WriteString("\nExample:\n  " + strings.ReplaceAll(k.Example, "\n", "\n  ") + "\n")
	}
	return b.String()
}
