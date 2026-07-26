// Package store is the local cache. It holds one thing that matters: the last
// response body for every (token, url) pair together with its ETag.
//
// That single table is what makes "only load when changed" work across
// restarts. On start the first request for each URL carries If-None-Match, the
// server answers 304, and we serve the stored body — a cold start costs
// essentially no rate limit. Everything else the app needs is derived from
// these bodies in memory, which keeps the schema small on purpose.
package store

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

type Store struct {
	db *sql.DB
}

// Entry is a cached HTTP response.
type Entry struct {
	ETag         string
	LastModified string
	Body         []byte
	FetchedAt    time.Time
}

const schema = `
CREATE TABLE IF NOT EXISTS http_cache (
    token_name    TEXT    NOT NULL,
    url           TEXT    NOT NULL,
    etag          TEXT    NOT NULL DEFAULT '',
    last_modified TEXT    NOT NULL DEFAULT '',
    body          BLOB    NOT NULL,
    fetched_at    INTEGER NOT NULL,
    PRIMARY KEY (token_name, url)
) WITHOUT ROWID;

CREATE TABLE IF NOT EXISTS meta (
    key   TEXT PRIMARY KEY,
    value TEXT NOT NULL
);
`

func Open(path string) (*Store, error) {
	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("creating %s: %w", dir, err)
		}
	}

	// WAL keeps the background refresher from blocking page renders.
	dsn := path + "?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=synchronous(NORMAL)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("opening %s: %w", path, err)
	}

	// modernc/sqlite tolerates concurrent use, but a single writer avoids
	// SQLITE_BUSY churn under the refresher's write bursts.
	db.SetMaxOpenConns(1)

	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("applying schema: %w", err)
	}

	return &Store{db: db}, nil
}

func (s *Store) Close() error { return s.db.Close() }

// Get returns the cached entry for a (token, url) pair. A missing entry is not
// an error — it just means the next request goes out unconditionally.
func (s *Store) Get(tokenName, url string) (*Entry, error) {
	var (
		e    Entry
		unix int64
	)
	err := s.db.QueryRow(
		`SELECT etag, last_modified, body, fetched_at FROM http_cache
		 WHERE token_name = ? AND url = ?`, tokenName, url,
	).Scan(&e.ETag, &e.LastModified, &e.Body, &unix)

	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	e.FetchedAt = time.Unix(unix, 0)
	return &e, nil
}

// Put stores a fresh response.
func (s *Store) Put(tokenName, url, etag, lastModified string, body []byte) error {
	_, err := s.db.Exec(
		`INSERT INTO http_cache (token_name, url, etag, last_modified, body, fetched_at)
		 VALUES (?, ?, ?, ?, ?, ?)
		 ON CONFLICT(token_name, url) DO UPDATE SET
		   etag = excluded.etag,
		   last_modified = excluded.last_modified,
		   body = excluded.body,
		   fetched_at = excluded.fetched_at`,
		tokenName, url, etag, lastModified, body, time.Now().Unix(),
	)
	return err
}

// Touch updates only the fetch timestamp, for when a 304 confirmed the body is
// still current. The body and ETag stay as they are.
func (s *Store) Touch(tokenName, url string) error {
	_, err := s.db.Exec(
		`UPDATE http_cache SET fetched_at = ? WHERE token_name = ? AND url = ?`,
		time.Now().Unix(), tokenName, url,
	)
	return err
}

func (s *Store) SetMeta(key, value string) error {
	_, err := s.db.Exec(
		`INSERT INTO meta (key, value) VALUES (?, ?)
		 ON CONFLICT(key) DO UPDATE SET value = excluded.value`, key, value)
	return err
}

func (s *Store) Meta(key string) (string, error) {
	var v string
	err := s.db.QueryRow(`SELECT value FROM meta WHERE key = ?`, key).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return v, err
}

// Stats reports cache size, for the status endpoint.
func (s *Store) Stats() (entries int, bytes int64, err error) {
	err = s.db.QueryRow(
		`SELECT COUNT(*), COALESCE(SUM(LENGTH(body)), 0) FROM http_cache`,
	).Scan(&entries, &bytes)
	return
}
