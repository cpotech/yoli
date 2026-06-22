package providers

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"iter"

	"yoli/internal/ai"
)

// WireToolCallFn mirrors the `function` field of an SSE tool-call delta.
type WireToolCallFn struct {
	Name      string `json:"name,omitempty"`
	Arguments string `json:"arguments,omitempty"`
}

// WireToolCallDelta is one tool_calls entry inside an SSE delta payload.
// `id`, `type`, and `function` may all be partially populated.
type WireToolCallDelta struct {
	Index    int             `json:"index"`
	ID       string          `json:"id,omitempty"`
	Type     string          `json:"type,omitempty"`
	Function *WireToolCallFn `json:"function,omitempty"`
}

// ToolCallAccumulator carries the merged state for a single tool call as
// deltas stream in.
type ToolCallAccumulator struct {
	ID        string
	Name      string
	Arguments string
}

// MergeToolCallDeltas folds one wire tool-call delta into the accumulator
// keyed by index and returns the corresponding ChatStreamChunk for the
// caller to forward downstream.
//
// The id/name are sticky: the first non-empty value wins for the
// accumulator, but the returned chunk always reports the freshest known
// id/name (the new delta's value if present, otherwise the accumulated
// one).
func MergeToolCallDeltas(acc map[int]*ToolCallAccumulator, d WireToolCallDelta) ai.ChatStreamChunk {
	var argsDelta, name string
	if d.Function != nil {
		argsDelta = d.Function.Arguments
		name = d.Function.Name
	}
	entry, exists := acc[d.Index]
	if !exists {
		acc[d.Index] = &ToolCallAccumulator{
			ID:        d.ID,
			Name:      name,
			Arguments: argsDelta,
		}
		return ai.ChatStreamChunk{
			Type:           ai.ChunkToolCall,
			Index:          d.Index,
			ID:             d.ID,
			Name:           name,
			ArgumentsDelta: argsDelta,
		}
	}
	if d.ID != "" && entry.ID == "" {
		entry.ID = d.ID
	}
	if name != "" && entry.Name == "" {
		entry.Name = name
	}
	entry.Arguments += argsDelta

	chunkID := d.ID
	if chunkID == "" {
		chunkID = entry.ID
	}
	chunkName := name
	if chunkName == "" {
		chunkName = entry.Name
	}
	return ai.ChatStreamChunk{
		Type:           ai.ChunkToolCall,
		Index:          d.Index,
		ID:             chunkID,
		Name:           chunkName,
		ArgumentsDelta: argsDelta,
	}
}

// IterSSE yields each `data:` JSON payload from body in order, terminating
// on the `data: [DONE]` sentinel or EOF. `:`-prefixed comment lines and
// blank lines are skipped. Frames that span multiple `data:` lines are
// joined with `\n` per the SSE spec.
//
// The yielded RawMessage is a copy and is safe to retain across iterations.
// A non-nil error is yielded once if the underlying reader fails.
func IterSSE(r io.Reader) iter.Seq2[json.RawMessage, error] {
	return func(yield func(json.RawMessage, error) bool) {
		scanner := bufio.NewScanner(r)
		scanner.Buffer(make([]byte, 4096), 4*1024*1024)
		scanner.Split(splitSSEFrames)
		for scanner.Scan() {
			data := extractSSEData(scanner.Bytes())
			if data == nil {
				continue
			}
			if bytes.Equal(data, []byte("[DONE]")) {
				return
			}
			payload := make([]byte, len(data))
			copy(payload, data)
			if !yield(json.RawMessage(payload), nil) {
				return
			}
		}
		if err := scanner.Err(); err != nil {
			yield(nil, err)
		}
	}
}

// splitSSEFrames is a bufio.SplitFunc that splits on `\n\n` or `\r\n\r\n`,
// matching the SSE event boundary. A partial frame at EOF is discarded.
func splitSSEFrames(data []byte, atEOF bool) (advance int, token []byte, err error) {
	if atEOF && len(data) == 0 {
		return 0, nil, nil
	}
	lf := bytes.Index(data, []byte("\n\n"))
	crlf := bytes.Index(data, []byte("\r\n\r\n"))
	if lf == -1 && crlf == -1 {
		if atEOF {
			// Drop incomplete tail at EOF; matches the TS implementation.
			return len(data), nil, nil
		}
		return 0, nil, nil
	}
	var idx, length int
	switch {
	case lf == -1:
		idx, length = crlf, 4
	case crlf == -1:
		idx, length = lf, 2
	case lf < crlf:
		idx, length = lf, 2
	default:
		idx, length = crlf, 4
	}
	return idx + length, data[:idx], nil
}

// extractSSEData reduces one frame to its joined `data:` payload, or nil
// if the frame contains no `data:` lines (e.g. a comment-only keepalive).
func extractSSEData(frame []byte) []byte {
	var parts [][]byte
	for _, line := range bytes.Split(frame, []byte("\n")) {
		line = bytes.TrimRight(line, "\r")
		if len(line) == 0 || line[0] == ':' {
			continue
		}
		if !bytes.HasPrefix(line, []byte("data:")) {
			continue
		}
		value := line[len("data:"):]
		if len(value) > 0 && value[0] == ' ' {
			value = value[1:]
		}
		parts = append(parts, value)
	}
	if len(parts) == 0 {
		return nil
	}
	return bytes.Join(parts, []byte("\n"))
}
