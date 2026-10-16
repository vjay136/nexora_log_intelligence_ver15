// Package multiline implements a state-machine multiline log assembler
// inspired by Fluent Bit's multiline parser design.
package multiline

import (
	"regexp"
	"time"
)

// Rule defines a multiline block pattern.
type Rule struct {
	Name       string
	StartRE    *regexp.Regexp
	ContinueRE *regexp.Regexp // nil = any line continues until next start
}

// Config holds multiline assembler settings.
type Config struct {
	Enabled        bool          `json:"enabled"`
	MaxBytes       int           `json:"max_bytes,omitempty"`
	MaxLines       int           `json:"max_lines,omitempty"`
	FlushTimeout   time.Duration `json:"-"`
	FlushTimeoutMS int           `json:"flush_timeout_ms,omitempty"`
	Rules          []string      `json:"rules,omitempty"`
}

// BuiltinRules contains pre-defined multiline detection rules.
var BuiltinRules = map[string]*Rule{
	"java_exception": {
		Name:       "java_exception",
		StartRE:    regexp.MustCompile(`^\w[\w.]*Exception[:\s]|^Exception in thread`),
		ContinueRE: regexp.MustCompile(`^\s+at |\s+\.\.\. \d+ more`),
	},
	"python_traceback": {
		Name:       "python_traceback",
		StartRE:    regexp.MustCompile(`^Traceback \(most recent call last\):`),
		ContinueRE: regexp.MustCompile(`^\s+File "|^[A-Za-z][\w.]*Error:|^[A-Za-z][\w.]*Exception:`),
	},
	"go_panic": {
		Name:       "go_panic",
		StartRE:    regexp.MustCompile(`^goroutine \d+ \[`),
		ContinueRE: regexp.MustCompile(`^\s`),
	},
	"generic_continuation": {
		Name:       "generic_continuation",
		StartRE:    regexp.MustCompile(`^\S`), // any non-indented line starts a block
		ContinueRE: regexp.MustCompile(`^\s`), // indented lines continue
	},
}

const (
	defaultMaxBytes     = 2 * 1024 * 1024 // 2MB
	maxMaxBytes         = 8 * 1024 * 1024 // hard per-event memory ceiling
	defaultMaxLines     = 10_000
	maxMaxLines         = 10_000
	defaultFlushTimeout = 4 * time.Second
)

// Assembler accumulates lines into multiline events for a single input source.
type Assembler struct {
	cfg       Config
	rules     []*Rule
	buf       []byte
	lines     int
	lastLine  time.Time
	active    bool
	truncated bool
}

// New creates an Assembler. ruleNames selects from BuiltinRules; empty = all.
func New(cfg Config) *Assembler {
	maxBytes := cfg.MaxBytes
	if maxBytes <= 0 {
		maxBytes = defaultMaxBytes
	} else if maxBytes > maxMaxBytes {
		maxBytes = maxMaxBytes
	}
	maxLines := cfg.MaxLines
	if maxLines <= 0 || maxLines > maxMaxLines {
		maxLines = defaultMaxLines
	}
	flush := cfg.FlushTimeout
	if flush <= 0 && cfg.FlushTimeoutMS > 0 {
		flush = time.Duration(cfg.FlushTimeoutMS) * time.Millisecond
	}
	if flush <= 0 {
		flush = defaultFlushTimeout
	}
	a := &Assembler{
		cfg: Config{
			Enabled:      cfg.Enabled,
			MaxBytes:     maxBytes,
			MaxLines:     maxLines,
			FlushTimeout: flush,
			Rules:        cfg.Rules,
		},
	}
	if len(cfg.Rules) == 0 {
		// Default: java + python + go panic
		a.rules = []*Rule{
			BuiltinRules["java_exception"],
			BuiltinRules["python_traceback"],
			BuiltinRules["go_panic"],
		}
	} else {
		for _, name := range cfg.Rules {
			if r, ok := BuiltinRules[name]; ok {
				a.rules = append(a.rules, r)
			}
		}
	}
	return a
}

// Result holds the assembled event.
type Result struct {
	Line      string
	Truncated bool
}

// Feed processes one raw line. Returns a completed Result when a multiline
// block is finished, or nil when more lines are needed.
// In --once mode call Flush() after the last line.
func (a *Assembler) Feed(raw string, now time.Time) *Result {
	if !a.cfg.Enabled {
		return &Result{Line: raw}
	}

	// Check timeout flush first.
	if a.active && !a.lastLine.IsZero() && now.Sub(a.lastLine) >= a.cfg.FlushTimeout {
		r := a.flush()
		// Then process the new line as a fresh start.
		a.start(raw, now)
		return r
	}

	// Is this line a continuation of the current block?
	if a.active && a.isContinuation(raw) {
		if a.lines >= a.cfg.MaxLines {
			// Split a pathological continuation stream into bounded events while
			// retaining the current line as the first line of the next event.
			a.truncated = true
			r := a.flush()
			a.start(raw, now)
			return r
		}
		a.append(raw, now)
		return nil
	}

	// Does this line start a new multiline block?
	if a.isStart(raw) {
		var r *Result
		if a.active {
			r = a.flush()
		}
		a.start(raw, now)
		return r // may be nil if nothing was buffered
	}

	// Plain line with nothing buffered — emit immediately.
	if !a.active {
		return &Result{Line: raw}
	}

	// Plain line while a block is active — flush the block, emit plain.
	r := a.flush()
	_ = r // caller will get this; emit raw next
	// Emit the flushed block, then this line separately.
	// We use a two-result pattern by returning the flush and then the raw.
	// For simplicity: append raw to a new buffer and return the old flush.
	a.start(raw, now)
	return r
}

// Flush forces emission of any buffered content (use at EOF in --once mode).
func (a *Assembler) Flush() *Result {
	if !a.active {
		return nil
	}
	return a.flush()
}

// TimedOut returns true if the current buffer has exceeded FlushTimeout.
func (a *Assembler) TimedOut(now time.Time) bool {
	return a.active && !a.lastLine.IsZero() && now.Sub(a.lastLine) >= a.cfg.FlushTimeout
}

func (a *Assembler) isStart(line string) bool {
	for _, r := range a.rules {
		if r.StartRE.MatchString(line) {
			return true
		}
	}
	return false
}

func (a *Assembler) isContinuation(line string) bool {
	for _, r := range a.rules {
		if r.ContinueRE != nil && r.ContinueRE.MatchString(line) {
			return true
		}
	}
	return false
}

func (a *Assembler) start(line string, now time.Time) {
	a.truncated = false
	if len(line) > a.cfg.MaxBytes {
		a.buf = append(a.buf[:0], line[:a.cfg.MaxBytes]...)
		a.truncated = true
	} else {
		a.buf = append(a.buf[:0], line...)
	}
	a.active = true
	a.lines = 1
	a.lastLine = now
}

func (a *Assembler) append(line string, now time.Time) {
	a.lines++
	if len(line) > a.cfg.MaxBytes-len(a.buf)-1 {
		if !a.truncated {
			a.truncated = true
		}
		return
	}
	a.buf = append(a.buf, '\n')
	a.buf = append(a.buf, line...)
	a.lastLine = now
}

func (a *Assembler) flush() *Result {
	r := &Result{
		Line:      string(a.buf),
		Truncated: a.truncated,
	}
	a.buf = nil
	a.active = false
	a.truncated = false
	a.lines = 0
	return r
}

// Pending reports whether an event is currently buffered.
func (a *Assembler) Pending() bool { return a.active }

// MarkTruncated marks the currently buffered event as truncated.
func (a *Assembler) MarkTruncated() {
	if a.active {
		a.truncated = true
	}
}
