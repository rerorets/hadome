package reuse

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"hash"
	"math"
	"reflect"
	"slices"
)

// Hash is a SHA-256 hash: of a file's content, of a stage's key, or of a result.
type Hash [32]byte

// plain reports whether values of t are made of values only: no pointer, interface, function,
// channel or unsafe pointer anywhere inside. Only such values can be keys, and only such results
// have a fingerprint of their own.
func plain(t reflect.Type) bool { return plainSeen(t, map[reflect.Type]bool{}) }

func plainSeen(t reflect.Type, seen map[reflect.Type]bool) bool {
	if seen[t] {
		return true
	}
	seen[t] = true
	switch t.Kind() {
	case reflect.Bool, reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr,
		reflect.Float32, reflect.Float64, reflect.String:
		return true
	case reflect.Slice, reflect.Array:
		return plainSeen(t.Elem(), seen)
	case reflect.Map:
		return plainSeen(t.Key(), seen) && plainSeen(t.Elem(), seen)
	case reflect.Struct:
		for i := range t.NumField() {
			if !plainSeen(t.Field(i).Type, seen) {
				return false
			}
		}
		return true
	}
	return false
}

// encoder writes a value's canonical form into a hash. Values of one type are written so that two
// different ones never write the same bytes: strings and lists with their length first, byte slices
// and maps as a hash of fixed size. Byte slices' hashes are also kept in contents.
type encoder struct {
	h        hash.Hash
	contents []Hash
}

func newEncoder() *encoder { return &encoder{h: sha256.New()} }

func (e *encoder) sum() Hash {
	var out Hash
	copy(out[:], e.h.Sum(nil))
	return out
}

func (e *encoder) uint(n uint64) {
	var b [8]byte
	binary.BigEndian.PutUint64(b[:], n)
	e.h.Write(b[:])
}

func (e *encoder) str(s string) {
	e.uint(uint64(len(s)))
	e.h.Write([]byte(s))
}

func (e *encoder) value(v reflect.Value) {
	switch v.Kind() {
	case reflect.Bool:
		if v.Bool() {
			e.uint(1)
		} else {
			e.uint(0)
		}
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		e.uint(uint64(v.Int()))
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		e.uint(v.Uint())
	case reflect.Float32, reflect.Float64:
		e.uint(math.Float64bits(v.Float()))
	case reflect.String:
		e.str(v.String())
	case reflect.Slice:
		if v.Type().Elem().Kind() == reflect.Uint8 {
			c := Hash(sha256.Sum256(v.Bytes()))
			e.contents = append(e.contents, c)
			e.h.Write(c[:])
			return
		}
		e.list(v)
	case reflect.Array:
		e.list(v)
	case reflect.Map:
		// Map keys are written in the order of their own encodings, so that iterating a map in a
		// different order never changes the key.
		type pair struct{ k, v []byte }
		var pairs []pair
		for _, k := range v.MapKeys() {
			pairs = append(pairs, pair{e.sub(k), e.sub(v.MapIndex(k))})
		}
		slices.SortFunc(pairs, func(a, b pair) int { return bytes.Compare(a.k, b.k) })
		m := sha256.New()
		for _, p := range pairs {
			m.Write(p.k)
			m.Write(p.v)
		}
		e.h.Write(m.Sum(nil))
	case reflect.Struct:
		// A stage's argument always has the same type, so its fields need no names to tell them apart.
		for i := range v.NumField() {
			e.value(v.Field(i))
		}
	default:
		panic(fmt.Sprintf("reuse: %s is not made of values only", v.Type()))
	}
}

func (e *encoder) list(v reflect.Value) {
	e.uint(uint64(v.Len()))
	for i := range v.Len() {
		e.value(v.Index(i))
	}
}

// sub encodes v on its own and returns its hash, keeping the byte slices it holds in contents.
func (e *encoder) sub(v reflect.Value) []byte {
	s := newEncoder()
	s.value(v)
	e.contents = append(e.contents, s.contents...)
	h := s.sum()
	return h[:]
}

// encode returns the hash of v's canonical form and the hashes of the byte slices it holds.
func encode(v any) (Hash, []Hash) {
	e := newEncoder()
	e.value(reflect.ValueOf(v))
	return e.sum(), e.contents
}
