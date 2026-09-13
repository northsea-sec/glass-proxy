package debug

import (
	"database/sql"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"

	_ "modernc.org/sqlite"
	"proxy.local/app/internal/runtimepaths"
)

// DB wraps the unified glass_debug.db connection.
type DB struct {
	db   *sql.DB
	mu   sync.Mutex
	path string
}

// OpenDB opens (or creates) the debug database.
// Default path resolves from the active runtime lane/root.
func OpenDB(path string) (*DB, error) {
	if path == "" {
		path = runtimepaths.Current().DebugDBPath
	}

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("create db dir: %w", err)
	}

	db, err := sql.Open("sqlite", path+"?_journal_mode=WAL&_busy_timeout=5000&_synchronous=NORMAL")
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}

	// WAL mode for concurrent reads
	if _, err := db.Exec("PRAGMA journal_mode=WAL"); err != nil {
		log.Printf("[DEBUG-DB] WAL pragma failed: %v", err)
	}

	// Apply schema
	if _, err := db.Exec(schemaSQL); err != nil {
		db.Close()
		return nil, fmt.Errorf("apply schema: %w", err)
	}
	if err := migrateSchema(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate schema: %w", err)
	}

	log.Printf("[DEBUG-DB] Opened %s", path)
	return &DB{db: db, path: path}, nil
}

func migrateSchema(db *sql.DB) error {
	requestColumns := map[string]string{
		"request_lane":                "TEXT",
		"request_transport":           "TEXT",
		"glass_prefix_change_kind":    "TEXT",
		"glass_prefix_divergence":     "TEXT",
		"glass_prefix_system_changed": "INTEGER DEFAULT 0",
		"glass_prefix_tools_changed":  "INTEGER DEFAULT 0",
		"glass_prefix_anchor":         "INTEGER DEFAULT 0",
		"glass_prefix_prev_anchor":    "INTEGER DEFAULT 0",
		"glass_prefix_measured_msgs":  "INTEGER DEFAULT 0",
		"glass_compression_watermark": "INTEGER DEFAULT 0",
		"glass_prefix_hash":           "TEXT",
		"glass_active_prefix_count":   "INTEGER DEFAULT 0",
		"glass_eviction_detected":     "INTEGER DEFAULT 0",
		"glass_tail_change_kind":      "TEXT",
		"glass_tail_divergence":       "TEXT",
		"glass_tail_anchor":           "INTEGER DEFAULT 0",
		"glass_tail_prev_anchor":      "INTEGER DEFAULT 0",
		"glass_tail_measured_msgs":    "INTEGER DEFAULT 0",
		"glass_tail_tokens":           "INTEGER DEFAULT 0",
		"glass_tail_hash":             "TEXT",
	}
	for name, decl := range requestColumns {
		if err := ensureColumn(db, "requests", name, decl); err != nil {
			return err
		}
	}
	subagentColumns := map[string]string{
		"request_lane":      "TEXT",
		"request_transport": "TEXT",
	}
	for name, decl := range subagentColumns {
		if err := ensureColumn(db, "subagent_events", name, decl); err != nil {
			return err
		}
	}
	quotaColumns := map[string]string{
		"statusline_burn_pp_hr":     "REAL",
		"statusline_current_5h_pct": "REAL",
		"statusline_hours_left":     "REAL",
		"statusline_samples_used":   "INTEGER DEFAULT 0",
		"statusline_window_min":     "REAL",
		"statusline_reset_detected": "INTEGER DEFAULT 0",
	}
	for name, decl := range quotaColumns {
		if err := ensureColumn(db, "quota_snapshots", name, decl); err != nil {
			return err
		}
	}
	return nil
}

func ensureColumn(db *sql.DB, table, column, decl string) error {
	rows, err := db.Query(fmt.Sprintf("PRAGMA table_info(%s)", table))
	if err != nil {
		return fmt.Errorf("table info %s: %w", table, err)
	}
	defer rows.Close()

	for rows.Next() {
		var (
			cid        int
			name       string
			colType    string
			notNull    int
			defaultV   sql.NullString
			primaryKey int
		)
		if err := rows.Scan(&cid, &name, &colType, &notNull, &defaultV, &primaryKey); err != nil {
			return fmt.Errorf("scan table info %s: %w", table, err)
		}
		if name == column {
			return nil
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate table info %s: %w", table, err)
	}

	stmt := fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s %s", table, column, decl)
	if _, err := db.Exec(stmt); err != nil {
		return fmt.Errorf("add column %s.%s: %w", table, column, err)
	}
	return nil
}

// Close closes the database.
func (d *DB) Close() error {
	return d.db.Close()
}

// Raw returns the underlying *sql.DB for direct queries.
func (d *DB) Raw() *sql.DB {
	return d.db
}

// Path returns the database file path.
func (d *DB) Path() string {
	return d.path
}
