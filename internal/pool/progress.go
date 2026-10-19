package pool

import (
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/mattn/go-isatty"
)

// setupProgress keeps the step transcript on stderr, with an indeterminate bar
// only on terminals. Each operation stops its animation before returning or
// emitting diagnostics. No cursor hiding or terminal mode changes are needed.
type setupProgress struct {
	out      io.Writer
	terminal bool
	stop     chan struct{}
	done     chan struct{}
	mu       sync.Mutex
	visible  bool
	lineOpen bool
	frame    int
}

func newSetupProgress() *setupProgress {
	return &setupProgress{out: os.Stderr, terminal: isatty.IsTerminal(os.Stderr.Fd()) || isatty.IsCygwinTerminal(os.Stderr.Fd())}
}

func (p *setupProgress) run(label string, operation func() error) error {
	p.start(label)
	defer p.finish()
	return operation()
}

func (p *setupProgress) start(label string) {
	fmt.Fprintln(p.out, "🌳 "+label)
	if !p.terminal {
		return
	}
	p.stop, p.done = make(chan struct{}), make(chan struct{})
	p.frame = 0
	p.draw()
	go func() {
		defer close(p.done)
		ticker := time.NewTicker(100 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-p.stop:
				return
			case <-ticker.C:
				p.mu.Lock()
				p.draw()
				p.mu.Unlock()
			}
		}
	}()
}

func (p *setupProgress) finish() {
	if p.stop == nil {
		return
	}
	close(p.stop)
	<-p.done
	p.stop = nil
	p.clear()
}

func (p *setupProgress) draw() {
	if p.lineOpen {
		return
	}
	// Bounce a fixed-width segment, without implying a percentage or ETA.
	position := p.frame % 18
	if position > 9 {
		position = 18 - position
	}
	fmt.Fprintf(p.out, "\r🌳 [%s===%s]", strings.Repeat(" ", position), strings.Repeat(" ", 9-position))
	p.frame++
	p.visible = true
}

func (p *setupProgress) clear() {
	if p.visible {
		// The tree occupies two terminal cells; the complete bar occupies 17.
		fmt.Fprint(p.out, "\r                 \r")
		p.visible = false
	}
}

// output clears the bar before streaming hook output, preserving its destination
// and bytes. Partial lines suspend redraws until the hook finishes the line.
func (p *setupProgress) output(dst io.Writer) io.Writer {
	if !p.terminal {
		return dst
	}
	return setupProgressWriter{progress: p, dst: dst}
}

type setupProgressWriter struct {
	progress *setupProgress
	dst      io.Writer
}

func (w setupProgressWriter) Write(b []byte) (int, error) {
	p := w.progress
	p.mu.Lock()
	defer p.mu.Unlock()
	p.clear()
	n, err := w.dst.Write(b)
	if n > 0 {
		p.lineOpen = b[n-1] != '\n'
	}
	return n, err
}
