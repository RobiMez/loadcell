// Package tmpl is a tiny placeholder renderer for dynamic load-test inputs.
//
// A template is a string with {{token}} placeholders interleaved with
// literal text. Parse() splits it once into a list of parts; Render()
// walks the parts and emits a fresh string each call. The supported
// tokens are intentionally minimal — enough to defeat caches and give
// each request a unique identity without becoming a programming
// language.
//
// Tokens:
//
//	{{uuid}}              random UUID v4
//	{{seq}}               monotonic counter, increments once per Render
//	{{nowMs}}             time.Now().UnixMilli()
//	{{randInt:min:max}}   random int64 in [min, max] inclusive
//
// Parsing is intentionally lenient: an unknown token like "{{uid}}"
// or an unterminated "{{" is kept verbatim as literal text rather
// than rejected. The UI highlights *recognized* tokens with a
// distinct color, so the user can see at a glance whether their
// syntax is being picked up — a typo just looks like plain text.
package tmpl

import (
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"math/rand/v2"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
)

type partKind int

const (
	partLiteral partKind = iota
	partUUID
	partSeq
	partNowMs
	partRandInt
)

type part struct {
	kind partKind
	text string // literal content (kind == partLiteral)
	rMin int64  // randInt lower bound, inclusive
	rMax int64  // randInt upper bound, inclusive
}

// Template is a parsed placeholder string. Render is safe to call
// concurrently from multiple goroutines.
type Template struct {
	parts  []part
	hasSeq bool
	seq    atomic.Int64
}

// Parse splits s into literal + token parts. It returns an error if
// any token is unrecognized or malformed (e.g. randInt missing
// min:max).
func Parse(s string) (*Template, error) {
	t := &Template{}
	i := 0
	for i < len(s) {
		open := strings.Index(s[i:], "{{")
		if open < 0 {
			t.parts = append(t.parts, part{kind: partLiteral, text: s[i:]})
			break
		}
		if open > 0 {
			t.parts = append(t.parts, part{kind: partLiteral, text: s[i : i+open]})
		}
		i += open
		close := strings.Index(s[i+2:], "}}")
		if close < 0 {
			// Unterminated "{{" — keep the rest as literal text. The
			// UI doesn't highlight it as a token, so the user sees
			// plain text and knows their syntax isn't being picked up.
			t.parts = append(t.parts, part{kind: partLiteral, text: s[i:]})
			break
		}
		token := s[i+2 : i+2+close]
		p, err := parseToken(token)
		if err != nil {
			// Unknown / malformed token — keep the full "{{...}}" as
			// literal text. Matches the highlighter: no recognition →
			// no special styling → it reads as the plain text it is.
			t.parts = append(t.parts, part{kind: partLiteral, text: s[i : i+2+close+2]})
			i += 2 + close + 2
			continue
		}
		if p.kind == partSeq {
			t.hasSeq = true
		}
		t.parts = append(t.parts, p)
		i += 2 + close + 2
	}
	return t, nil
}

func parseToken(tok string) (part, error) {
	tok = strings.TrimSpace(tok)
	switch tok {
	case "uuid":
		return part{kind: partUUID}, nil
	case "seq":
		return part{kind: partSeq}, nil
	case "nowMs":
		return part{kind: partNowMs}, nil
	}
	if rest, ok := strings.CutPrefix(tok, "randInt:"); ok {
		sep := strings.Index(rest, ":")
		if sep < 0 {
			return part{}, errors.New("randInt needs min:max, e.g. {{randInt:1:100}}")
		}
		min, err := strconv.ParseInt(strings.TrimSpace(rest[:sep]), 10, 64)
		if err != nil {
			return part{}, fmt.Errorf("randInt min: %w", err)
		}
		max, err := strconv.ParseInt(strings.TrimSpace(rest[sep+1:]), 10, 64)
		if err != nil {
			return part{}, fmt.Errorf("randInt max: %w", err)
		}
		if min > max {
			return part{}, errors.New("randInt min must be <= max")
		}
		return part{kind: partRandInt, rMin: min, rMax: max}, nil
	}
	return part{}, errors.New("unknown token")
}

// SeqValue returns the current value of the {{seq}} counter without
// incrementing it. Use with SetSeq to carry the count across templates
// (e.g. when the user edits a sample request and we re-parse).
func (t *Template) SeqValue() int64 { return t.seq.Load() }

// SetSeq overwrites the {{seq}} counter. The next Render call sees
// v+1 (Add(1) on the underlying atomic).
func (t *Template) SetSeq(v int64) { t.seq.Store(v) }

// IsStatic reports whether the template contains no dynamic tokens
// (so Render returns the same string every time).
func (t *Template) IsStatic() bool {
	for _, p := range t.parts {
		if p.kind != partLiteral {
			return false
		}
	}
	return true
}

// Render produces a freshly substituted string. Each call increments
// {{seq}} by one and snapshots {{nowMs}} once, so multiple occurrences
// of those tokens in the same template share the same value within a
// single render.
func (t *Template) Render() string {
	if len(t.parts) == 0 {
		return ""
	}
	if len(t.parts) == 1 && t.parts[0].kind == partLiteral {
		return t.parts[0].text
	}
	var seqVal int64
	if t.hasSeq {
		seqVal = t.seq.Add(1)
	}
	var nowVal int64
	nowCached := false

	var b strings.Builder
	for _, p := range t.parts {
		switch p.kind {
		case partLiteral:
			b.WriteString(p.text)
		case partUUID:
			b.WriteString(uuidV4())
		case partSeq:
			b.WriteString(strconv.FormatInt(seqVal, 10))
		case partNowMs:
			if !nowCached {
				nowVal = time.Now().UnixMilli()
				nowCached = true
			}
			b.WriteString(strconv.FormatInt(nowVal, 10))
		case partRandInt:
			span := p.rMax - p.rMin + 1
			b.WriteString(strconv.FormatInt(rand.Int64N(span)+p.rMin, 10))
		}
	}
	return b.String()
}

// uuidV4 builds a random UUID v4 using math/rand/v2, which is
// goroutine-safe and lock-free — important because workers call this
// at high RPS. Not crypto-grade randomness, but cache-busting doesn't
// need it to be.
func uuidV4() string {
	var b [16]byte
	binary.LittleEndian.PutUint64(b[0:8], rand.Uint64())
	binary.LittleEndian.PutUint64(b[8:16], rand.Uint64())
	b[6] = (b[6] & 0x0f) | 0x40 // version 4
	b[8] = (b[8] & 0x3f) | 0x80 // variant 1 (RFC 4122)
	var s [36]byte
	hex.Encode(s[0:8], b[0:4])
	s[8] = '-'
	hex.Encode(s[9:13], b[4:6])
	s[13] = '-'
	hex.Encode(s[14:18], b[6:8])
	s[18] = '-'
	hex.Encode(s[19:23], b[8:10])
	s[23] = '-'
	hex.Encode(s[24:36], b[10:16])
	return string(s[:])
}

