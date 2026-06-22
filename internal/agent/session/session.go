// Package session implements a JSONL-backed conversation store with a
// branching entry tree. Each session is a single JSONL file under a
// per-cwd bucket: the first line is a header describing the session,
// every subsequent line is either a message entry (with parentId
// pointing at its parent in the tree) or a leaf marker that moves the
// active leaf without appending a new message.
package session

import (
	"bufio"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"yoli/internal/ai"
)

// Version is the session format version written into Header.Version.
const Version = 3

// DefaultRootDir is the on-disk root for sessions when none is supplied.
// It is appended to ~/.yoli/agent/sessions.
const DefaultRootDir = ".yoli/agent/sessions"

// Header is the first line of every session file.
type Header struct {
	Type          string `json:"type"`
	Version       int    `json:"version"`
	ID            string `json:"id"`
	Timestamp     string `json:"timestamp"`
	Cwd           string `json:"cwd"`
	ParentSession string `json:"parentSession,omitempty"`
}

// Entry is a non-header line in the session file. Type is "message" for
// normal entries and "leaf" for explicit leaf moves recorded by Branch.
type Entry struct {
	Type      string      `json:"type"`
	ID        string      `json:"id"`
	ParentID  string      `json:"parentId"`
	Timestamp string      `json:"timestamp"`
	Message   *ai.Message `json:"message,omitempty"`
}

// Options configure session construction.
type Options struct {
	// RootDir is the on-disk root under which per-cwd buckets live.
	// Empty means ~/.yoli/agent/sessions.
	RootDir string
	// Cwd is the working directory the session is associated with.
	Cwd string
}

// Session is an opened or in-memory JSONL conversation. Methods are not
// safe for concurrent use.
type Session struct {
	header  Header
	file    string
	entries []Entry
	leaf    string
	// inMemory means AppendMessage and Branch update state without
	// touching the filesystem.
	inMemory bool
}

// Create initialises a new session file on disk and returns the open
// handle. The file is created with a v3 header line and no entries; the
// leaf is empty until the first AppendMessage call.
func Create(opts Options) (*Session, error) {
	root, err := resolveRoot(opts.RootDir)
	if err != nil {
		return nil, err
	}
	bucket := filepath.Join(root, cwdBucket(opts.Cwd))
	if err := os.MkdirAll(bucket, 0o755); err != nil {
		return nil, err
	}
	id, err := newUUID()
	if err != nil {
		return nil, err
	}
	header := Header{
		Type:      "session",
		Version:   Version,
		ID:        id,
		Timestamp: timestampNow(),
		Cwd:       opts.Cwd,
	}
	file := filepath.Join(bucket, id+".jsonl")
	if err := writeHeader(file, header); err != nil {
		return nil, err
	}
	return &Session{header: header, file: file}, nil
}

// Open loads a session from a JSONL file. The header is required; entries
// and the implicit leaf are reconstructed from the file order.
func Open(path string) (*Session, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	if !sc.Scan() {
		return nil, fmt.Errorf("session: empty file %s", path)
	}
	var head Header
	if err := json.Unmarshal(sc.Bytes(), &head); err != nil {
		return nil, fmt.Errorf("session: header: %w", err)
	}
	if head.Type != "session" {
		return nil, fmt.Errorf("session: header type = %q want session", head.Type)
	}
	var entries []Entry
	leaf := ""
	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}
		var e Entry
		if err := json.Unmarshal(line, &e); err != nil {
			return nil, fmt.Errorf("session: entry: %w", err)
		}
		switch e.Type {
		case "message":
			entries = append(entries, e)
			leaf = e.ID
		case "leaf":
			leaf = e.ID
		}
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	return &Session{header: head, file: path, entries: entries, leaf: leaf}, nil
}

// ContinueRecent returns the most-recently-modified session for the
// configured cwd, or creates a fresh one when no prior session exists.
func ContinueRecent(opts Options) (*Session, error) {
	sessions, err := List(opts)
	if err != nil {
		return nil, err
	}
	if len(sessions) == 0 {
		return Create(opts)
	}
	return sessions[0], nil
}

// ForkFrom resolves src and returns a new session whose active branch
// replays the source's active branch. The header records the source
// session ID in ParentSession.
func ForkFrom(opts Options, src string) (*Session, error) {
	source, err := Resolve(opts, src)
	if err != nil {
		return nil, err
	}
	root, err := resolveRoot(opts.RootDir)
	if err != nil {
		return nil, err
	}
	bucket := filepath.Join(root, cwdBucket(opts.Cwd))
	if err := os.MkdirAll(bucket, 0o755); err != nil {
		return nil, err
	}
	id, err := newUUID()
	if err != nil {
		return nil, err
	}
	header := Header{
		Type:          "session",
		Version:       Version,
		ID:            id,
		Timestamp:     timestampNow(),
		Cwd:           opts.Cwd,
		ParentSession: source.header.ID,
	}
	file := filepath.Join(bucket, id+".jsonl")
	if err := writeHeader(file, header); err != nil {
		return nil, err
	}
	s := &Session{header: header, file: file}
	for _, m := range source.BuildMessages() {
		if _, err := s.AppendMessage(m); err != nil {
			return nil, err
		}
	}
	return s, nil
}

// InMemory returns a session that never touches the filesystem. Useful
// when --no-session is set: the caller still gets the same API for
// building messages and recording the in-flight conversation but no
// file is created.
func InMemory(opts Options) *Session {
	id, _ := newUUID()
	return &Session{
		header: Header{
			Type:      "session",
			Version:   Version,
			ID:        id,
			Timestamp: timestampNow(),
			Cwd:       opts.Cwd,
		},
		inMemory: true,
	}
}

// List returns the sessions associated with opts.Cwd, newest first.
func List(opts Options) ([]*Session, error) {
	root, err := resolveRoot(opts.RootDir)
	if err != nil {
		return nil, err
	}
	bucket := filepath.Join(root, cwdBucket(opts.Cwd))
	return listBucket(bucket)
}

// ListAll returns every session under opts.RootDir, regardless of cwd,
// newest first.
func ListAll(opts Options) ([]*Session, error) {
	root, err := resolveRoot(opts.RootDir)
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	var out []*Session
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		sessions, err := listBucket(filepath.Join(root, e.Name()))
		if err != nil {
			return nil, err
		}
		out = append(out, sessions...)
	}
	sort.SliceStable(out, func(i, j int) bool {
		fi, _ := os.Stat(out[i].file)
		fj, _ := os.Stat(out[j].file)
		return fi.ModTime().After(fj.ModTime())
	})
	return out, nil
}

// Resolve accepts a session file path, a full session ID, or a unique
// ID prefix and returns the matching open session. A prefix that
// matches multiple sessions is an error.
func Resolve(opts Options, spec string) (*Session, error) {
	if spec == "" {
		return nil, errors.New("session: empty spec")
	}
	if _, err := os.Stat(spec); err == nil {
		return Open(spec)
	}
	all, err := ListAll(opts)
	if err != nil {
		return nil, err
	}
	var matches []*Session
	for _, s := range all {
		if s.header.ID == spec {
			return s, nil
		}
		if strings.HasPrefix(s.header.ID, spec) {
			matches = append(matches, s)
		}
	}
	switch len(matches) {
	case 0:
		return nil, fmt.Errorf("session: no match for %q", spec)
	case 1:
		return matches[0], nil
	default:
		return nil, fmt.Errorf("session: %q is ambiguous (%d matches)", spec, len(matches))
	}
}

// GetSessionID returns the session UUID.
func (s *Session) GetSessionID() string { return s.header.ID }

// GetSessionFile returns the absolute path on disk, or "" for InMemory
// sessions.
func (s *Session) GetSessionFile() string {
	if s.inMemory {
		return ""
	}
	return s.file
}

// GetHeader returns the header struct.
func (s *Session) GetHeader() Header { return s.header }

// GetEntries returns all message entries in the order they were
// appended, including those on branches that are no longer active.
func (s *Session) GetEntries() []Entry {
	out := make([]Entry, len(s.entries))
	copy(out, s.entries)
	return out
}

// GetLeafID returns the ID of the active leaf entry, or "" before the
// first AppendMessage.
func (s *Session) GetLeafID() string { return s.leaf }

// AppendMessage records m as a new entry whose parentId is the current
// leaf, then moves the leaf to the new entry. Returns the new entry ID.
func (s *Session) AppendMessage(m ai.Message) (string, error) {
	id, err := newEntryID()
	if err != nil {
		return "", err
	}
	cp := m
	entry := Entry{
		Type:      "message",
		ID:        id,
		ParentID:  s.leaf,
		Timestamp: timestampNow(),
		Message:   &cp,
	}
	if !s.inMemory {
		if err := appendLine(s.file, entry); err != nil {
			return "", err
		}
	}
	s.entries = append(s.entries, entry)
	s.leaf = id
	return id, nil
}

// Branch moves the active leaf to entryID without removing existing
// entries. entryID must reference an existing message entry.
func (s *Session) Branch(entryID string) error {
	found := false
	for _, e := range s.entries {
		if e.ID == entryID {
			found = true
			break
		}
	}
	if !found {
		return fmt.Errorf("session: branch: unknown entry %q", entryID)
	}
	if !s.inMemory {
		marker := Entry{
			Type:      "leaf",
			ID:        entryID,
			Timestamp: timestampNow(),
		}
		if err := appendLine(s.file, marker); err != nil {
			return err
		}
	}
	s.leaf = entryID
	return nil
}

// GetChildren returns the entries whose parentId is parentID, in append
// order.
func (s *Session) GetChildren(parentID string) []Entry {
	var out []Entry
	for _, e := range s.entries {
		if e.ParentID == parentID {
			out = append(out, e)
		}
	}
	return out
}

// BuildMessages returns the ai.Messages along the active branch, ordered
// root-to-leaf. Returns nil when the session has no entries.
func (s *Session) BuildMessages() []ai.Message {
	return s.GetBranch(s.leaf)
}

// GetBranch walks back from entryID to the root via parentId, then
// returns the messages in root-to-leaf order. entryID == "" yields nil.
func (s *Session) GetBranch(entryID string) []ai.Message {
	if entryID == "" {
		return nil
	}
	byID := make(map[string]Entry, len(s.entries))
	for _, e := range s.entries {
		byID[e.ID] = e
	}
	var chain []ai.Message
	cur := entryID
	for cur != "" {
		e, ok := byID[cur]
		if !ok {
			break
		}
		if e.Message != nil {
			chain = append(chain, *e.Message)
		}
		cur = e.ParentID
	}
	for i, j := 0, len(chain)-1; i < j; i, j = i+1, j-1 {
		chain[i], chain[j] = chain[j], chain[i]
	}
	return chain
}

// GetTree returns the entries grouped by parent for tree-style rendering.
// The result maps parentID → ordered children.
func (s *Session) GetTree() map[string][]Entry {
	out := make(map[string][]Entry, len(s.entries))
	for _, e := range s.entries {
		out[e.ParentID] = append(out[e.ParentID], e)
	}
	return out
}

// --- helpers ---

func resolveRoot(root string) (string, error) {
	if root != "" {
		return root, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, DefaultRootDir), nil
}

// cwdBucket maps a cwd to a stable on-disk directory name. We use a
// 12-char SHA-256 prefix so the bucket survives unusual characters in
// the cwd path without collisions in practice.
func cwdBucket(cwd string) string {
	h := sha256.Sum256([]byte(cwd))
	return hex.EncodeToString(h[:6])
}

func listBucket(bucket string) ([]*Session, error) {
	entries, err := os.ReadDir(bucket)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	type withMod struct {
		s   *Session
		mod time.Time
	}
	var collected []withMod
	for _, ent := range entries {
		if ent.IsDir() || !strings.HasSuffix(ent.Name(), ".jsonl") {
			continue
		}
		path := filepath.Join(bucket, ent.Name())
		info, err := os.Stat(path)
		if err != nil {
			continue
		}
		s, err := Open(path)
		if err != nil {
			continue
		}
		collected = append(collected, withMod{s: s, mod: info.ModTime()})
	}
	sort.SliceStable(collected, func(i, j int) bool {
		return collected[i].mod.After(collected[j].mod)
	})
	out := make([]*Session, len(collected))
	for i, c := range collected {
		out[i] = c.s
	}
	return out, nil
}

func writeHeader(path string, h Header) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	enc.SetEscapeHTML(false)
	return enc.Encode(h)
}

func appendLine(path string, v any) error {
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	enc.SetEscapeHTML(false)
	return enc.Encode(v)
}

func timestampNow() string {
	return time.Now().UTC().Format(time.RFC3339Nano)
}

func newUUID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	b[6] = (b[6] & 0x0f) | 0x40 // v4
	b[8] = (b[8] & 0x3f) | 0x80 // variant
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16]), nil
}

func newEntryID() (string, error) {
	var b [4]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]), nil
}
