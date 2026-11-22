// SPDX-License-Identifier: MIT
// Copyright (c) 2026 rerorets

package facts

import "testing"

// viewpoint: behaviour
// target: facts.ConfigString
// A whole string is a value that belongs in configuration when it is a URL with a scheme of somewhere
// to connect to and a host, an IP address, or a host and port. A path, a sentence with a URL in it, a
// host filled in at run time, a namespace URL, a host reserved for examples, a value inside other
// text and an address that means the same on every machine without a port are not.
func TestConfigString(t *testing.T) {
	cases := map[string]LiteralKind{
		"https://api.shop.internal/v1":     LiteralURL,
		"https://api.example.com/v1":       "",
		"http://svc.test:8080":             "",
		"example.com:443":                  "",
		"https://host.io/pull/1（例）":        "",
		"redis://cache:6379":               LiteralURL,
		"postgres://u:p@db.internal/app":   LiteralURL,
		"https://api.example.com/v1 extra": "",
		"http://localhost:3000":            LiteralURL,
		"http://[fe80::1]:8080/":           LiteralURL,
		"http://[::]:8080/":                LiteralURL,
		"[:::]:8080":                       "",
		"10.0.0.12":                        LiteralIP,
		"fe80::1":                          LiteralIP,
		"10.0.0.0/8":                       LiteralIP,
		":8080":                            LiteralHostPort,
		"db.internal:5432":                 LiteralHostPort,
		"127.0.0.1:3000":                   LiteralHostPort,
		"/v1/orders":                       "",
		"see https://docs.io/guide":        "",
		"https://%s/v1":                    "",
		"http://{host}/v1":                 "",
		"http://[${host}]:8080":            "",
		"http://www.w3.org/2000/svg":       "",
		"https://w3.org.cdn.io/api":        LiteralURL,
		"http://localhost":                 "",
		"127.0.0.1":                        "",
		"0.0.0.0":                          "",
		"::":                               "",
		"::1":                              "",
		"file:///etc/hosts":                "",
		"mailto:someone@example.com":       "",
		"10.0.0.7/not-a-prefix":            "",
		"1.2.3":                            "",
		"12:30":                            "",
		"":                                 "",
	}
	for s, want := range cases {
		if got := ConfigString(s); got != want {
			t.Errorf("ConfigString(%q) = %q, want %q", s, got, want)
		}
	}
}

// viewpoint: behaviour
// target: facts.Zero
// A number literal is zero in any language's spelling, and only then.
func TestZero(t *testing.T) {
	for s, want := range map[string]bool{
		"0": true, "0.0": true, "0L": true, "0u": true, "0f": true, "0x0": true, "0o0": true, "0b0": true,
		"0_0": true, "0e10": true, "0.0d": true,
		"1": false, "0xff": false, "0x10": false, "10": false, "0.5": false, "1e0": false, "": false, "0b1": false,
	} {
		if got := Zero(s); got != want {
			t.Errorf("Zero(%q) = %v, want %v", s, got, want)
		}
	}
}

// viewpoint: behaviour
// target: facts.NamedLiteral
// A number passed to a name for a port or a time limit counts as one, unless it is zero; a literal
// passed to a configured name counts as named, whatever its spelling of the name.
func TestNamedLiteral(t *testing.T) {
	names := NameKeys([]string{"page_size"})
	cases := []struct {
		name         string
		number, zero bool
		want         LiteralKind
	}{
		{"port", true, false, LiteralPort},
		{"Port", true, false, LiteralPort},
		{"port", true, true, ""},
		{"port", false, false, ""},
		{"timeout", true, false, LiteralDuration},
		{"readTimeout", true, false, LiteralDuration},
		{"connect_timeout", true, false, LiteralDuration},
		{"requestTimeoutMs", true, false, LiteralDuration},
		{"ttl", true, false, LiteralDuration},
		{"timeout", true, true, ""},
		{"pageSize", true, false, LiteralNamed},
		{"PAGE_SIZE", false, false, LiteralNamed},
		{"limit", true, false, ""},
	}
	for _, c := range cases {
		if got := NamedLiteral(c.name, c.number, c.zero, names); got != c.want {
			t.Errorf("NamedLiteral(%q, %v, %v) = %q, want %q", c.name, c.number, c.zero, got, c.want)
		}
	}
}
