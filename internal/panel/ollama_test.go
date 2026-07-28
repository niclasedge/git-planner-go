package panel

import (
	"testing"
	"time"
)

func tagModel(name string, size int64, modified time.Time) ollamaModel {
	m := ollamaModel{Name: name, Size: size, ModifiedAt: modified}
	m.Details.ParameterSize = "20.9B"
	return m
}

// The loaded model is not a separate entry: it is the listed one with VRAM
// attached, and it sorts to the top.
func TestMergeOllama_LoadedFirstAndAnnotated(t *testing.T) {
	now := time.Now()
	tags := []ollamaModel{
		tagModel("newest:latest", 100, now),
		tagModel("loaded:20b", 200, now.Add(-48*time.Hour)),
		tagModel("older:7b", 300, now.Add(-24*time.Hour)),
	}
	running := []ollamaModel{{Name: "loaded:20b", SizeVRAM: 199, ExpiresAt: now.Add(5 * time.Minute), Context: 65536}}

	got := mergeOllama(tags, running)
	if len(got) != 3 {
		t.Fatalf("expected 3 models, got %d", len(got))
	}
	if got[0].Name != "loaded:20b" || !got[0].Loaded() {
		t.Fatalf("the loaded model must come first: %+v", got[0])
	}
	if got[0].VRAM != 199 || got[0].Context != 65536 {
		t.Fatalf("ps data not merged in: %+v", got[0])
	}
	if got[0].UnloadAt() != now.Add(5*time.Minute).Local().Format("15:04") {
		t.Fatalf("unload time wrong: %q", got[0].UnloadAt())
	}
	// Everything else stays in most-recently-pulled order.
	if got[1].Name != "newest:latest" || got[2].Name != "older:7b" {
		t.Fatalf("unloaded models out of order: %s, %s", got[1].Name, got[2].Name)
	}
	if got[1].Loaded() {
		t.Fatal("an unlisted-in-ps model must not look loaded")
	}
}

// Ollama serving something that is no longer in the inventory is exactly the
// kind of surprise a dashboard must not swallow.
func TestMergeOllama_RunningButNotListed(t *testing.T) {
	got := mergeOllama(
		[]ollamaModel{tagModel("on-disk:7b", 100, time.Now())},
		[]ollamaModel{{Name: "ghost:70b", SizeVRAM: 500}},
	)
	if len(got) != 2 {
		t.Fatalf("expected both models, got %d", len(got))
	}
	if got[0].Name != "ghost:70b" || !got[0].Loaded() {
		t.Fatalf("the ghost must be shown and first: %+v", got)
	}
}

func TestMergeOllama_NothingLoaded(t *testing.T) {
	got := mergeOllama([]ollamaModel{tagModel("a:7b", 100, time.Now())}, nil)
	if len(got) != 1 || got[0].Loaded() {
		t.Fatalf("nothing should look loaded: %+v", got)
	}
	if got[0].UnloadAt() != "" {
		t.Fatalf("no expiry means no unload time, got %q", got[0].UnloadAt())
	}
	if got[0].Age() != 0 {
		t.Fatal("an unloaded model has no age")
	}
}

// An expiry in the past is shown as-is: the snapshot is a few minutes old, and
// pretending otherwise would hide that.
func TestOllamaModel_UnloadAtPast(t *testing.T) {
	past := time.Now().Add(-time.Minute)
	m := OllamaModel{VRAM: 1, Expires: past}
	if got := m.UnloadAt(); got != past.Local().Format("15:04") {
		t.Fatalf("expected the past time, got %q", got)
	}
}

// The age of a loaded model comes from this widget's own observations, so the
// first round can only give a lower bound — and that stays a lower bound for as
// long as the model remains loaded.
func TestTrackLoaded_AgeAndLowerBound(t *testing.T) {
	o := &Ollama{}
	t0 := time.Now().Add(-30 * time.Minute)

	// Round one: already loaded when the widget started.
	got := o.trackLoaded([]OllamaModel{{Name: "a:7b", VRAM: 1}}, t0)
	if !got[0].AtStartup {
		t.Fatal("a model loaded at the first observation is a lower bound")
	}
	if got[0].Since != t0 {
		t.Fatalf("since wrong: %v", got[0].Since)
	}

	// Round two, ten minutes later: same load, so the original stamp and the
	// lower-bound flag both survive.
	got = o.trackLoaded([]OllamaModel{{Name: "a:7b", VRAM: 1}}, t0.Add(10*time.Minute))
	if got[0].Since != t0 || !got[0].AtStartup {
		t.Fatalf("stamp or flag lost on the second round: %+v", got[0])
	}
	if d := got[0].Age(); d < 29*time.Minute {
		t.Fatalf("age should be ~30m, got %v", d)
	}

	// A model that appears later was genuinely loaded in between, so its time is
	// not a lower bound.
	t1 := t0.Add(20 * time.Minute)
	got = o.trackLoaded([]OllamaModel{{Name: "a:7b", VRAM: 1}, {Name: "b:3b", VRAM: 1}}, t1)
	for _, m := range got {
		if m.Name == "b:3b" && (m.AtStartup || m.Since != t1) {
			t.Fatalf("a newly loaded model must be exact: %+v", m)
		}
	}
}

// Unloading forgets the stamp, so a reload does not inherit the old age.
func TestTrackLoaded_ForgetsUnloaded(t *testing.T) {
	o := &Ollama{}
	t0 := time.Now().Add(-time.Hour)
	o.trackLoaded([]OllamaModel{{Name: "a:7b", VRAM: 1}}, t0)
	o.trackLoaded([]OllamaModel{{Name: "a:7b"}}, t0.Add(time.Minute)) // unloaded

	t1 := t0.Add(2 * time.Minute)
	got := o.trackLoaded([]OllamaModel{{Name: "a:7b", VRAM: 1}}, t1)
	if got[0].Since != t1 {
		t.Fatalf("reload must start over, got %v", got[0].Since)
	}
	if got[0].AtStartup {
		t.Fatal("a reload we watched happen is not a lower bound")
	}
}

// An unloaded model carries no stamp at all.
func TestTrackLoaded_UnloadedUntouched(t *testing.T) {
	o := &Ollama{}
	got := o.trackLoaded([]OllamaModel{{Name: "a:7b"}}, time.Now())
	if !got[0].Since.IsZero() || got[0].AtStartup {
		t.Fatalf("an unloaded model must stay unstamped: %+v", got[0])
	}
}

func TestOllamaModel_NameSplit(t *testing.T) {
	m := OllamaModel{Name: "gpt-oss:20b"}
	if m.BaseName() != "gpt-oss" || m.Tag() != "20b" {
		t.Fatalf("split wrong: %q / %q", m.BaseName(), m.Tag())
	}
	// A name without a tag must not lose its name to the split.
	bare := OllamaModel{Name: "mymodel"}
	if bare.BaseName() != "mymodel" || bare.Tag() != "" {
		t.Fatalf("bare name mangled: %q / %q", bare.BaseName(), bare.Tag())
	}
}

func TestOllamaInit(t *testing.T) {
	if err := (&Ollama{}).Init(); err == nil {
		t.Fatal("a url is required")
	}
	o := &Ollama{URL: "http://127.0.0.1:11434/"}
	if err := o.Init(); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if o.URL != "http://127.0.0.1:11434" {
		t.Fatalf("trailing slash not trimmed: %q", o.URL)
	}
	if o.WTitle == "" || o.Timeout.Std() <= 0 {
		t.Fatalf("defaults missing: %+v", o)
	}
}
