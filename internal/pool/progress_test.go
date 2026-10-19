package pool

import (
	"bytes"
	"errors"
	"strings"
	"testing"
	"testing/synctest"
	"time"
)

func TestSetupProgressRedirected(t *testing.T) {
	var out bytes.Buffer
	p := &setupProgress{out: &out}
	p.start("Fetching from origin...")
	p.finish()
	p.start("Creating worktree...")
	p.finish()
	want := "🌳 Fetching from origin...\n🌳 Creating worktree...\n"
	if got := out.String(); got != want {
		t.Fatalf("redirected transcript = %q, want %q", got, want)
	}
}

func TestSetupProgressResize(t *testing.T) {
	var out bytes.Buffer
	width := 80
	p := &setupProgress{out: &out, terminal: true, width: func() int { return width }}
	p.draw()
	if !strings.Contains(out.String(), "[===         ]") {
		t.Fatalf("missing initial frame: %q", out.String())
	}
	out.Reset()
	width = 10
	p.draw()
	if got, want := out.String(), "\r         \r"; got != want {
		t.Fatalf("resized clear = %q, want %q without wrapping", got, want)
	}
	out.Reset()
	p.draw()
	if out.Len() != 0 {
		t.Fatalf("animation continued in narrow terminal: %q", out.String())
	}
	width = 18
	p.draw()
	if !strings.Contains(out.String(), "[ ===        ]") {
		t.Fatalf("animation did not resume in wider terminal: %q", out.String())
	}
}

func TestSetupProgressTerminal(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var out bytes.Buffer
		p := &setupProgress{out: &out, terminal: true, width: func() int { return 80 }}
		snapshot := func() string {
			p.mu.Lock()
			defer p.mu.Unlock()
			return out.String()
		}
		p.start("Creating worktree...")
		synctest.Wait()
		first := snapshot()
		time.Sleep(100 * time.Millisecond) // Virtual time, not a wall-clock delay.
		synctest.Wait()
		second := strings.TrimPrefix(snapshot(), first)
		if !strings.Contains(first, "[===         ]") || !strings.Contains(second, "[ ===        ]") {
			t.Fatalf("bar did not move: first %q, next %q", first, second)
		}

		p.finish()
		finished := snapshot()
		if !strings.HasSuffix(finished, "\r                 \r") {
			t.Fatalf("bar not cleared on finish: %q", finished)
		}
		time.Sleep(time.Second)
		synctest.Wait()
		if snapshot() != finished {
			t.Fatal("animation continued after finish")
		}
		// A following operation starts cleanly and stops even on failure.
		wantErr := errors.New("fetch failed")
		err := p.run("Fetching from origin...", func() error {
			time.Sleep(200 * time.Millisecond)
			return wantErr
		})
		if err != wantErr {
			t.Fatalf("operation error = %v, want %v", err, wantErr)
		}
		failed := snapshot()
		if !strings.HasSuffix(failed, "\r                 \r") {
			t.Fatalf("bar not cleared on error: %q", failed)
		}
		time.Sleep(time.Second)
		synctest.Wait()
		if snapshot() != failed {
			t.Fatal("animation continued after failure")
		}
	})
}
