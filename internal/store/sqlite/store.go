package sqlite

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

type RunRecord struct {
	RunID         string `json:"runId"`
	CapsuleID     string `json:"capsuleId"`
	CapsulePath   string `json:"capsulePath"`
	AgentName     string `json:"agentName"`
	Status        string `json:"status"`
	Lifecycle     string `json:"lifecycle"`
	RuntimeTarget string `json:"runtimeTarget"`
	ContainerID   string `json:"containerId"`
	ExitCode      *int   `json:"exitCode,omitempty"`
	StartedAt     string `json:"startedAt"`
	EndedAt       string `json:"endedAt,omitempty"`
	LastError     string `json:"lastError,omitempty"`
}

func Open(stateDir string) (*Store, error) {
	if stateDir == "" {
		stateDir = ".metaclaw"
	}
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		return nil, err
	}
	dbPath := filepath.Join(stateDir, "state.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, err
	}
	s := &Store{db: db}
	if err := s.initSchema(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return s, nil
}

func (s *Store) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

func columnExists(db *sql.DB, table, col string) bool {
	rows, err := db.Query(fmt.Sprintf("PRAGMA table_info(%s)", table))
	if err != nil {
		return false
	}
	defer rows.Close()
	for rows.Next() {
		var cid int
		var name, ctype string
		var notnull, pk int
		var dflt_value interface{}
		rows.Scan(&cid, &name, &ctype, &notnull, &dflt_value, &pk)
		if name == col {
			return true
		}
	}
	return false
}

func (s *Store) initSchema() error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS capsules (
			capsule_id TEXT PRIMARY KEY,
			capsule_path TEXT NOT NULL,
			created_at TEXT NOT NULL
		);`,
		`CREATE TABLE IF NOT EXISTS runs (
			run_id TEXT PRIMARY KEY,
			capsule_id TEXT NOT NULL,
			capsule_path TEXT NOT NULL,
			agent_name TEXT NOT NULL DEFAULT '',
			status TEXT NOT NULL,
			lifecycle TEXT NOT NULL,
			runtime_target TEXT NOT NULL,
			container_id TEXT,
			exit_code INTEGER,
			started_at TEXT NOT NULL,
			ended_at TEXT,
			last_error TEXT,
			FOREIGN KEY(capsule_id) REFERENCES capsules(capsule_id)
		);`,
	}
	for _, stmt := range stmts {
		if _, err := s.db.Exec(stmt); err != nil {
			return err
		}
	}
	// Safe migration: add agent_name only if runs table exists and column is missing
	if columnExists(s.db, "runs", "agent_name") {
		return nil
	}
	_, err := s.db.Exec(`ALTER TABLE runs ADD COLUMN agent_name TEXT NOT NULL DEFAULT '';`)
	return err
}

func (s *Store) UpsertCapsule(capsuleID, capsulePath string) error {
	_, err := s.db.Exec(
		`INSERT INTO capsules (capsule_id, capsule_path, created_at)
		 VALUES (?, ?, ?)
		 ON CONFLICT(capsule_id) DO UPDATE SET capsule_path = excluded.capsule_path`,
		capsuleID, capsulePath, time.Now().UTC().Format(time.RFC3339Nano),
	)
	return err
}

func (s *Store) InsertRun(r RunRecord) error {
	_, err := s.db.Exec(
		`INSERT INTO runs (run_id, capsule_id, capsule_path, agent_name, status, lifecycle, runtime_target, container_id, exit_code, started_at, ended_at, last_error)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		r.RunID, r.CapsuleID, r.CapsulePath, r.AgentName, r.Status, r.Lifecycle, r.RuntimeTarget, nullableString(r.ContainerID), nullableInt(r.ExitCode),
		r.StartedAt, nullableString(r.EndedAt), nullableString(r.LastError),
	)
	return err
}

func (s *Store) UpdateRunStatus(runID, status, containerID, lastError string) error {
	_, err := s.db.Exec(
		`UPDATE runs SET status = ?, container_id = ?, last_error = ? WHERE run_id = ?`,
		status, nullableString(containerID), nullableString(lastError), runID,
	)
	return err
}

func (s *Store) UpdateRunCompletion(runID, status, containerID string, exitCode *int, lastError string) error {
	_, err := s.db.Exec(
		`UPDATE runs SET status = ?, container_id = ?, exit_code = ?, ended_at = ?, last_error = ? WHERE run_id = ?`,
		status, nullableString(containerID), nullableInt(exitCode), time.Now().UTC().Format(time.RFC3339Nano), nullableString(lastError), runID,
	)
	return err
}

func (s *Store) GetRun(runID string) (RunRecord, error) {
	row := s.db.QueryRow(`SELECT run_id, capsule_id, capsule_path, COALESCE(agent_name,''), status, lifecycle, runtime_target, COALESCE(container_id,''), exit_code, started_at, COALESCE(ended_at,''), COALESCE(last_error,'') FROM runs WHERE run_id = ?`, runID)
	var r RunRecord
	var exit sql.NullInt64
	if err := row.Scan(&r.RunID, &r.CapsuleID, &r.CapsulePath, &r.AgentName, &r.Status, &r.Lifecycle, &r.RuntimeTarget, &r.ContainerID, &exit, &r.StartedAt, &r.EndedAt, &r.LastError); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return RunRecord{}, fmt.Errorf("run not found: %s", runID)
		}
		return RunRecord{}, err
	}
	if exit.Valid {
		v := int(exit.Int64)
		r.ExitCode = &v
	}
	return r, nil
}

func (s *Store) ListRuns(limit int) ([]RunRecord, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := s.db.Query(`SELECT run_id, capsule_id, capsule_path, COALESCE(agent_name,''), status, lifecycle, runtime_target, COALESCE(container_id,''), exit_code, started_at, COALESCE(ended_at,''), COALESCE(last_error,'')
		FROM runs ORDER BY started_at DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]RunRecord, 0)
	for rows.Next() {
		var r RunRecord
		var exit sql.NullInt64
		if err := rows.Scan(&r.RunID, &r.CapsuleID, &r.CapsulePath, &r.AgentName, &r.Status, &r.Lifecycle, &r.RuntimeTarget, &r.ContainerID, &exit, &r.StartedAt, &r.EndedAt, &r.LastError); err != nil {
			return nil, err
		}
		if exit.Valid {
			v := int(exit.Int64)
			r.ExitCode = &v
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func nullableString(v string) any {
	if v == "" {
		return nil
	}
	return v
}

func nullableInt(v *int) any {
	if v == nil {
		return nil
	}
	return *v
}
