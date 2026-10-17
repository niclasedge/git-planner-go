package demo

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/niclasedge/git-planner-go/internal/reports"
)

// Start boots the demo: a fake GitHub API plus a throw-away workspace under dir
// (git repos with beads maps and evidence reports). It points the process
// environment at both and returns the directory to pass as --cwd. Requires git
// and bd on PATH; it never reads or writes the real HOME.
func Start(dir string) (cwd string, fake *FakeGitHub, err error) {
	home := filepath.Join(dir, "home")
	work := filepath.Join(dir, "work")
	for _, d := range []string{filepath.Join(home, ".claude"), work} {
		if err = os.MkdirAll(d, 0o755); err != nil {
			return "", nil, err
		}
	}
	gitconfig := "[user]\n\tname = Alex Example\n\temail = alex@example.com\n[init]\n\tdefaultBranch = main\n"
	if err = os.WriteFile(filepath.Join(home, ".gitconfig"), []byte(gitconfig), 0o644); err != nil {
		return "", nil, err
	}

	fake = NewFakeGitHub()
	env := map[string]string{
		"HOME":                home,
		"GIT_CONFIG_GLOBAL":   filepath.Join(home, ".gitconfig"),
		"BD_ACTOR":            "alex",
		"CC_KNOWLEDGE_ROOTS":  work,
		"CC_REPORTS_DIR":      filepath.Join(home, ".claude", "reports"),
		"GKP_GITHUB_API":      fake.URL(),
		"GITHUB_PAT_PERSONAL": Token,
		"GITHUB_PAT_WORK":     "",
		"GKP_WORK_OWNERS":     "",
	}
	for k, v := range env {
		if err = os.Setenv(k, v); err != nil {
			return "", nil, err
		}
	}

	shop := filepath.Join(work, "webshop")
	infra := filepath.Join(work, "infra")
	if err = seedShop(shop, dir); err != nil {
		return "", nil, fmt.Errorf("demo webshop: %w", err)
	}
	if err = seedInfra(infra); err != nil {
		return "", nil, fmt.Errorf("demo infra: %w", err)
	}
	return shop, fake, nil
}

func run(dir string, name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("%s %s: %v: %s", name, strings.Join(args, " "), err, strings.TrimSpace(errb.String()))
	}
	return strings.TrimSpace(out.String()), nil
}

func initRepo(dir, origin string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	for _, a := range [][]string{{"init", "-q", "-b", "main"}, {"remote", "add", "origin", origin}} {
		if _, err := run(dir, "git", a...); err != nil {
			return err
		}
	}
	return nil
}

func commit(dir, file, msg string) error {
	if err := os.WriteFile(filepath.Join(dir, file), []byte(msg+"\n"), 0o644); err != nil {
		return err
	}
	if _, err := run(dir, "git", "add", file); err != nil {
		return err
	}
	_, err := run(dir, "git", "commit", "-q", "-m", msg)
	return err
}

// bd runs a bd command and returns its trimmed stdout.
func bd(dir string, args ...string) (string, error) { return run(dir, "bd", args...) }

func seedShop(dir, tmp string) error {
	if err := initRepo(dir, "https://github.com/acme/webshop.git"); err != nil {
		return err
	}
	if err := commit(dir, "README.md", "Initial storefront"); err != nil {
		return err
	}
	if _, err := bd(dir, "init", "--prefix", "shop", "--quiet"); err != nil {
		return err
	}
	create := func(args ...string) (string, error) {
		return bd(dir, append([]string{"create", "--silent"}, args...)...)
	}

	checkout, err := create("Checkout redesign", "-t", "epic", "-l", "wayfinder:map", "-p", "1",
		"-d", "Replace the legacy checkout with hosted payment fields, guest checkout and a confirmation mail.")
	if err != nil {
		return err
	}
	audit, _ := create("Audit the current checkout funnel", "-t", "task", "--parent", checkout)
	hosted, _ := create("Hosted payment fields", "-t", "task", "--parent", checkout)
	guest, _ := create("Guest checkout flow", "-t", "task", "--parent", checkout)
	mail, _ := create("Order confirmation mail", "-t", "task", "--parent", checkout)
	search, err := create("Search performance", "-t", "epic", "-l", "wayfinder:map", "-p", "2",
		"-d", "Bring the p95 of the search page on mobile under one second.")
	if err != nil {
		return err
	}
	facet, _ := create("Bound the facet query", "-t", "task", "--parent", search)
	cache, _ := create("Cache popular queries", "-t", "task", "--parent", search)

	for _, d := range [][2]string{{guest, hosted}, {mail, hosted}, {cache, facet}} {
		if _, err := bd(dir, "dep", "add", d[0], d[1]); err != nil {
			return err
		}
	}
	if _, err := bd(dir, "update", hosted, "--claim"); err != nil {
		return err
	}
	if _, err := bd(dir, "update", facet, "--claim"); err != nil {
		return err
	}

	for _, c := range [][2]string{
		{"checkout.md", audit + ": funnel audit, drop-off at payment step"},
		{"payment.md", hosted + ": spike for hosted fields"},
		{"search.md", facet + ": limit facet buckets"},
	} {
		if err := commit(dir, c[0], c[1]); err != nil {
			return err
		}
	}

	shot := filepath.Join(tmp, "funnel.png")
	if err := writeMockPNG(shot); err != nil {
		return err
	}
	emit := func(p reports.Payload) error {
		p.Cwd = dir
		_, err := reports.Append(p)
		return err
	}
	if err := emit(reports.Payload{Map: checkout, Bead: audit, Kind: "plan", Title: "Measure funnel drop-off per step",
		Impact: "Without numbers the redesign is guesswork."}); err != nil {
		return err
	}
	if err := emit(reports.Payload{Map: checkout, Bead: audit, Kind: "done", Title: "Funnel audit finished: 41% leave at the payment step",
		Impact: "The payment form is the single biggest drop-off, so it goes first.",
		Proof: []reports.ProofItem{
			{"cmd": "python tools/funnel.py --days 30", "out": "cart 100%  address 82%  payment 41%  confirm 38%"},
			{"image": shot},
			{"file": "checkout.md:1"},
		}}); err != nil {
		return err
	}
	if _, err := bd(dir, "close", audit); err != nil {
		return err
	}
	if err := emit(reports.Payload{Map: checkout, Bead: hosted, Kind: "plan", Title: "Spike hosted fields behind a feature flag",
		Impact: "Card data stays off our servers; rollout is reversible."}); err != nil {
		return err
	}
	if err := emit(reports.Payload{Map: search, Bead: facet, Kind: "plan", Title: "Cap facet buckets at 20",
		Notes: "Unbounded aggregation is the likely cause of the 2.8 s p95."}); err != nil {
		return err
	}
	_ = mail
	return nil
}

func seedInfra(dir string) error {
	if err := initRepo(dir, "https://github.com/acme/infra.git"); err != nil {
		return err
	}
	if err := commit(dir, "README.md", "Initial infra"); err != nil {
		return err
	}
	if _, err := bd(dir, "init", "--prefix", "infra", "--quiet"); err != nil {
		return err
	}
	epic, err := bd(dir, "create", "--silent", "Observability baseline", "-t", "epic", "-l", "wayfinder:map")
	if err != nil {
		return err
	}
	for _, t := range []string{"Queue depth dashboard", "Alert on certificate expiry"} {
		if _, err := bd(dir, "create", "--silent", t, "-t", "task", "--parent", epic); err != nil {
			return err
		}
	}
	return nil
}

// writeMockPNG draws a small funnel bar chart used as evidence image.
func writeMockPNG(path string) error {
	img := image.NewRGBA(image.Rect(0, 0, 640, 280))
	bg := color.RGBA{248, 249, 251, 255}
	for y := 0; y < 280; y++ {
		for x := 0; x < 640; x++ {
			img.Set(x, y, bg)
		}
	}
	bars := []struct {
		pct int
		c   color.RGBA
	}{{100, color.RGBA{99, 130, 230, 255}}, {82, color.RGBA{99, 130, 230, 255}}, {41, color.RGBA{225, 90, 80, 255}}, {38, color.RGBA{99, 130, 230, 255}}}
	for i, b := range bars {
		x0 := 60 + i*140
		h := b.pct * 2
		for y := 250 - h; y < 250; y++ {
			for x := x0; x < x0+100; x++ {
				img.Set(x, y, b.c)
			}
		}
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return png.Encode(f, img)
}
