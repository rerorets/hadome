// SPDX-License-Identifier: MIT
// Copyright (c) 2026 rerorets

package facts

import (
	"net/netip"
	"regexp"
	"strconv"
	"strings"
)

// LiteralKind is the shape of a value that belongs in configuration (docs/design/rules-code.md).
type LiteralKind string

// Literal kinds. The string shapes (URL, IP address, host and port) are judged by ConfigString in
// every language; the others need to know the language's calls, so each adapter judges them.
const (
	LiteralURL      LiteralKind = "url"
	LiteralIP       LiteralKind = "ip"
	LiteralHostPort LiteralKind = "host-port"
	LiteralPort     LiteralKind = "port"
	LiteralDuration LiteralKind = "duration"
	// LiteralNamed is a literal passed to an argument or field whose name the configuration lists
	// (rules.code.config-literal.names).
	LiteralNamed LiteralKind = "named"
)

// Literal is one value written in a function's body that belongs in configuration.
type Literal struct {
	Kind LiteralKind
	Line int
	// Evidence is the source text of the value, on one line, for the message.
	Evidence string
}

// Swallow is one place in a function's body where an error is caught or returned and dropped.
type Swallow struct {
	Line int
	// Evidence is the source text of the place, on one line, for the message.
	Evidence string
}

// urlSchemes are the schemes of URLs that name somewhere to connect to. Schemes that name no host
// (file:, data:, mailto:) are not among them.
var urlSchemes = map[string]bool{
	"http": true, "https": true, "ws": true, "wss": true, "grpc": true, "grpcs": true,
	"redis": true, "rediss": true, "postgres": true, "postgresql": true, "mysql": true, "mariadb": true,
	"mongodb": true, "mongodb+srv": true, "amqp": true, "amqps": true, "kafka": true, "nats": true,
	"mqtt": true, "mqtts": true, "tcp": true, "udp": true, "smtp": true, "smtps": true, "ftp": true,
	"sftp": true, "ldap": true, "ldaps": true, "s3": true, "gs": true, "memcached": true,
}

// namespaceHosts are hosts whose URLs are identifiers (XML namespaces, schema IDs), never endpoints.
var namespaceHosts = []string{
	"w3.org", "json-schema.org", "purl.org", "schemas.xmlsoap.org", "schemas.openxmlformats.org",
	"schemas.microsoft.com", "xmlns.com", "ns.adobe.com", "opengis.net", "xmlns.jcp.org",
	"java.sun.com", "schemas.android.com", "springframework.org", "maven.apache.org", "xml.apache.org",
	"openxmlformats.org", "docbook.org", "relaxng.org", "xml.org", "oasis-open.org", "openid.net",
	"schema.org", "ogp.me",
}

var (
	urlForm  = regexp.MustCompile(`^([a-z][a-z0-9+.-]*)://([^/?#\s]+)([/?#][^\s]*)?$`)
	hostPort = regexp.MustCompile(`^(localhost|[A-Za-z0-9-]+(\.[A-Za-z0-9-]+)+|\[[0-9A-Fa-f:.]+\])?:([0-9]{2,5})$`)
	hostName = regexp.MustCompile(`^(localhost|[A-Za-z0-9]([A-Za-z0-9-]*[A-Za-z0-9])?(\.[A-Za-z0-9]([A-Za-z0-9-]*[A-Za-z0-9])?)*)$`)
)

// ConfigString returns the shape of a string literal's whole content when it is a value that belongs
// in configuration: a URL with a scheme and a host, an IP address, or a host and port. It returns
// "" for anything else, including a string with such a value in the middle of a sentence, and a URL
// whose host is filled in at run time.
func ConfigString(s string) LiteralKind {
	if s == "" || strings.ContainsAny(s, " \t\n") || !ascii(s) {
		return "" // text around a value, such as an example in a placeholder
	}
	if m := urlForm.FindStringSubmatch(s); m != nil {
		return urlKind(m[1], m[2])
	}
	if m := hostPort.FindStringSubmatch(s); m != nil {
		return hostPortKind(m[1], m[3])
	}
	ip, bits, cidr := strings.Cut(s, "/")
	if cidr {
		if _, err := netip.ParsePrefix(s); err != nil || bits == "" {
			return ""
		}
	}
	if isIP(ip) && !wellKnown(ip) {
		return LiteralIP
	}
	return ""
}

// urlKind is the shape of a URL with the given scheme and authority (user@host:port).
func urlKind(scheme, authority string) LiteralKind {
	if !urlSchemes[strings.ToLower(scheme)] {
		return ""
	}
	host := authority
	if at := strings.LastIndex(host, "@"); at >= 0 { // user:password@host
		host = host[at+1:]
	}
	port := ""
	if bracketed, ok := strings.CutPrefix(host, "["); ok {
		addr, rest, _ := strings.Cut(bracketed, "]")
		if _, err := netip.ParseAddr(addr); err != nil {
			return "" // http://[${host}]:8080
		}
		host, port = addr, strings.TrimPrefix(rest, ":")
	} else {
		host, port, _ = strings.Cut(host, ":")
		if !hostName.MatchString(host) {
			return "" // a host filled in at run time: https://%s/, http://{host}/
		}
	}
	if _, err := strconv.Atoi(port); port != "" && err != nil {
		return ""
	}
	if namespace(strings.ToLower(host)) || example(host) || wellKnown(host) && port == "" {
		return ""
	}
	return LiteralURL
}

// hostPortKind is the shape of host:port, host possibly empty.
func hostPortKind(host, port string) LiteralKind {
	if p, err := strconv.Atoi(port); err != nil || p == 0 || p > 65535 {
		return ""
	}
	if inner, ok := strings.CutPrefix(host, "["); ok {
		if _, err := netip.ParseAddr(strings.TrimSuffix(inner, "]")); err != nil {
			return ""
		}
	} else if host != "" && host != "localhost" && !hasLetter(host) && !isIP(host) {
		return "" // digits and dots that are no address, such as a version
	}
	if example(host) {
		return ""
	}
	return LiteralHostPort
}

// wellKnown reports an address that means the same on every machine: the loopback, every address,
// the broadcast. Code compares with them or binds to them; with no port they are no configuration.
// "http://localhost" alone is the base a relative URL is parsed against.
func wellKnown(host string) bool {
	switch strings.ToLower(host) {
	case "localhost", "127.0.0.1", "0.0.0.0", "::1", "::", "255.255.255.255":
		return true
	}
	return false
}

// example reports a host reserved for documentation and tests (RFC 2606, RFC 6761), which no
// installation connects to: example.com, api.example.org, svc.test.
func example(host string) bool {
	host = strings.ToLower(host)
	for _, h := range []string{"example.com", "example.org", "example.net", "example", "test", "invalid"} {
		if host == h || strings.HasSuffix(host, "."+h) {
			return true
		}
	}
	return false
}

func ascii(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] >= 0x80 {
			return false
		}
	}
	return true
}

// comparing are calls that compare text with a value or take it apart, by NameKey. A URL given to
// one is something to recognise, not somewhere to connect to: strings.HasPrefix(u,
// "https://github.com/").
var comparing = NameKeys([]string{"startsWith", "endsWith", "HasPrefix", "HasSuffix", "TrimPrefix", "TrimSuffix",
	"CutPrefix", "CutSuffix", "strip_prefix", "strip_suffix", "removeprefix", "removesuffix", "removePrefix", "removeSuffix",
	"trim_start_matches", "trim_end_matches", "trim_matches", "includes", "contains", "indexOf", "lastIndexOf", "find",
	"rfind", "match", "matches", "search", "split", "equals", "equalsIgnoreCase"})

// Compares reports a callee, by its last name, that compares text with its argument.
func Compares(callee string) bool { return comparing[NameKey(callee)] }

// dateMath are calls that move a time by a duration or compare two, by NameKey. A duration given to
// one is part of a calculation, not a setting: now.Add(-24 * time.Hour), date.minus(Duration.ofDays(7)).
var dateMath = NameKeys([]string{"Add", "Sub", "plus", "minus", "Truncate", "Round", "isAfter", "isBefore", "compareTo"})

// MovesTime reports a callee, by its last name, that takes a duration to calculate a time.
func MovesTime(callee string) bool { return dateMath[NameKey(callee)] }

// Comparison reports an operator that compares its operands.
func Comparison(op string) bool {
	switch op {
	case "==", "!=", "===", "!==", "<", ">", "<=", ">=":
		return true
	}
	return false
}

// namespace reports a host of namespaceHosts, or a host under one of them.
func namespace(host string) bool {
	for _, h := range namespaceHosts {
		if host == h || strings.HasSuffix(host, "."+h) {
			return true
		}
	}
	return false
}

// isIP reports an IPv4 address in dotted form or an IPv6 address with at least one hex digit, so
// that "::" (a separator in many languages' strings) is not one.
func isIP(s string) bool {
	a, err := netip.ParseAddr(s)
	if err != nil {
		return false
	}
	if a.Is4() {
		return strings.Count(s, ".") == 3
	}
	return strings.ContainsAny(s, "0123456789abcdefABCDEF")
}

func hasLetter(s string) bool {
	return strings.IndexFunc(s, func(r rune) bool { return r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z' }) >= 0
}

// NameKey is how an argument or field name is compared with the configured names and with the
// built-in names of ports and timeouts: lower case, without _ and -, so that max_retries,
// maxRetries and MaxRetries are the same name.
func NameKey(name string) string {
	return strings.ToLower(strings.NewReplacer("_", "", "-", "").Replace(name))
}

// NamedLiteral returns the kind of a literal passed to an argument, keyword argument or field of
// this name: a port, a timeout, or a name the configuration lists. It returns "" for other names.
// number is true when the literal is a number; ports and timeouts are only ever numbers. zero is
// true when the number is 0, which counts as none of them.
func NamedLiteral(name string, number, zero bool, names map[string]bool) LiteralKind {
	key := NameKey(name)
	switch {
	case number && zero: // no timeout, any port: a choice, not a value that differs by environment
		return ""
	case number && key == "port":
		return LiteralPort
	case number && (strings.HasSuffix(key, "timeout") || key == "ttl" || strings.HasSuffix(key, "timeoutms") ||
		strings.HasSuffix(key, "timeoutseconds")):
		return LiteralDuration
	case names[key]:
		return LiteralNamed
	}
	return ""
}

// NameKeys returns the configured names as NameKey keys.
func NameKeys(names []string) map[string]bool {
	if len(names) == 0 {
		return nil
	}
	m := map[string]bool{}
	for _, n := range names {
		m[NameKey(n)] = true
	}
	return m
}

// Evidence writes source text on one line for a message: whitespace runs become one space, and
// text over 80 characters is cut with "…".
func Evidence(text string) string {
	text = strings.Join(strings.Fields(text), " ")
	if r := []rune(text); len(r) > 80 {
		return string(r[:80]) + "…"
	}
	return text
}

// Zero reports a number literal whose value is 0, in any language's spelling: 0, 0.0, 0L, 0u, 0f,
// 0x0, 0o0, 0b0, 0_0, 0e10.
func Zero(literal string) bool {
	s := strings.ToLower(strings.ReplaceAll(literal, "_", ""))
	for _, prefix := range []string{"0x", "0o", "0b"} {
		if digits, ok := strings.CutPrefix(s, prefix); ok {
			digits = strings.TrimRight(digits, "lu") // a suffix after hex digits is only l or u
			return digits != "" && strings.Trim(digits, "0") == ""
		}
	}
	s = strings.TrimRight(s, "lufd")
	if mantissa, _, ok := strings.Cut(s, "e"); ok {
		s = mantissa
	}
	return s != "" && strings.Trim(s, "0.") == "" && strings.Contains(s, "0")
}
