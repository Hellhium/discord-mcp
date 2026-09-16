package events

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"
)

func msg(channel, author string) json.RawMessage {
	return json.RawMessage(fmt.Sprintf(`{"id":"1","guild_id":"9","channel_id":%q,"author":{"id":%q},"content":"x"}`, channel, author))
}

func seqs(evs []Event) []uint64 {
	out := make([]uint64, len(evs))
	for i, e := range evs {
		out[i] = e.Seq
	}
	return out
}

func TestAppendExtractsIDs(t *testing.T) {
	b := newBuffer(10, "boot")
	e := b.Append("MESSAGE_CREATE", msg("5", "7"))
	if e.Seq != 1 || e.GuildID != "9" || e.ChannelID != "5" || e.AuthorID != "7" || e.Time.IsZero() {
		t.Fatalf("event = %+v", e)
	}
	r := b.Append("MESSAGE_REACTION_ADD", json.RawMessage(`{"user_id":"3","channel_id":"5","guild_id":"9"}`))
	if r.AuthorID != "3" || r.Seq != 2 {
		t.Fatalf("reaction = %+v", r)
	}
	c := b.Append("CHANNEL_CREATE", json.RawMessage(`{"id":"44","guild_id":"9"}`))
	if c.ChannelID != "44" {
		t.Fatalf("channel event = %+v", c)
	}
	if b.Cursor() != "boot:3" {
		t.Fatalf("cursor = %s", b.Cursor())
	}
}

func TestSinceWithoutCursorReturnsLatest(t *testing.T) {
	b := newBuffer(10, "boot")
	for i := 0; i < 5; i++ {
		b.Append("MESSAGE_CREATE", msg("5", "7"))
	}
	p, err := b.Since("", Filter{}, 2)
	if err != nil {
		t.Fatal(err)
	}
	if got := seqs(p.Events); fmt.Sprint(got) != "[4 5]" || p.NextCursor != "boot:5" || p.Gap {
		t.Fatalf("page = %v next=%s gap=%v", got, p.NextCursor, p.Gap)
	}
}

func TestSinceCursorFilterAndPaging(t *testing.T) {
	b := newBuffer(10, "boot")
	b.Append("MESSAGE_CREATE", msg("5", "7")) // 1
	b.Append("MESSAGE_CREATE", msg("6", "7")) // 2
	b.Append("TYPING_START", msg("5", "7"))   // 3
	b.Append("MESSAGE_CREATE", msg("5", "8")) // 4
	b.Append("MESSAGE_CREATE", msg("5", "7")) // 5

	f := Filter{Types: []string{"MESSAGE_CREATE"}, ChannelID: "5"}
	p, err := b.Since("boot:0", f, 2)
	if err != nil {
		t.Fatal(err)
	}
	if fmt.Sprint(seqs(p.Events)) != "[1 4]" || p.NextCursor != "boot:4" {
		t.Fatalf("first page = %v next=%s", seqs(p.Events), p.NextCursor)
	}
	p, _ = b.Since(p.NextCursor, f, 2)
	if fmt.Sprint(seqs(p.Events)) != "[5]" || p.NextCursor != "boot:5" {
		t.Fatalf("second page = %v next=%s", seqs(p.Events), p.NextCursor)
	}
	p, _ = b.Since(p.NextCursor, f, 2)
	if len(p.Events) != 0 || p.NextCursor != "boot:5" {
		t.Fatalf("empty page = %v next=%s", seqs(p.Events), p.NextCursor)
	}
	p, _ = b.Since("boot:0", Filter{ExcludeAuthorID: "7"}, 10)
	if fmt.Sprint(seqs(p.Events)) != "[4]" {
		t.Fatalf("exclude author = %v", seqs(p.Events))
	}
}

func TestSinceGapAfterWraparound(t *testing.T) {
	b := newBuffer(3, "boot")
	for i := 0; i < 6; i++ { // keeps 4,5,6
		b.Append("MESSAGE_CREATE", msg("5", "7"))
	}
	p, err := b.Since("boot:1", Filter{}, 10)
	if err != nil {
		t.Fatal(err)
	}
	if !p.Gap || fmt.Sprint(seqs(p.Events)) != "[4 5 6]" {
		t.Fatalf("page = %v gap=%v", seqs(p.Events), p.Gap)
	}
	p, _ = b.Since("boot:3", Filter{}, 10)
	if p.Gap || fmt.Sprint(seqs(p.Events)) != "[4 5 6]" {
		t.Fatalf("cursor at oldest-1 must not be a gap: %v gap=%v", seqs(p.Events), p.Gap)
	}
}

func TestSinceOtherBootIsGap(t *testing.T) {
	b := newBuffer(3, "boot")
	b.Append("MESSAGE_CREATE", msg("5", "7"))
	p, err := b.Since("previous:99", Filter{}, 10)
	if err != nil || !p.Gap || len(p.Events) != 1 {
		t.Fatalf("page = %+v err=%v", p, err)
	}
}

func TestSinceBadCursor(t *testing.T) {
	b := newBuffer(3, "boot")
	for _, c := range []string{"nocolon", "boot:x", "boot:-1", "boot:5"} {
		if _, err := b.Since(c, Filter{}, 10); !errors.Is(err, ErrBadCursor) {
			t.Errorf("Since(%q) err = %v, want ErrBadCursor", c, err)
		}
	}
}

func TestWaitReturnsBufferedEvent(t *testing.T) {
	b := newBuffer(10, "boot")
	b.Append("MESSAGE_CREATE", msg("5", "7"))
	ev, next, gap, err := b.Wait(context.Background(), "boot:0", Filter{Types: []string{"MESSAGE_CREATE"}})
	if err != nil || ev == nil || ev.Seq != 1 || next != "boot:1" || gap {
		t.Fatalf("ev=%v next=%s gap=%v err=%v", ev, next, gap, err)
	}
}

func TestWaitBlocksUntilMatchingAppend(t *testing.T) {
	b := newBuffer(10, "boot")
	done := make(chan *Event, 1)
	go func() {
		ev, _, _, _ := b.Wait(context.Background(), "", Filter{ChannelID: "5", ExcludeAuthorID: "7"})
		done <- ev
	}()
	time.Sleep(20 * time.Millisecond)
	b.Append("MESSAGE_CREATE", msg("5", "7")) // own message: ignored
	b.Append("MESSAGE_CREATE", msg("6", "8")) // other channel: ignored
	b.Append("MESSAGE_CREATE", msg("5", "8")) // match
	select {
	case ev := <-done:
		if ev == nil || ev.Seq != 3 {
			t.Fatalf("ev = %+v", ev)
		}
	case <-time.After(time.Second):
		t.Fatal("Wait did not wake")
	}
}

func TestWaitTimeout(t *testing.T) {
	b := newBuffer(10, "boot")
	b.Append("MESSAGE_CREATE", msg("6", "8"))
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	ev, next, _, err := b.Wait(ctx, "boot:0", Filter{ChannelID: "5"})
	if ev != nil || !errors.Is(err, context.DeadlineExceeded) || next != "boot:1" {
		t.Fatalf("ev=%v next=%s err=%v", ev, next, err)
	}
}

func TestCloseWakesWaiters(t *testing.T) {
	b := newBuffer(10, "boot")
	errc := make(chan error, 1)
	go func() {
		_, _, _, err := b.Wait(context.Background(), "", Filter{})
		errc <- err
	}()
	time.Sleep(20 * time.Millisecond)
	b.Close()
	select {
	case err := <-errc:
		if !errors.Is(err, ErrClosed) {
			t.Fatalf("err = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Close did not wake the waiter")
	}
	b.Append("MESSAGE_CREATE", msg("5", "7")) // must not panic after Close
}

func TestNewBufferBootIDsDiffer(t *testing.T) {
	if NewBuffer(1).Cursor() == NewBuffer(1).Cursor() {
		t.Fatal("two buffers share a boot id")
	}
}
