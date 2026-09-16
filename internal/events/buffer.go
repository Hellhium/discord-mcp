// Package events buffers Gateway events for configured bots and serves them
// to the event tools.
//
// The buffer is a bounded ring shared read-only by every client; there is no
// per-client state. A client holds a cursor "<boot-id>:<seq>" naming the last
// event it saw. The boot ID is random per process, so a cursor from before a
// restart — or one older than the oldest buffered event — is reported as a
// gap instead of silently skipping events.
package events

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
)

var (
	// ErrClosed is returned by Wait once the buffer is closed.
	ErrClosed = errors.New("event buffer closed")
	// ErrBadCursor is wrapped by errors about malformed cursors.
	ErrBadCursor = errors.New("invalid cursor")
)

// Event is one buffered Gateway dispatch.
type Event struct {
	Seq       uint64          `json:"seq"`
	Type      string          `json:"type"`
	Time      time.Time       `json:"time"`
	GuildID   string          `json:"guild_id,omitempty"`
	ChannelID string          `json:"channel_id,omitempty"`
	AuthorID  string          `json:"author_id,omitempty"`
	Data      json.RawMessage `json:"data,omitempty"`
}

// Filter selects events. Empty fields match everything.
type Filter struct {
	Types           []string
	GuildID         string
	ChannelID       string
	AuthorID        string
	ExcludeAuthorID string
}

// Match reports whether e passes the filter.
func (f Filter) Match(e Event) bool {
	switch {
	case len(f.Types) > 0 && !slices.Contains(f.Types, e.Type):
		return false
	case f.GuildID != "" && e.GuildID != f.GuildID:
		return false
	case f.ChannelID != "" && e.ChannelID != f.ChannelID:
		return false
	case f.AuthorID != "" && e.AuthorID != f.AuthorID:
		return false
	case f.ExcludeAuthorID != "" && e.AuthorID == f.ExcludeAuthorID:
		return false
	}
	return true
}

// Page is a batch of events and the cursor to continue from.
type Page struct {
	Events     []Event
	NextCursor string
	Gap        bool
}

// Buffer is a bounded, concurrency-safe ring of events.
type Buffer struct {
	mu      sync.Mutex
	bootID  string
	ring    []Event
	start   int // index of the oldest event
	count   int
	lastSeq uint64
	changed chan struct{} // closed and replaced on every Append
	closed  bool
}

// NewBuffer returns a buffer holding the last size events.
func NewBuffer(size int) *Buffer {
	var b [8]byte
	_, _ = rand.Read(b[:])
	return newBuffer(size, hex.EncodeToString(b[:]))
}

func newBuffer(size int, bootID string) *Buffer {
	return &Buffer{bootID: bootID, ring: make([]Event, size), changed: make(chan struct{})}
}

// Append stores an event, dropping the oldest when full, and wakes waiters.
func (b *Buffer) Append(typ string, data json.RawMessage) Event {
	ids := extractIDs(typ, data)
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return Event{}
	}
	b.lastSeq++
	e := Event{Seq: b.lastSeq, Type: typ, Time: time.Now().UTC(), GuildID: ids.guild, ChannelID: ids.channel, AuthorID: ids.author, Data: data}
	if b.count < len(b.ring) {
		b.ring[(b.start+b.count)%len(b.ring)] = e
		b.count++
	} else {
		b.ring[b.start] = e
		b.start = (b.start + 1) % len(b.ring)
	}
	close(b.changed)
	b.changed = make(chan struct{})
	return e
}

// Cursor returns the cursor of the newest event (seq 0 when empty).
func (b *Buffer) Cursor() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.cursor(b.lastSeq)
}

func (b *Buffer) cursor(seq uint64) string { return b.bootID + ":" + strconv.FormatUint(seq, 10) }

// Since returns up to limit events matching f after cursor. With an empty
// cursor it returns the latest limit matching events.
func (b *Buffer) Since(cursor string, f Filter, limit int) (Page, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.since(cursor, f, limit)
}

func (b *Buffer) since(cursor string, f Filter, limit int) (Page, error) {
	if cursor == "" {
		var out []Event
		for i := b.count - 1; i >= 0 && len(out) < limit; i-- {
			if e := b.at(i); f.Match(e) {
				out = append(out, e)
			}
		}
		slices.Reverse(out)
		return Page{Events: out, NextCursor: b.cursor(b.lastSeq)}, nil
	}

	after, gap, err := b.parse(cursor)
	if err != nil {
		return Page{}, err
	}
	next := b.lastSeq
	var out []Event
	for i := 0; i < b.count; i++ {
		e := b.at(i)
		if e.Seq <= after || !f.Match(e) {
			continue
		}
		out = append(out, e)
		if len(out) == limit {
			next = e.Seq
			break
		}
	}
	if next < after {
		next = after
	}
	return Page{Events: out, NextCursor: b.cursor(next), Gap: gap}, nil
}

// at returns the i-th oldest event.
func (b *Buffer) at(i int) Event { return b.ring[(b.start+i)%len(b.ring)] }

// parse returns the sequence to read after and whether events were missed.
func (b *Buffer) parse(cursor string) (uint64, bool, error) {
	boot, seqStr, ok := strings.Cut(cursor, ":")
	if !ok || boot == "" {
		return 0, false, fmt.Errorf("%w: %q (want the next_cursor of a previous call)", ErrBadCursor, cursor)
	}
	seq, err := strconv.ParseUint(seqStr, 10, 64)
	if err != nil {
		return 0, false, fmt.Errorf("%w: %q (want the next_cursor of a previous call)", ErrBadCursor, cursor)
	}
	if boot != b.bootID {
		return 0, true, nil // cursor from before a restart
	}
	if seq > b.lastSeq {
		return 0, false, fmt.Errorf("%w: %q is ahead of the newest event", ErrBadCursor, cursor)
	}
	oldest := b.lastSeq + 1
	if b.count > 0 {
		oldest = b.at(0).Seq
	}
	return seq, seq+1 < oldest, nil
}

// Wait blocks until an event matching f arrives after cursor (after the
// newest event when cursor is empty). It returns the cursor to resume from
// whatever happens, and whether any gap was crossed.
func (b *Buffer) Wait(ctx context.Context, cursor string, f Filter) (*Event, string, bool, error) {
	if cursor == "" {
		cursor = b.Cursor()
	}
	gap := false
	for {
		b.mu.Lock()
		if b.closed {
			b.mu.Unlock()
			return nil, cursor, gap, ErrClosed
		}
		p, err := b.since(cursor, f, 1)
		ch := b.changed
		b.mu.Unlock()
		if err != nil {
			return nil, cursor, gap, err
		}
		gap = gap || p.Gap
		if len(p.Events) == 1 {
			return &p.Events[0], p.NextCursor, gap, nil
		}
		cursor = p.NextCursor
		select {
		case <-ch:
		case <-ctx.Done():
			return nil, cursor, gap, ctx.Err()
		}
	}
}

// Close wakes every waiter with ErrClosed; later Appends are dropped.
func (b *Buffer) Close() {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return
	}
	b.closed = true
	close(b.changed)
}

type eventIDs struct{ guild, channel, author string }

// extractIDs reads the IDs the filters use. Message events carry author.id,
// reaction events user_id, and channel/thread events their own id.
func extractIDs(typ string, data json.RawMessage) eventIDs {
	var v struct {
		ID        string `json:"id"`
		GuildID   string `json:"guild_id"`
		ChannelID string `json:"channel_id"`
		UserID    string `json:"user_id"`
		Author    *struct {
			ID string `json:"id"`
		} `json:"author"`
	}
	_ = json.Unmarshal(data, &v)
	ids := eventIDs{guild: v.GuildID, channel: v.ChannelID, author: v.UserID}
	if v.Author != nil {
		ids.author = v.Author.ID
	}
	if ids.channel == "" && (strings.HasPrefix(typ, "CHANNEL_") || strings.HasPrefix(typ, "THREAD_")) {
		ids.channel = v.ID
	}
	return ids
}
