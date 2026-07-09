package tools

import (
	"encoding/json"
	"strings"
	"unicode"

	"yoli/internal/ai"
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

// NormalizeArgKeysToSchema rewrites top-level argument keys toward the
// property names DECLARED in the tool's schema, whatever casing the
// schema uses. NormalizeArgKeys above assumes snake_case is canonical,
// which mangles tools whose schema parameters are camelCase (the
// yolium_* protocol tools: itemId, agentName, parentId, …) — a model
// that calls them exactly as documented would have its keys rewritten
// to snake_case and silently dropped by the tool's unmarshal.
//
// Behaviour:
//   - Keys already matching a declared property pass through verbatim.
//   - Unknown keys are remapped to a declared property when their
//     camel↔snake counterpart matches one (itemId ⇄ item_id).
//   - On collision the key that already matches the schema wins.
//   - Keys matching nothing are kept as-is; rejecting them is the
//     tool's job.
//   - Definitions with no object properties fall back to the legacy
//     snake_case normalisation.
func NormalizeArgKeysToSchema(raw json.RawMessage, def ai.ToolDefinition) json.RawMessage {
	props := declaredPropertyNames(def)
	if len(props) == 0 {
		return NormalizeArgKeys(raw)
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(raw, &m); err != nil {
		return raw
	}
	needsRewrite := false
	for k := range m {
		if !props[k] {
			needsRewrite = true
			break
		}
	}
	if !needsRewrite {
		return raw
	}
	out := make(map[string]json.RawMessage, len(m))
	// Pass 1: keys that already match the schema pass through verbatim.
	for k, v := range m {
		if props[k] {
			out[k] = v
		}
	}
	// Pass 2: remap unknown keys via their camel/snake counterpart, only
	// when that name is declared and not already provided.
	for k, v := range m {
		if props[k] {
			continue
		}
		target := k
		if snake := camelToSnake(k); props[snake] {
			target = snake
		} else if camel := snakeToCamel(k); props[camel] {
			target = camel
		}
		if _, exists := out[target]; exists {
			continue
		}
		out[target] = v
	}
	encoded, err := json.Marshal(out)
	if err != nil {
		return raw
	}
	return json.RawMessage(encoded)
}

// declaredPropertyNames extracts the top-level property names from a
// tool definition's JSON-schema parameters. Handles both Go-native
// (map[string]any) definitions and ones that round-tripped through JSON.
func declaredPropertyNames(def ai.ToolDefinition) map[string]bool {
	if def.Parameters == nil {
		return nil
	}
	props, ok := def.Parameters["properties"].(map[string]any)
	if !ok || len(props) == 0 {
		return nil
	}
	out := make(map[string]bool, len(props))
	for name := range props {
		out[name] = true
	}
	return out
}

// snakeToCamel converts a snake_case identifier to lowerCamelCase
// (item_id → itemId). Leading/trailing underscores are preserved-ish by
// virtue of empty segments being skipped; inputs without underscores
// are returned unchanged.
func snakeToCamel(s string) string {
	if !strings.Contains(s, "_") {
		return s
	}
	parts := strings.Split(s, "_")
	var b strings.Builder
	b.Grow(len(s))
	first := true
	for _, p := range parts {
		if p == "" {
			continue
		}
		if first {
			b.WriteString(p)
			first = false
			continue
		}
		r := []rune(p)
		b.WriteRune(unicode.ToUpper(r[0]))
		b.WriteString(string(r[1:]))
	}
	return b.String()
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
