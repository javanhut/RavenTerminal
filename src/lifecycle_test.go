package main

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/javanhut/RavenTerminal/src/tab"
)

func TestSignalRequestsQuit(t *testing.T) {
	var wakes atomic.Int32
	stop := installSignalHandler(func() { wakes.Add(1) })
	defer stop()
	quitRequested.Store(false)
	defer quitRequested.Store(false)
	if err := syscall.Kill(os.Getpid(), syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for !quitRequested.Load() || wakes.Load() == 0 {
		if time.Now().After(deadline) {
			t.Fatal("SIGTERM did not request a quit and wake the loop")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestIdleTimeoutFor(t *testing.T) {
	now := time.Unix(100, 0)
	cases := []struct {
		name string
		s    idleState
		want float64
	}{
		{"idle", idleState{}, maxIdle},
		{"drag", idleState{selecting: true}, 0.016},
		{"sync", idleState{syncActive: true}, 0.016},
		{"blink edge", idleState{nextBlink: now.Add(200 * time.Millisecond)}, 0.2},
		{"blink overdue", idleState{nextBlink: now.Add(-time.Second)}, 0},
		{"toast expiry", idleState{toastUntil: now.Add(300 * time.Millisecond)}, 0.3},
		{"toast and blink", idleState{toastUntil: now.Add(300 * time.Millisecond), nextBlink: now.Add(100 * time.Millisecond)}, 0.1},
		{"spinner", idleState{animating: true}, 0.1},
		{"blink beyond max", idleState{nextBlink: now.Add(5 * time.Second)}, maxIdle},
	}
	for _, c := range cases {
		got := idleTimeoutFor(c.s, now)
		if d := got - c.want; d > 1e-9 || d < -1e-9 {
			t.Errorf("%s: idleTimeoutFor = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestDeliverWakesAndHonoursDone(t *testing.T) {
	var wakes atomic.Int32
	prev := wakeUI
	wakeUI = func() { wakes.Add(1) }
	defer func() { wakeUI = prev }()

	ch := make(chan int, 1)
	if !deliver(context.Background(), ch, 1) || <-ch != 1 || wakes.Load() != 1 {
		t.Fatal("deliver did not send and wake")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	ch <- 0 // full: a closed window never drains it
	start := time.Now()
	if deliver(ctx, ch, 2) {
		t.Fatal("deliver reported success after the window closed")
	}
	if time.Since(start) > 50*time.Millisecond {
		t.Fatal("deliver blocked on a closed window")
	}
}

func TestNewRequestCancelsSuperseded(t *testing.T) {
	a := &App{}
	a.ctx, a.cancel = context.WithCancel(context.Background())
	first := a.newRequest(&a.searchCancel)
	second := a.newRequest(&a.searchCancel)
	if first.Err() == nil {
		t.Fatal("superseded request was not cancelled")
	}
	if second.Err() != nil {
		t.Fatal("current request cancelled")
	}
	a.cancel()
	if second.Err() == nil {
		t.Fatal("closing the window did not cancel in-flight requests")
	}
}

func TestGoSafeRecovers(t *testing.T) {
	var buf bytes.Buffer
	var mu sync.Mutex
	log.SetOutput(writerFunc(func(p []byte) (int, error) { mu.Lock(); defer mu.Unlock(); return buf.Write(p) }))
	defer log.SetOutput(os.Stderr)
	done := make(chan struct{})
	goSafe("websearch", func() {
		defer close(done)
		panic("boom")
	})
	<-done
	deadline := time.Now().Add(time.Second)
	for {
		mu.Lock()
		s := buf.String()
		mu.Unlock()
		if bytes.Contains([]byte(s), []byte("websearch")) && bytes.Contains([]byte(s), []byte("boom")) {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("panic not logged: %q", s)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

type writerFunc func([]byte) (int, error)

func (f writerFunc) Write(p []byte) (int, error) { return f(p) }

func TestWriteSessionAtomic(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.json")
	var wg sync.WaitGroup
	for i := range 32 {
		wg.Go(func() {
			s := session{ActiveIndex: i}
			for j := 0; j <= i; j++ {
				s.Tabs = append(s.Tabs, tab.Layout{Dir: fmt.Sprintf("/dir/%d/%d", i, j)})
			}
			if err := writeSession(path, s); err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	got, err := readSession(path)
	if err != nil {
		t.Fatalf("session file corrupt after concurrent saves: %v", err)
	}
	if len(got.Tabs) != got.ActiveIndex+1 {
		t.Fatalf("session mixes two writes: %d tabs, active %d", len(got.Tabs), got.ActiveIndex)
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Fatalf("temp files left behind: %v", entries)
	}
}
