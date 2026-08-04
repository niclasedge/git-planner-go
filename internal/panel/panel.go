// Package panel implements the config-driven pages — everything from page 3 on.
//
// A widget owns its own data, its own refresh interval and its own lock. The
// scheduler updates only the widgets that are due, concurrently, so one slow
// endpoint cannot hold up a page.
package panel

import (
	"context"
	"fmt"
	"sync"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/niclasedge/git-planner-go/internal/config"
	"github.com/niclasedge/git-planner-go/internal/sched"
)

// defaultInterval applies to widgets that do not set `cache:`.
const defaultInterval = 5 * time.Minute

// Widget is the contract every widget type satisfies. Kind() doubles as the
// template name, so adding a type means adding a struct and a template — no
// switch statement to extend.
type Widget interface {
	Kind() string
	Init() error
	Update(ctx context.Context)

	ID() int
	Title() string
	Due(now time.Time) bool
	Err() error
	UpdatedAt() time.Time

	base() *Base
}

// Base carries the bookkeeping every widget shares. Embed it inline so its YAML
// keys sit at the widget's own level.
type Base struct {
	WTitle string          `yaml:"title"`
	Cache  config.Duration `yaml:"cache"`

	mu        sync.RWMutex
	id        int
	sch       sched.Schedule
	err       error
	updatedAt time.Time
}

func (b *Base) base() *Base   { return b }
func (b *Base) ID() int       { return b.id }
func (b *Base) Title() string { return b.WTitle }

func (b *Base) Due(now time.Time) bool {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.sch.Due(now)
}

func (b *Base) Err() error {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.err
}

func (b *Base) UpdatedAt() time.Time {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.updatedAt
}

// done records the outcome of an update. Passing a non-nil error keeps whatever
// data the widget already had and shortens the next retry.
func (b *Base) done(err error) {
	now := time.Now()
	b.mu.Lock()
	defer b.mu.Unlock()
	b.err = err
	if err != nil {
		b.sch.Fail(now)
		return
	}
	b.updatedAt = now
	b.sch.Succeed(now)
}

// Init is the no-op default; widgets that need setup override it.
func (b *Base) Init() error { return nil }

// Page is one config-defined dashboard page.
type Page struct {
	Title   string
	Slug    string
	Columns []Column
}

type Column struct {
	Size    string
	Widgets []Widget
}

func (p *Page) allWidgets() []Widget {
	var out []Widget
	for _, c := range p.Columns {
		out = append(out, c.Widgets...)
	}
	return out
}

// Set is every config-driven page plus the scheduler that keeps them fresh.
type Set struct {
	Pages  []*Page
	bySlug map[string]*Page
}

// ConfigureShots tells every monitor widget where the screenshot job drops its
// files. Injected rather than configured per widget: one directory, one place to
// change it.
func (s *Set) ConfigureShots(dir string) {
	for _, m := range s.Monitors() {
		m.shotsDir = dir
	}
}

// Monitors is every monitor widget on every page. The screenshot endpoint needs
// them all: which services exist is a property of the config, not of one page.
func (s *Set) Monitors() []*Monitor {
	if s == nil {
		return nil
	}
	var out []*Monitor
	for _, p := range s.Pages {
		for _, c := range p.Columns {
			for _, w := range c.Widgets {
				if m, ok := w.(*Monitor); ok {
					out = append(out, m)
				}
			}
		}
	}
	return out
}

// ShotTargets is every reachable site across all monitors, deduplicated by slug
// — two widgets naming the same service must not mean two screenshot jobs
// fighting over one file.
func (s *Set) ShotTargets() []ShotTarget {
	seen := map[string]bool{}
	var out []ShotTarget
	for _, m := range s.Monitors() {
		for _, t := range m.ShotTargets() {
			if seen[t.Slug] {
				continue
			}
			seen[t.Slug] = true
			out = append(out, t)
		}
	}
	return out
}

func (s *Set) Page(slug string) *Page {
	if s == nil {
		return nil
	}
	return s.bySlug[slug]
}

// Build turns config pages into live widget trees. A widget that fails to
// initialise is reported but does not stop the rest of the dashboard.
func Build(pages []config.Page) (*Set, []string, error) {
	set := &Set{bySlug: map[string]*Page{}}
	var warnings []string
	nextID := 1

	for _, cp := range pages {
		if cp.Type != "widgets" {
			continue // issues and actions are built in, not widget-composed
		}
		p := &Page{Title: cp.Title, Slug: cp.Slug}

		for _, cc := range cp.Columns {
			col := Column{Size: cc.Size}
			if col.Size == "" {
				col.Size = "full"
			}
			for _, node := range cc.Widgets {
				w, err := buildWidget(node, nextID)
				if err != nil {
					warnings = append(warnings, fmt.Sprintf("page %q: %v", cp.Title, err))
					continue
				}
				nextID++
				col.Widgets = append(col.Widgets, w)
			}
			p.Columns = append(p.Columns, col)
		}

		set.Pages = append(set.Pages, p)
		set.bySlug[p.Slug] = p
	}
	return set, warnings, nil
}

func buildWidget(node yaml.Node, id int) (Widget, error) {
	var probe struct {
		Type string `yaml:"type"`
	}
	if err := node.Decode(&probe); err != nil {
		return nil, fmt.Errorf("reading widget type: %w", err)
	}

	var w Widget
	switch probe.Type {
	case "monitor":
		w = &Monitor{}
	case "bookmarks":
		w = &Bookmarks{}
	case "semaphore":
		w = &Semaphore{}
	case "ollama":
		w = &Ollama{}
	case "beads":
		w = &Beads{}
	case "iframe":
		w = &IFrame{}
	case "html":
		w = &HTML{}
	case "":
		return nil, fmt.Errorf("widget without a type")
	default:
		return nil, fmt.Errorf("unknown widget type %q", probe.Type)
	}

	if err := node.Decode(w); err != nil {
		return nil, fmt.Errorf("widget %q: %w", probe.Type, err)
	}

	b := w.base()
	b.id = id
	interval := b.Cache.Std()
	if interval <= 0 {
		interval = defaultInterval
	}
	b.sch = sched.New(interval)

	if err := w.Init(); err != nil {
		return nil, fmt.Errorf("widget %q: %w", probe.Type, err)
	}
	return w, nil
}

// tickInterval is how often the scheduler looks for due widgets.
const tickInterval = 10 * time.Second

// Start refreshes due widgets until ctx is cancelled.
func (s *Set) Start(ctx context.Context) {
	if s == nil || len(s.Pages) == 0 {
		return
	}
	s.updateDue(ctx, time.Now())

	t := time.NewTicker(tickInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-t.C:
			s.updateDue(ctx, now)
		}
	}
}

func (s *Set) updateDue(ctx context.Context, now time.Time) {
	var wg sync.WaitGroup
	for _, p := range s.Pages {
		for _, w := range p.allWidgets() {
			if !w.Due(now) {
				continue
			}
			wg.Add(1)
			go func(w Widget) {
				defer wg.Done()
				w.Update(ctx)
			}(w)
		}
	}
	wg.Wait()
}
