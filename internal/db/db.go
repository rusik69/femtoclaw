package db

import (
	"database/sql"
	"time"

	_ "modernc.org/sqlite"
)

// TrackedPR holds state for a PR we watch for new comments.
type TrackedPR struct {
	Owner       string
	Repo        string
	Number      int
	Title       string
	LastChecked time.Time
	ChatID      int64 // Telegram chat to notify when replying to PR comments (0 = skip)
}

type DB struct {
	conn *sql.DB
}

func Open(path string) (*DB, error) {
	conn, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	if err := conn.Ping(); err != nil {
		conn.Close()
		return nil, err
	}
	_, err = conn.Exec(`
		CREATE TABLE IF NOT EXISTS tracked_prs (
			key TEXT PRIMARY KEY,
			owner TEXT NOT NULL,
			repo TEXT NOT NULL,
			number INTEGER NOT NULL,
			title TEXT NOT NULL,
			last_checked DATETIME NOT NULL,
			chat_id INTEGER DEFAULT 0
		)
	`)
	if err != nil {
		conn.Close()
		return nil, err
	}
	// Migrate existing DBs: add chat_id if missing
	_, _ = conn.Exec(`ALTER TABLE tracked_prs ADD COLUMN chat_id INTEGER DEFAULT 0`)
	return &DB{conn: conn}, nil
}

func (d *DB) SaveTrackedPR(key string, pr *TrackedPR) error {
	_, err := d.conn.Exec(
		`INSERT OR REPLACE INTO tracked_prs (key, owner, repo, number, title, last_checked, chat_id) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		key, pr.Owner, pr.Repo, pr.Number, pr.Title, pr.LastChecked.Format(time.RFC3339), pr.ChatID,
	)
	return err
}

func (d *DB) ListTrackedPRs() ([]*TrackedPR, error) {
	rows, err := d.conn.Query(`SELECT owner, repo, number, title, last_checked, chat_id FROM tracked_prs`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*TrackedPR
	for rows.Next() {
		var pr TrackedPR
		var lastChecked string
		if err := rows.Scan(&pr.Owner, &pr.Repo, &pr.Number, &pr.Title, &lastChecked, &pr.ChatID); err != nil {
			return nil, err
		}
		pr.LastChecked, _ = time.Parse(time.RFC3339, lastChecked)
		out = append(out, &pr)
	}
	return out, rows.Err()
}

func (d *DB) UpdateLastChecked(key string, t time.Time) error {
	_, err := d.conn.Exec(`UPDATE tracked_prs SET last_checked = ? WHERE key = ?`, t.Format(time.RFC3339), key)
	return err
}

func (d *DB) Close() error {
	return d.conn.Close()
}
