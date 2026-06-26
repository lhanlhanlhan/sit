package store

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	_ "modernc.org/sqlite"
)

// SQLiteStore is the SQLite-backed Store (WAL mode, single file).
type SQLiteStore struct {
	db *sql.DB
}

const schema = `
CREATE TABLE IF NOT EXISTS nodes (
  node_id      TEXT PRIMARY KEY,
  display_name TEXT NOT NULL DEFAULT '',
  os           TEXT NOT NULL DEFAULT '',
  arch         TEXT NOT NULL DEFAULT '',
  version      TEXT NOT NULL DEFAULT '',
  hostname     TEXT NOT NULL DEFAULT '',
  addrs_json   TEXT NOT NULL DEFAULT '[]',
  status       TEXT NOT NULL DEFAULT 'offline',
  mcp_enabled  INTEGER NOT NULL DEFAULT 0,
  last_seen    INTEGER NOT NULL DEFAULT 0,
  created_at   INTEGER NOT NULL DEFAULT 0
);
CREATE TABLE IF NOT EXISTS node_credentials (
  node_id     TEXT PRIMARY KEY,
  secret_hash TEXT NOT NULL,
  state       TEXT NOT NULL DEFAULT 'active',
  issued_at   INTEGER NOT NULL DEFAULT 0,
  revoked_at  INTEGER NOT NULL DEFAULT 0
);
CREATE TABLE IF NOT EXISTS enroll_tokens (
  token_hash TEXT PRIMARY KEY,
  state      TEXT NOT NULL DEFAULT 'unused',
  expires_at INTEGER NOT NULL DEFAULT 0,
  created_at INTEGER NOT NULL DEFAULT 0
);
CREATE TABLE IF NOT EXISTS tasks (
  task_id     TEXT PRIMARY KEY,
  node_id     TEXT NOT NULL,
  kind        TEXT NOT NULL,
  command     TEXT NOT NULL DEFAULT '',
  name        TEXT NOT NULL DEFAULT '',
  args_json   TEXT NOT NULL DEFAULT '',
  state       TEXT NOT NULL DEFAULT 'queued',
  created_at  INTEGER NOT NULL DEFAULT 0,
  sent_at     INTEGER NOT NULL DEFAULT 0,
  finished_at INTEGER NOT NULL DEFAULT 0,
  deadline    INTEGER NOT NULL DEFAULT 0,
  exit_code   INTEGER NOT NULL DEFAULT 0,
  stdout      TEXT NOT NULL DEFAULT '',
  stderr      TEXT NOT NULL DEFAULT '',
  truncated   INTEGER NOT NULL DEFAULT 0,
  duration_ms INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS idx_tasks_node ON tasks(node_id, created_at);
CREATE TABLE IF NOT EXISTS activities (
  id          INTEGER PRIMARY KEY AUTOINCREMENT,
  node_id     TEXT NOT NULL,
  type        TEXT NOT NULL,
  detail_json TEXT NOT NULL DEFAULT '',
  at          INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS idx_activities_node ON activities(node_id, at);
CREATE TABLE IF NOT EXISTS admins (
  username      TEXT PRIMARY KEY,
  password_hash TEXT NOT NULL,
  created_at    INTEGER NOT NULL DEFAULT 0
);
`

// OpenSQLite opens (creating if needed) a SQLite database at path with WAL mode
// and runs schema migrations.
func OpenSQLite(path string) (*SQLiteStore, error) {
	dsn := fmt.Sprintf("file:%s?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(on)", path)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1) // serialize writes; SQLite single-writer
	if _, err := db.ExecContext(context.Background(), schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("store: migrate: %w", err)
	}
	return &SQLiteStore{db: db}, nil
}

func (s *SQLiteStore) Close() error { return s.db.Close() }

func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}

// ---- Nodes ----

func (s *SQLiteStore) UpsertNode(ctx context.Context, n Node) error {
	_, err := s.db.ExecContext(ctx, `
INSERT INTO nodes (node_id, display_name, os, arch, version, hostname, addrs_json, status, mcp_enabled, last_seen, created_at)
VALUES (?,?,?,?,?,?,?,?,?,?,?)
ON CONFLICT(node_id) DO UPDATE SET
  os=excluded.os, arch=excluded.arch, version=excluded.version,
  hostname=excluded.hostname, addrs_json=excluded.addrs_json,
  status=excluded.status, last_seen=excluded.last_seen`,
		n.NodeID, n.DisplayName, n.OS, n.Arch, n.Version, n.Hostname, n.AddrsJSON,
		n.Status, b2i(n.MCPEnabled), n.LastSeen, n.CreatedAt)
	return err
}

func scanNode(row interface{ Scan(...any) error }) (Node, error) {
	var n Node
	var mcp int
	err := row.Scan(&n.NodeID, &n.DisplayName, &n.OS, &n.Arch, &n.Version, &n.Hostname,
		&n.AddrsJSON, &n.Status, &mcp, &n.LastSeen, &n.CreatedAt)
	n.MCPEnabled = mcp != 0
	return n, err
}

const nodeCols = `node_id, display_name, os, arch, version, hostname, addrs_json, status, mcp_enabled, last_seen, created_at`

func (s *SQLiteStore) GetNode(ctx context.Context, nodeID string) (Node, error) {
	row := s.db.QueryRowContext(ctx, `SELECT `+nodeCols+` FROM nodes WHERE node_id=?`, nodeID)
	n, err := scanNode(row)
	if err == sql.ErrNoRows {
		return Node{}, ErrNotFound
	}
	return n, err
}

func (s *SQLiteStore) ListNodes(ctx context.Context, statusFilter, q string) ([]Node, error) {
	query := `SELECT ` + nodeCols + ` FROM nodes WHERE 1=1`
	var args []any
	if statusFilter != "" {
		query += ` AND status=?`
		args = append(args, statusFilter)
	}
	if q != "" {
		query += ` AND (node_id LIKE ? OR display_name LIKE ?)`
		like := "%" + q + "%"
		args = append(args, like, like)
	}
	query += ` ORDER BY created_at DESC`
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Node
	for rows.Next() {
		n, err := scanNode(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

func (s *SQLiteStore) SetDisplayName(ctx context.Context, nodeID, name string) error {
	return s.affectOne(ctx, `UPDATE nodes SET display_name=? WHERE node_id=?`, name, nodeID)
}

func (s *SQLiteStore) SetNodeStatus(ctx context.Context, nodeID, status string, lastSeen int64) error {
	return s.affectOne(ctx, `UPDATE nodes SET status=?, last_seen=? WHERE node_id=?`, status, lastSeen, nodeID)
}

func (s *SQLiteStore) SetMCPEnabled(ctx context.Context, nodeID string, enabled bool) error {
	return s.affectOne(ctx, `UPDATE nodes SET mcp_enabled=? WHERE node_id=?`, b2i(enabled), nodeID)
}

func (s *SQLiteStore) DeleteNode(ctx context.Context, nodeID string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM nodes WHERE node_id=?`, nodeID)
	return err
}

// affectOne runs an update and maps zero affected rows to ErrNotFound.
func (s *SQLiteStore) affectOne(ctx context.Context, query string, args ...any) error {
	res, err := s.db.ExecContext(ctx, query, args...)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// ---- Credentials ----

func (s *SQLiteStore) PutCredential(ctx context.Context, c Credential) error {
	_, err := s.db.ExecContext(ctx, `
INSERT INTO node_credentials (node_id, secret_hash, state, issued_at, revoked_at)
VALUES (?,?,?,?,?)
ON CONFLICT(node_id) DO UPDATE SET
  secret_hash=excluded.secret_hash, state=excluded.state,
  issued_at=excluded.issued_at, revoked_at=excluded.revoked_at`,
		c.NodeID, c.SecretHash, c.State, c.IssuedAt, c.RevokedAt)
	return err
}

func (s *SQLiteStore) GetCredential(ctx context.Context, nodeID string) (Credential, error) {
	var c Credential
	err := s.db.QueryRowContext(ctx,
		`SELECT node_id, secret_hash, state, issued_at, revoked_at FROM node_credentials WHERE node_id=?`, nodeID).
		Scan(&c.NodeID, &c.SecretHash, &c.State, &c.IssuedAt, &c.RevokedAt)
	if err == sql.ErrNoRows {
		return Credential{}, ErrNotFound
	}
	return c, err
}

func (s *SQLiteStore) RevokeCredential(ctx context.Context, nodeID string, at int64) error {
	return s.affectOne(ctx, `UPDATE node_credentials SET state='revoked', revoked_at=? WHERE node_id=?`, at, nodeID)
}

// ---- Enroll tokens ----

func (s *SQLiteStore) PutEnrollToken(ctx context.Context, t EnrollToken) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO enroll_tokens (token_hash, state, expires_at, created_at) VALUES (?,?,?,?)`,
		t.TokenHash, t.State, t.ExpiresAt, t.CreatedAt)
	return err
}

func (s *SQLiteStore) ConsumeEnrollToken(ctx context.Context, tokenHash string, now int64) (bool, error) {
	res, err := s.db.ExecContext(ctx,
		`UPDATE enroll_tokens SET state='used' WHERE token_hash=? AND state='unused' AND expires_at>=?`,
		tokenHash, now)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n == 1, nil
}

// ---- Tasks ----

func (s *SQLiteStore) CreateTask(ctx context.Context, t Task) error {
	_, err := s.db.ExecContext(ctx, `
INSERT INTO tasks (task_id, node_id, kind, command, name, args_json, state, created_at, deadline)
VALUES (?,?,?,?,?,?,?,?,?)`,
		t.TaskID, t.NodeID, t.Kind, t.Command, t.Name, t.ArgsJSON, t.State, t.CreatedAt, t.Deadline)
	return err
}

const taskCols = `task_id, node_id, kind, command, name, args_json, state, created_at, sent_at, finished_at, deadline, exit_code, stdout, stderr, truncated, duration_ms`

func scanTask(row interface{ Scan(...any) error }) (Task, error) {
	var t Task
	var trunc int
	err := row.Scan(&t.TaskID, &t.NodeID, &t.Kind, &t.Command, &t.Name, &t.ArgsJSON,
		&t.State, &t.CreatedAt, &t.SentAt, &t.FinishedAt, &t.Deadline,
		&t.ExitCode, &t.Stdout, &t.Stderr, &trunc, &t.DurationMS)
	t.Truncated = trunc != 0
	return t, err
}

func (s *SQLiteStore) GetTask(ctx context.Context, taskID string) (Task, error) {
	row := s.db.QueryRowContext(ctx, `SELECT `+taskCols+` FROM tasks WHERE task_id=?`, taskID)
	t, err := scanTask(row)
	if err == sql.ErrNoRows {
		return Task{}, ErrNotFound
	}
	return t, err
}

func (s *SQLiteStore) ListTasks(ctx context.Context, nodeID, stateFilter string, limit int) ([]Task, error) {
	query := `SELECT ` + taskCols + ` FROM tasks WHERE node_id=?`
	args := []any{nodeID}
	if stateFilter != "" {
		query += ` AND state=?`
		args = append(args, stateFilter)
	}
	query += ` ORDER BY created_at DESC`
	if limit > 0 {
		query += ` LIMIT ?`
		args = append(args, limit)
	}
	return s.queryTasks(ctx, query, args...)
}

func (s *SQLiteStore) QueuedTasks(ctx context.Context, nodeID string) ([]Task, error) {
	return s.queryTasks(ctx,
		`SELECT `+taskCols+` FROM tasks WHERE node_id=? AND state='queued' ORDER BY created_at ASC`, nodeID)
}

func (s *SQLiteStore) queryTasks(ctx context.Context, query string, args ...any) ([]Task, error) {
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Task
	for rows.Next() {
		t, err := scanTask(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func (s *SQLiteStore) UpdateTaskState(ctx context.Context, taskID, state string, at int64) error {
	col := map[string]string{TaskSent: "sent_at"}[state]
	if col != "" {
		return s.affectOne(ctx, `UPDATE tasks SET state=?, `+col+`=? WHERE task_id=?`, state, at, taskID)
	}
	return s.affectOne(ctx, `UPDATE tasks SET state=? WHERE task_id=?`, state, taskID)
}

func (s *SQLiteStore) CompleteTask(ctx context.Context, t Task) error {
	return s.affectOne(ctx, `
UPDATE tasks SET state=?, finished_at=?, exit_code=?, stdout=?, stderr=?, truncated=?, duration_ms=?
WHERE task_id=?`,
		t.State, t.FinishedAt, t.ExitCode, t.Stdout, t.Stderr, b2i(t.Truncated), t.DurationMS, t.TaskID)
}

// ---- Activities ----

func (s *SQLiteStore) AppendActivity(ctx context.Context, a Activity) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO activities (node_id, type, detail_json, at) VALUES (?,?,?,?)`,
		a.NodeID, a.Type, a.DetailJSON, a.At)
	return err
}

func (s *SQLiteStore) ListActivities(ctx context.Context, nodeID string, limit int, before int64) ([]Activity, error) {
	query := `SELECT id, node_id, type, detail_json, at FROM activities WHERE node_id=?`
	args := []any{nodeID}
	if before > 0 {
		query += ` AND at < ?`
		args = append(args, before)
	}
	query += ` ORDER BY at DESC`
	if limit > 0 {
		query += ` LIMIT ?`
		args = append(args, limit)
	}
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Activity
	for rows.Next() {
		var a Activity
		if err := rows.Scan(&a.ID, &a.NodeID, &a.Type, &a.DetailJSON, &a.At); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// ---- Admins ----

func (s *SQLiteStore) PutAdmin(ctx context.Context, a Admin) error {
	_, err := s.db.ExecContext(ctx, `
INSERT INTO admins (username, password_hash, created_at) VALUES (?,?,?)
ON CONFLICT(username) DO UPDATE SET password_hash=excluded.password_hash`,
		a.Username, a.PasswordHash, a.CreatedAt)
	return err
}

func (s *SQLiteStore) GetAdmin(ctx context.Context, username string) (Admin, error) {
	var a Admin
	err := s.db.QueryRowContext(ctx,
		`SELECT username, password_hash, created_at FROM admins WHERE username=?`, username).
		Scan(&a.Username, &a.PasswordHash, &a.CreatedAt)
	if err == sql.ErrNoRows {
		return Admin{}, ErrNotFound
	}
	return a, err
}

// compile guard for unused import in some build configs
var _ = strings.TrimSpace

// Ensure SQLiteStore satisfies Store.
var _ Store = (*SQLiteStore)(nil)
