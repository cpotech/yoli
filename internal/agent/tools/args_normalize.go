package tools

import (
	"encoding/json"
	"strings"
	"unicode"
)

// NormalizeArgKeys rewrites top-level camelCase JSON object keys to
// snake_case so tool schemas defined with snake_case (yoli's canonical
// form — old_string, replace_all, with_hashes, output_mode, head_limit,
// from_line, from_hash, to_line, to_hash, new_text, …) also accept the
// camelCase variant that many models — especially those imitating
// Claude-CLI examples — emit (oldString, replaceAll, withHashes, …).
//
// Without this normalisation, Go's json.Unmarshal silently drops the
// unknown key, the destination field stays at its zero value, and the
// tool returns a misleading "missing edit parameters" error that the
// model is unlikely to recover from.
//
// Behaviour:
//   - Top-level keys only. Nested objects (e.g. WebSearch's `web.results`)
//     are tool-specific and outside the scope of this normaliser.
//   - If both the snake_case and camelCase forms are present in the same
//     payload, the snake_case form wins (it is the canonical schema name).
//   - If the input is not a JSON object — or it decodes cleanly but
//     contains no uppercase letters in any top-level key — the input is
//     returned unchanged so downstream unmarshal still produces the
//     original error verbatim. This keeps the fast path allocation-free.
//   - Marshalling failure is treated the same way (return raw); a
//     downstream Unmarshal will then produce the original error.
func NormalizeArgKeys(raw json.RawMessage) json.RawMessage {
	var m map[string]json.RawMessage
	if err := json.Unmarshal(raw, &m); err != nil {
		return raw
	}
	needsRewrite := false
	for k := range m {
		if hasUpper(k) {
			needsRewrite = true
			break
		}
	}
	if !needsRewrite {
		return raw
	}
	out := make(map[string]json.RawMessage, len(m))
	// Pass 1: keep snake_case (no-uppercase) keys verbatim.
	for k, v := range m {
		if !hasUpper(k) {
			out[k] = v
		}
	}
	// Pass 2: insert camelCase translations only if the snake_case form
	// isn't already present (snake_case wins on collision).
	for k, v := range m {
		if !hasUpper(k) {
			continue
		}
		snake := camelToSnake(k)
		if _, exists := out[snake]; exists {
			continue
		}
		out[snake] = v
	}
	encoded, err := json.Marshal(out)
	if err != nil {
		return raw
	}
	return json.RawMessage(encoded)
}

func hasUpper(s string) bool {
	for _, r := range s {
		if unicode.IsUpper(r) {
			return true
		}
	}
	return false
}

// camelToSnake converts a camelCase identifier to snake_case.
// Underscores in the input are preserved as-is to keep already-snake
// keys (and mixed inputs like `foo_BarBaz`) deterministic.
func camelToSnake(s string) string {
	var b strings.Builder
	b.Grow(len(s) + 4)
	for i, r := range s {
		if i > 0 && unicode.IsUpper(r) {
			// Only insert a separating '_' if the previous rune wasn't
			// already one — prevents `Foo_Bar` becoming `foo__bar`.
			prev := s[i-1]
			if prev != '_' {
				b.WriteByte('_')
			}
		}
		b.WriteRune(unicode.ToLower(r))
	}
	return b.String()
}
