package panel

import (
	"context"
	"crypto/tls"
	"fmt"
	"html/template"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/niclasedge/git-planner-go/internal/config"
)

// ---------------------------------------------------------------------------
// monitor — HTTP uptime checks
// ---------------------------------------------------------------------------

type Monitor struct {
	Base  `yaml:",inline"`
	Sites []*Site `yaml:"sites"`
	// FailuresOnly hides healthy sites, which is what you want once the list
	// gets long.
	FailuresOnly bool `yaml:"failures-only"`
}

type Site struct {
	Title string `yaml:"title"`
	URL   string `yaml:"url"`
	// CheckURL is probed instead of URL when the link target and the health
	// endpoint differ.
	CheckURL      string          `yaml:"check-url"`
	Icon          string          `yaml:"icon"`
	Timeout       config.Duration `yaml:"timeout"`
	AllowInsecure bool            `yaml:"allow-insecure"`

	// Results, written by Update and read by the template. The widget's lock
	// covers these.
	Status  int
	Latency time.Duration
	Error   string
}

func (s *Site) target() string {
	if s.CheckURL != "" {
		return s.CheckURL
	}
	return s.URL
}

func (s *Site) Up() bool {
	return s.Error == "" && s.Status >= 200 && s.Status < 400
}

// Outcome mirrors the vocabulary the Actions page uses, so both pages can share
// the same status colours.
func (s *Site) Outcome() string {
	switch {
	case s.Error != "":
		return "failure"
	case s.Status == 0:
		return "queued"
	case s.Up():
		return "success"
	default:
		return "failure"
	}
}

func (m *Monitor) Kind() string { return "monitor" }

func (m *Monitor) Init() error {
	if len(m.Sites) == 0 {
		return fmt.Errorf("monitor needs at least one site")
	}
	if m.WTitle == "" {
		m.WTitle = "Monitor"
	}
	for _, s := range m.Sites {
		if s.URL == "" && s.CheckURL == "" {
			return fmt.Errorf("site %q has no url", s.Title)
		}
		if s.Title == "" {
			s.Title = s.URL
		}
		if s.Timeout.Std() <= 0 {
			s.Timeout = config.Duration(5 * time.Second)
		}
	}
	return nil
}

func (m *Monitor) Update(ctx context.Context) {
	results := make([]struct {
		status  int
		latency time.Duration
		err     string
	}, len(m.Sites))

	var wg sync.WaitGroup
	for i, s := range m.Sites {
		wg.Add(1)
		go func(i int, s *Site) {
			defer wg.Done()
			status, latency, err := probe(ctx, s)
			results[i].status = status
			results[i].latency = latency
			if err != nil {
				results[i].err = err.Error()
			}
		}(i, s)
	}
	wg.Wait()

	m.mu.Lock()
	for i, r := range results {
		m.Sites[i].Status = r.status
		m.Sites[i].Latency = r.latency
		m.Sites[i].Error = r.err
	}
	m.mu.Unlock()

	// A monitor never reports an error upward: a site being down is the data,
	// not a failure of the widget.
	m.done(nil)
}

// insecureTransport is shared by every allow-insecure site, so we do not build a
// TLS config per check.
var (
	insecureOnce      sync.Once
	insecureTransport *http.Transport
)

func probe(ctx context.Context, s *Site) (int, time.Duration, error) {
	ctx, cancel := context.WithTimeout(ctx, s.Timeout.Std())
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.target(), nil)
	if err != nil {
		return 0, 0, err
	}
	req.Header.Set("User-Agent", "git-planner-go/monitor")

	client := &http.Client{Timeout: s.Timeout.Std()}
	if s.AllowInsecure {
		insecureOnce.Do(func() {
			insecureTransport = &http.Transport{
				TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
			}
		})
		client.Transport = insecureTransport
	}

	start := time.Now()
	resp, err := client.Do(req)
	if err != nil {
		return 0, time.Since(start), err
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<10))

	return resp.StatusCode, time.Since(start), nil
}

// ---------------------------------------------------------------------------
// bookmarks — static links
// ---------------------------------------------------------------------------

type Bookmarks struct {
	Base   `yaml:",inline"`
	Groups []BookmarkGroup `yaml:"groups"`
}

type BookmarkGroup struct {
	Title string     `yaml:"title"`
	Color string     `yaml:"color"`
	Links []Bookmark `yaml:"links"`
}

type Bookmark struct {
	Title       string `yaml:"title"`
	URL         string `yaml:"url"`
	Description string `yaml:"description"`
	Icon        string `yaml:"icon"`
}

func (b *Bookmarks) Kind() string { return "bookmarks" }

func (b *Bookmarks) Init() error {
	if len(b.Groups) == 0 {
		return fmt.Errorf("bookmarks needs at least one group")
	}
	if b.WTitle == "" {
		b.WTitle = "Bookmarks"
	}
	return nil
}

// Update is a no-op: static config, nothing to fetch. It still marks itself
// done so the scheduler stops looking at it.
func (b *Bookmarks) Update(context.Context) { b.done(nil) }

// ---------------------------------------------------------------------------
// iframe — embed anything
// ---------------------------------------------------------------------------

type IFrame struct {
	Base   `yaml:",inline"`
	Source string `yaml:"source"`
	Height int    `yaml:"height"`
}

func (f *IFrame) Kind() string { return "iframe" }

func (f *IFrame) Init() error {
	if f.Source == "" {
		return fmt.Errorf("iframe needs a source")
	}
	if f.Height <= 0 {
		f.Height = 400
	}
	return nil
}

func (f *IFrame) Update(context.Context) { f.done(nil) }

// ---------------------------------------------------------------------------
// html — raw markup from the config
// ---------------------------------------------------------------------------

type HTML struct {
	Base   `yaml:",inline"`
	Source string `yaml:"source"`
}

func (h *HTML) Kind() string { return "html" }

func (h *HTML) Init() error {
	if h.Source == "" {
		return fmt.Errorf("html widget needs a source")
	}
	return nil
}

func (h *HTML) Update(context.Context) { h.done(nil) }

// Content is injected unescaped. That is deliberate: config.yaml is a local file
// the operator wrote, so it is as trusted as the binary itself.
func (h *HTML) Content() template.HTML { return template.HTML(h.Source) }
