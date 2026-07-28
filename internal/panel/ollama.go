package panel

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/niclasedge/git-planner-go/internal/config"
)

// ---------------------------------------------------------------------------
// ollama — local model inventory
// ---------------------------------------------------------------------------

// Ollama answers the two questions `ollama list` and `ollama ps` answer: which
// models are on disk, and is one of them loaded right now. The two endpoints are
// merged, because a loaded model is not a separate thing — it is one of the
// listed ones with VRAM attached to it.
type Ollama struct {
	Base `yaml:",inline"`
	// URL is the Ollama root. Ollama binds loopback only, so a containerised
	// planner reaches it via host.docker.internal.
	URL string `yaml:"url"`
	// Timeout covers one endpoint. Loading a model can make the API slow to
	// answer, hence a default well above the monitor's.
	Timeout config.Duration `yaml:"timeout"`

	models  []OllamaModel
	version string
	// seen is when each currently loaded model was first observed loaded.
	// Ollama reports no load time — expires_at is pushed forward by every
	// request — so the only way to say how long something has been up is to
	// watch it. Dropped as soon as a model unloads, so a reload starts over.
	seen map[string]loadSpan
}

// OllamaModel is one model on disk, plus whatever /api/ps says about it.
type OllamaModel struct {
	Name     string
	Family   string
	Params   string
	Quant    string
	Size     int64 // on disk
	Modified time.Time

	// VRAM above zero means the model is loaded. Expires is when Ollama will
	// unload it again.
	VRAM    int64
	Expires time.Time
	Context int
	// Since is when this widget first saw the model loaded, and AtStartup marks
	// that this was the very first observation — the model may well have been
	// running for hours before that, so the age is a lower bound only.
	Since     time.Time
	AtStartup bool
}

// Loaded reports whether the model currently occupies memory.
func (m OllamaModel) Loaded() bool { return m.VRAM > 0 }

// UnloadAt is the clock time Ollama will drop the model, "18:55", empty when it
// is not loaded.
//
// Deliberately not a countdown: the data is a snapshot up to one refresh
// interval old, and Ollama's own keep_alive is five minutes — a computed
// "unloads in 4m" would be wrong about as often as it was right. An absolute
// time stays true however stale the snapshot is, and a time in the past is
// honest about the snapshot instead of hiding it.
func (m OllamaModel) UnloadAt() string {
	if m.Expires.IsZero() {
		return ""
	}
	return m.Expires.Local().Format("15:04")
}

// Age is how long the model has been loaded, as far as this widget can tell.
// Zero when unknown. Whether it is exact or a lower bound is AtStartup's job to
// say — the template renders "läuft seit" or "läuft seit mind." accordingly.
func (m OllamaModel) Age() time.Duration {
	if m.Since.IsZero() {
		return 0
	}
	if d := time.Since(m.Since); d > 0 {
		return d
	}
	return 0
}

// Tag is the part after the colon — "20b" of "gpt-oss:20b". Shown separately so
// a column of names stays readable.
func (m OllamaModel) Tag() string {
	if _, tag, ok := strings.Cut(m.Name, ":"); ok {
		return tag
	}
	return ""
}

// Base is the name without the tag.
func (m OllamaModel) BaseName() string {
	name, _, _ := strings.Cut(m.Name, ":")
	return name
}

func (o *Ollama) Kind() string { return "ollama" }

func (o *Ollama) Init() error {
	if o.URL == "" {
		return fmt.Errorf("ollama needs a url")
	}
	o.URL = strings.TrimRight(o.URL, "/")
	if o.WTitle == "" {
		o.WTitle = "Ollama"
	}
	if o.Timeout.Std() <= 0 {
		o.Timeout = config.Duration(10 * time.Second)
	}
	return nil
}

func (o *Ollama) Update(ctx context.Context) {
	tags, err := o.fetchTags(ctx)
	if err != nil {
		o.done(err)
		return
	}
	// A failing /api/ps must not cost the inventory: not knowing what is loaded
	// is worse than nothing, but far better than an empty widget.
	running, psErr := o.fetchRunning(ctx)
	version, _ := o.fetchVersion(ctx)

	o.mu.Lock()
	o.models = o.trackLoaded(mergeOllama(tags, running), time.Now())
	o.version = version
	o.mu.Unlock()
	o.done(psErr)
}

// Models is what the template renders: loaded models first, then the rest by
// most recently pulled.
func (o *Ollama) Models() []OllamaModel {
	o.mu.RLock()
	defer o.mu.RUnlock()
	return o.models
}

func (o *Ollama) Version() string {
	o.mu.RLock()
	defer o.mu.RUnlock()
	return o.version
}

// Active is the loaded models. Usually one, but Ollama will hold several.
func (o *Ollama) Active() []OllamaModel {
	var out []OllamaModel
	for _, m := range o.Models() {
		if m.Loaded() {
			out = append(out, m)
		}
	}
	return out
}

// Idle is the models on disk that are not loaded.
func (o *Ollama) Idle() []OllamaModel {
	var out []OllamaModel
	for _, m := range o.Models() {
		if !m.Loaded() {
			out = append(out, m)
		}
	}
	return out
}

// ---------------------------------------------------------------------------
// api
// ---------------------------------------------------------------------------

// ollamaModel covers both endpoints: /api/tags fills modified_at, /api/ps fills
// size_vram and expires_at. The overlap is deliberate on Ollama's side.
type ollamaModel struct {
	Name       string    `json:"name"`
	Size       int64     `json:"size"`
	SizeVRAM   int64     `json:"size_vram"`
	ModifiedAt time.Time `json:"modified_at"`
	ExpiresAt  time.Time `json:"expires_at"`
	Context    int       `json:"context_length"`
	Details    struct {
		Family        string `json:"family"`
		ParameterSize string `json:"parameter_size"`
		Quantization  string `json:"quantization_level"`
	} `json:"details"`
}

type ollamaList struct {
	Models []ollamaModel `json:"models"`
}

func (o *Ollama) fetchTags(ctx context.Context) ([]ollamaModel, error) {
	var out ollamaList
	if err := o.get(ctx, "/api/tags", &out); err != nil {
		return nil, err
	}
	return out.Models, nil
}

func (o *Ollama) fetchRunning(ctx context.Context) ([]ollamaModel, error) {
	var out ollamaList
	if err := o.get(ctx, "/api/ps", &out); err != nil {
		return nil, err
	}
	return out.Models, nil
}

func (o *Ollama) fetchVersion(ctx context.Context) (string, error) {
	var out struct {
		Version string `json:"version"`
	}
	if err := o.get(ctx, "/api/version", &out); err != nil {
		return "", err
	}
	return out.Version, nil
}

// maxOllamaBody caps a response. The model list of a well-stocked machine is a
// few kilobytes; anything past this is not an inventory.
const maxOllamaBody = 1 << 20

func (o *Ollama) get(ctx context.Context, path string, out any) error {
	ctx, cancel := context.WithTimeout(ctx, o.Timeout.Std())
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, o.URL+path, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "git-planner-go")

	resp, err := (&http.Client{Timeout: o.Timeout.Std()}).Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return fmt.Errorf("GET %s: %d %s", path, resp.StatusCode, http.StatusText(resp.StatusCode))
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxOllamaBody))
	if err != nil {
		return err
	}
	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("GET %s: %w", path, err)
	}
	return nil
}

// trackLoaded stamps every loaded model with the first time it was seen loaded,
// and forgets the ones that unloaded so a reload is not credited with the old
// model's age. Caller holds the write lock; now is a parameter so the test does
// not have to sleep.
func (o *Ollama) trackLoaded(models []OllamaModel, now time.Time) []OllamaModel {
	// A nil map means this is the first round: anything loaded now may have been
	// loaded long before the widget existed, and that stays true for as long as
	// it remains loaded — so the flag travels with the entry, it is not a
	// property of the round.
	firstRound := o.seen == nil

	next := make(map[string]loadSpan, len(models))
	for i := range models {
		if !models[i].Loaded() {
			continue
		}
		span, known := o.seen[models[i].Name]
		if !known {
			span = loadSpan{since: now, atStartup: firstRound}
		}
		next[models[i].Name] = span
		models[i].Since = span.since
		models[i].AtStartup = span.atStartup
	}
	o.seen = next
	return models
}

// loadSpan is when a model was first seen loaded, and whether that observation
// was the widget's first — in which case the age is a lower bound.
type loadSpan struct {
	since     time.Time
	atStartup bool
}

// mergeOllama joins the inventory with what is loaded and sorts it: loaded
// first, then most recently pulled. A model reported as running but absent from
// the inventory is still shown — that is Ollama serving something it no longer
// lists, which is exactly the sort of thing a dashboard should not hide.
func mergeOllama(tags, running []ollamaModel) []OllamaModel {
	loaded := make(map[string]ollamaModel, len(running))
	for _, r := range running {
		loaded[r.Name] = r
	}

	out := make([]OllamaModel, 0, len(tags)+len(running))
	seen := make(map[string]bool, len(tags))
	for _, t := range tags {
		m := OllamaModel{
			Name:     t.Name,
			Family:   t.Details.Family,
			Params:   t.Details.ParameterSize,
			Quant:    t.Details.Quantization,
			Size:     t.Size,
			Modified: t.ModifiedAt,
		}
		if r, ok := loaded[t.Name]; ok {
			m.VRAM, m.Expires, m.Context = r.SizeVRAM, r.ExpiresAt, r.Context
		}
		out = append(out, m)
		seen[t.Name] = true
	}
	for _, r := range running {
		if seen[r.Name] {
			continue
		}
		out = append(out, OllamaModel{
			Name:    r.Name,
			Family:  r.Details.Family,
			Params:  r.Details.ParameterSize,
			Quant:   r.Details.Quantization,
			Size:    r.Size,
			VRAM:    r.SizeVRAM,
			Expires: r.ExpiresAt,
			Context: r.Context,
		})
	}

	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Loaded() != out[j].Loaded() {
			return out[i].Loaded()
		}
		return out[i].Modified.After(out[j].Modified)
	})
	return out
}
