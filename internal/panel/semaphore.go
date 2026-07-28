package panel

import (
	"context"
	"fmt"
	"os"

	sema "github.com/niclasedge/git-planner-go/internal/semaphore"
)

// ---------------------------------------------------------------------------
// semaphore — Ansible Semaphore run history
// ---------------------------------------------------------------------------

// Semaphore shows the newest run of every template in one Semaphore project:
// failures first, with the lines of their log that explain them, then the rest.
type Semaphore struct {
	Base    `yaml:",inline"`
	URL     string `yaml:"url"`
	Project string `yaml:"project"`
	// User defaults to admin, matching Semaphore's own first account.
	User string `yaml:"user"`
	// PasswordEnv and TokenEnv name an environment variable. The credential
	// itself never goes into config.yaml, which is committed; it lives in .env.
	PasswordEnv string `yaml:"password-env"`
	TokenEnv    string `yaml:"token-env"`
	// Limit is how much task history to pull. The newest run per template has
	// to be in that window, so it needs to be comfortably larger than the
	// number of templates.
	Limit int `yaml:"limit"`

	client *sema.Client
	// credErr keeps the widget on the page when the credential is missing: a
	// banner naming the variable is more use than a widget that silently
	// disappeared with a startup warning.
	credErr error
	report  *sema.Report
}

func (s *Semaphore) Kind() string { return "semaphore" }

func (s *Semaphore) Init() error {
	if s.URL == "" {
		return fmt.Errorf("semaphore needs a url")
	}
	if s.Project == "" {
		return fmt.Errorf("semaphore needs a project")
	}
	if s.PasswordEnv == "" && s.TokenEnv == "" {
		return fmt.Errorf("semaphore needs password-env or token-env")
	}
	if s.WTitle == "" {
		s.WTitle = "Semaphore · " + s.Project
	}
	if s.User == "" {
		s.User = "admin"
	}
	if s.Limit <= 0 {
		s.Limit = 400
	}

	// The dotenv file is loaded before the widgets are built, so a variable
	// that is still empty here is genuinely missing.
	pass, token := os.Getenv(s.PasswordEnv), os.Getenv(s.TokenEnv)
	if pass == "" && token == "" {
		s.credErr = fmt.Errorf("%s ist leer — Passwort in .env eintragen", firstNonEmpty(s.PasswordEnv, s.TokenEnv))
		return nil
	}

	c, err := sema.New(s.URL, s.User, pass, token)
	if err != nil {
		return fmt.Errorf("semaphore: %w", err)
	}
	s.client = c
	return nil
}

func (s *Semaphore) Update(ctx context.Context) {
	if s.client == nil {
		s.done(s.credErr)
		return
	}
	rep, err := s.client.Report(ctx, s.Project, s.Limit)
	if err != nil {
		s.done(err)
		return
	}
	s.mu.Lock()
	s.report = rep
	s.mu.Unlock()
	s.done(nil)
}

// Report is what the template renders. Nil until the first successful update.
// The report is replaced whole and never mutated afterwards, so handing the
// pointer out under the read lock is enough.
func (s *Semaphore) Report() *sema.Report {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.report
}

// maxLogLines caps an expanded log. Ansible output runs into the thousands of
// lines, the end is the interesting part, and Semaphore's own UI has the rest.
const maxLogLines = 500

// LogView is one run's full output, for an expanded row.
type LogView struct {
	Template string
	TaskID   int
	Status   string
	URL      string
	Lines    []string
	// Dropped counts the lines cut from the front by maxLogLines.
	Dropped int
	Err     string
}

// Log fetches the output of a run that is on the page. Returns nil for a task
// the current report does not know — a stale page after a refresh, usually.
func (s *Semaphore) Log(ctx context.Context, taskID int) *LogView {
	rep := s.Report()
	if rep == nil || s.client == nil {
		return nil
	}
	row := rep.Row(taskID)
	if row == nil {
		return nil
	}

	v := &LogView{Template: row.Template, TaskID: taskID, Status: row.Status(), URL: row.URL()}
	lines, err := s.client.Output(ctx, rep.ProjectID, taskID)
	if err != nil {
		v.Err = err.Error()
		return v
	}
	if len(lines) > maxLogLines {
		v.Dropped = len(lines) - maxLogLines
		lines = lines[len(lines)-maxLogLines:]
	}
	v.Lines = lines
	return v
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}
