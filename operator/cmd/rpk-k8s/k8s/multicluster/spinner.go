// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package multicluster

import (
	"fmt"
	"io"
	"os"
	"sync"
	"time"

	"github.com/mattn/go-isatty"
)

// spinner renders a minimal progress indicator for long-running
// interactive operations (LoadBalancer provisioning). When the output
// isn't a TTY it falls back to plain-text lines so log capture stays
// readable. Safe to UpdateMessage from any goroutine; all writes go
// through the embedded mutex.
type spinner struct {
	out       io.Writer
	isTTY     bool
	msg       string
	mu        sync.Mutex
	stop      chan struct{}
	done      chan struct{}
	startedAt time.Time
}

// newSpinner constructs a spinner bound to out. Call Start to begin
// animating and Stop(finalMsg) to tear it down and emit a final line.
func newSpinner(out io.Writer, msg string) *spinner {
	s := &spinner{
		out:  out,
		msg:  msg,
		stop: make(chan struct{}),
		done: make(chan struct{}),
	}
	// Only animate when out is our own stdout AND stdout is a TTY.
	// Every other case (redirected, piped, tests with bytes.Buffer)
	// renders plain-text events.
	if f, ok := out.(*os.File); ok && f == os.Stdout && isatty.IsTerminal(f.Fd()) {
		s.isTTY = true
	}
	return s
}

// Start begins animating. Noop-idempotent guard omitted intentionally —
// callers pair Start/Stop once.
func (s *spinner) Start() {
	s.startedAt = time.Now()
	if !s.isTTY {
		fmt.Fprintf(s.out, "  %s...\n", s.msg)
		close(s.done)
		return
	}
	go s.run()
}

// UpdateMessage changes the text shown next to the spinner. Effective
// on the next tick (~120ms).
func (s *spinner) UpdateMessage(msg string) {
	s.mu.Lock()
	s.msg = msg
	s.mu.Unlock()
}

// Stop halts animation, clears the current line, and prints finalMsg
// on its own line. finalMsg should include any success/failure marker.
func (s *spinner) Stop(finalMsg string) {
	if s.isTTY {
		close(s.stop)
	}
	<-s.done
	if s.isTTY {
		fmt.Fprintf(s.out, "\r\033[K%s\n", finalMsg)
	} else {
		fmt.Fprintf(s.out, "  %s\n", finalMsg)
	}
}

func (s *spinner) run() {
	defer close(s.done)
	// Braille-dots cycle — renders the same in common monospace
	// terminals (iTerm2, Alacritty, Ghostty, Terminal.app).
	frames := []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}
	ticker := time.NewTicker(120 * time.Millisecond)
	defer ticker.Stop()
	i := 0
	for {
		select {
		case <-s.stop:
			return
		case <-ticker.C:
			s.mu.Lock()
			msg := s.msg
			s.mu.Unlock()
			elapsed := time.Since(s.startedAt).Round(time.Second)
			fmt.Fprintf(s.out, "\r\033[K%s %s (%s)", frames[i%len(frames)], msg, elapsed)
			i++
		}
	}
}
