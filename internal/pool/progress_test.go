package pool

import (
	"bytes"
	"errors"
	"fmt"
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
	p.start("Running post-create hooks...")
	if _, err := p.output(&out).Write([]byte("hook output\n")); err != nil {
		t.Fatal(err)
	}
	p.finish()
	want := "🌳 Fetching from origin...\n🌳 Running post-create hooks...\nhook output\n"
	if got := out.String(); got != want {
		t.Fatalf("redirected transcript = %q, want %q", got, want)
	}
}

// Each hook stream owns its partial line: a newline on redirected stdout
// must not allow animation to overwrite an unfinished stderr line (or vice versa).
func TestSetupProgressIndependentHookStreams(t *testing.T) {
	for _, partialStderr := range []bool{true, false} {
		t.Run(fmt.Sprintf("partial-stderr=%v", partialStderr), func(t *testing.T) {
			var stderr, stdout bytes.Buffer
			p := &setupProgress{out: &stderr, terminal: true, width: func() int { return 80 }}
			partial, complete := p.output(&stderr), p.output(&stdout)
			if !partialStderr {
				partial, complete = complete, partial
			}
			if _, err := partial.Write([]byte("hook-partial")); err != nil {
				t.Fatal(err)
			}
			if _, err := complete.Write([]byte("stdout-line\n")); err != nil {
				t.Fatal(err)
			}
			before := stderr.String()
			p.draw()
			if stderr.String() != before {
				t.Fatalf("animation overwrote a partial hook line: before=%q after=%q", before, stderr.String())
			}
			if _, err := partial.Write([]byte("-finished\n")); err != nil {
				t.Fatal(err)
			}
			before = stderr.String()
			p.draw()
			if stderr.String() == before {
				t.Fatal("animation did not resume after both hook streams finished their lines")
			}
			partialOutput := stderr.String()
			if !partialStderr {
				partialOutput = stdout.String()
			}
			if !strings.Contains(partialOutput, "hook-partial-finished\n") {
				t.Fatalf("partial hook output corrupted: %q", partialOutput)
			}
		})
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
		p.start("Running post-create hooks...")
		synctest.Wait()
		first := snapshot()
		time.Sleep(100 * time.Millisecond) // Virtual time, not a wall-clock delay.
		synctest.Wait()
		second := strings.TrimPrefix(snapshot(), first)
		if !strings.Contains(first, "[===         ]") || !strings.Contains(second, "[ ===        ]") {
			t.Fatalf("bar did not move: first %q, next %q", first, second)
		}

		writer := p.output(&out)
		if _, err := writer.Write([]byte("hook partial")); err != nil {
			t.Fatal(err)
		}
		partial := snapshot()
		time.Sleep(300 * time.Millisecond)
		synctest.Wait()
		if snapshot() != partial {
			t.Fatal("animation overwrote a partial hook line")
		}
		if _, err := writer.Write([]byte(" line\n")); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(snapshot(), "hook partial line\n") {
			t.Fatalf("hook output corrupted: %q", snapshot())
		}
		before := len(snapshot())
		time.Sleep(100 * time.Millisecond)
		synctest.Wait()
		if len(snapshot()) == before {
			t.Fatal("animation did not resume after hook output")
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
		// A following step starts cleanly, and its stdout destination stays intact.
		var stdout bytes.Buffer
		p.start("Creating worktree...")
		if _, err := p.output(&stdout).Write([]byte("stdout bytes\n")); err != nil {
			t.Fatal(err)
		}
		p.finish()
		if stdout.String() != "stdout bytes\n" {
			t.Fatalf("stdout changed: %q", stdout.String())
		}
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
