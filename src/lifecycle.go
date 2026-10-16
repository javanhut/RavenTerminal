package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"runtime/debug"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/javanhut/RavenTerminal/src/window"
)

// wakeUI wakes the GLFW main loop from any goroutine.
var wakeUI = window.PostEmptyEvent

// goSafe runs fn on a goroutine; a panic is logged instead of taking the
// whole terminal down.
func goSafe(name string, fn func()) {
	go func() {
		defer recoverLog(name)
		fn()
	}()
}

func recoverLog(name string) {
	if r := recover(); r != nil {
		log.Printf("raven: recovered panic in %s: %v\n%s", name, r, debug.Stack())
	}
}

// deliver sends v to the main loop and wakes it. It gives up when ctx ends
// (the request was superseded or the window closed), so a background worker
// never blocks forever on a channel nobody drains.
func deliver[T any](ctx context.Context, ch chan<- T, v T) bool {
	select {
	case ch <- v:
		wakeUI()
		return true
	case <-ctx.Done():
		return false
	}
}

// baseCtx is the window's lifetime context, cancelled on shutdown.
func (a *App) baseCtx() context.Context {
	if a.ctx == nil {
		return context.Background()
	}
	return a.ctx
}

// newRequest starts a cancellable request in the slot *cancel, cancelling
// whatever request it supersedes.
func (a *App) newRequest(cancel *context.CancelFunc) context.Context {
	if *cancel != nil {
		(*cancel)()
	}
	ctx, c := context.WithCancel(a.baseCtx())
	*cancel = c
	return ctx
}

// quitRequested is set by SIGTERM/SIGHUP/SIGINT; every window then shuts
// down through its normal close path (session save included).
var quitRequested atomic.Bool

// installSignalHandler turns termination signals into a clean shutdown. A
// second signal while the first is still being handled exits at once.
func installSignalHandler(wake func()) (stop func()) {
	ch := make(chan os.Signal, 2)
	signal.Notify(ch, syscall.SIGTERM, syscall.SIGHUP, syscall.SIGINT)
	done := make(chan struct{})
	go func() {
		defer recoverLog("signal handler")
		for {
			select {
			case sig := <-ch:
				if !quitRequested.CompareAndSwap(false, true) {
					os.Exit(128 + int(sig.(syscall.Signal)))
				}
				wake()
			case <-done:
				return
			}
		}
	}()
	return func() {
		signal.Stop(ch)
		close(done)
	}
}

// maxIdle is how long the main loop sleeps when nothing animates; any input,
// PTY output or background result wakes it sooner.
const maxIdle = 1.0

// idleState is what decides how soon a window needs its next frame.
type idleState struct {
	selecting  bool // drag selection: edge auto-scroll
	syncActive bool // ?2026 held: poll for its release
	animating  bool // spinner or modal with timed phases
	nextBlink  time.Time
	toastUntil time.Time
}

func idleTimeoutFor(s idleState, now time.Time) float64 {
	if s.selecting || s.syncActive {
		return 0.016
	}
	t := maxIdle
	if s.animating {
		t = 0.1
	}
	for _, at := range []time.Time{s.nextBlink, s.toastUntil} {
		if !at.IsZero() {
			t = min(t, max(0, at.Sub(now).Seconds()))
		}
	}
	return t
}

// Launch options applied to new windows: windowClass to every window,
// initialTitle to the first one only.
var (
	windowClass  string
	initialTitle string
)
