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

func TestSetupProgressTerminal(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var out bytes.Buffer
		p := &setupProgress{out: &out, terminal: true}
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
