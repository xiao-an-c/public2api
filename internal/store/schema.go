// Package store 是 public2api 的持久化层（SQLite）。
//
// 本文件只放**结构与迁移**，不含查询实现。迁移用最小 DB 接口而不是具体驱动，
// 这样本包零外部依赖、可离线编译；驱动选型（modernc.org/sqlite 纯 Go 无 cgo，
// 还是 mattn/go-sqlite3 需要 cgo）留到实现阶段再定，不阻塞骨架。
package store

import (
	"context"
	"database/sql"
	"fmt"
)

// DB 是迁移需要的最小能力。*sql.DB 天然满足它，测试里可以用假实现。
type DB interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	BeginTx(ctx context.Context, opts *sql.TxOptions) (*sql.Tx, error)
}

// migration 是一次结构变更。version 必须严格递增，且**发布后不可修改**——
// 改一条已发布的迁移，等于让新老数据库结构分叉。要改结构就加新的一条。
type migration struct {
	version int
	name    string
	ddl     string
}

// migrations 是全部结构变更，按 version 升序。
var migrations = []migration{
	{
		version: 1,
		name:    "initial",
		ddl: `
-- 渠道。静态注册为主（见 internal/channel 的 Catalog），落库是为了存渠道级
-- 配置与开关：某个渠道临时全挂时，运维要能把它从面板藏起来。
CREATE TABLE IF NOT EXISTS channels (
  id       TEXT PRIMARY KEY,          -- wbp-cn | wbp-global | grok
  name     TEXT NOT NULL,
  enabled  INTEGER NOT NULL DEFAULT 1,
  sort     INTEGER NOT NULL DEFAULT 0,
  config   TEXT                        -- JSON：端点、超时、渠道级策略
);

-- 账号。跨渠道统一一张表：渠道与账号类型是列，不是表名。
-- 这样「按渠道统计」「跨渠道选号」都是普通 WHERE，不需要 UNION 多张表。
CREATE TABLE IF NOT EXISTS accounts (
  id          INTEGER PRIMARY KEY,
  channel     TEXT NOT NULL,
  kind        TEXT NOT NULL,
  uid         TEXT NOT NULL,           -- 渠道内的稳定标识
  source_key  TEXT NOT NULL,           -- 凭据指纹，用于识别重复导入
  name        TEXT,
  email       TEXT,
  status      TEXT NOT NULL,           -- active | cooling | disabled | invalid
  credentials BLOB,                    -- AES-256-GCM 密文，明文只在内存
  quota       TEXT,                    -- JSON：额度快照
  health      TEXT,                    -- JSON：健康分 / 连败 / 降权
  created_at  INTEGER NOT NULL,
  updated_at  INTEGER NOT NULL,
  UNIQUE(channel, kind, source_key)
);
-- 选号热路径：按渠道 + 类型 + 状态取候选。
CREATE INDEX IF NOT EXISTS idx_accounts_pick ON accounts(channel, kind, status);

-- 渠道维护任务。由渠道能力声明决定该渠道该有哪些任务，
-- 没有 CapCheckin 的渠道就不会有 checkin 行。
CREATE TABLE IF NOT EXISTS maintenance_tasks (
  id       INTEGER PRIMARY KEY,
  channel  TEXT NOT NULL,
  kind     TEXT NOT NULL,              -- keepalive | checkin | tasks
  name     TEXT NOT NULL,
  cron     TEXT NOT NULL,
  enabled  INTEGER NOT NULL DEFAULT 1,
  config   TEXT                        -- JSON：任务参数（如旅游目的地列表）
);
CREATE INDEX IF NOT EXISTS idx_tasks_channel ON maintenance_tasks(channel, kind);

-- 任务执行记录。面板「渠道维护空间」里那一栏历史的来源。
CREATE TABLE IF NOT EXISTS maintenance_runs (
  id          INTEGER PRIMARY KEY,
  task_id     INTEGER NOT NULL,
  account_id  INTEGER,                 -- 任务可能不针对具体账号
  started_at  INTEGER NOT NULL,
  finished_at INTEGER,
  ok          INTEGER,
  detail      TEXT                     -- JSON：成功/失败细节
);
CREATE INDEX IF NOT EXISTS idx_runs_task ON maintenance_runs(task_id, started_at);

-- API Key。channel 是**渠道作用域**：非空时该 Key 只能调度到该渠道的账号，
-- 空串表示不限。这是把「渠道」从运维概念变成产品能力的那一个字段。
CREATE TABLE IF NOT EXISTS api_keys (
  id          INTEGER PRIMARY KEY,
  name        TEXT NOT NULL,
  key_hash    TEXT NOT NULL UNIQUE,    -- 只存哈希，不存明文
  channel     TEXT NOT NULL DEFAULT '',
  cost_limit  REAL,
  token_limit INTEGER,
  rpm_limit   INTEGER,
  expires_at  INTEGER,
  enabled     INTEGER NOT NULL DEFAULT 1,
  created_at  INTEGER NOT NULL
);

-- 请求账本。渠道维度统计的基础。
CREATE TABLE IF NOT EXISTS request_logs (
  id                INTEGER PRIMARY KEY,
  ts                INTEGER NOT NULL,
  channel           TEXT NOT NULL,
  account_id        INTEGER,
  api_key_id        INTEGER,
  model             TEXT,
  prompt_tokens     INTEGER,
  completion_tokens INTEGER,
  status            TEXT,
  latency_ms        INTEGER,
  error_code        TEXT
);
CREATE INDEX IF NOT EXISTS idx_logs_ts_channel ON request_logs(ts, channel);

-- 冷却。账号级与账号×模型级并存：一个账号可能整体可用、但某个模型还在限额。
-- model 为空串表示账号级，这正是主键的一部分，所以两级共用一张表。
CREATE TABLE IF NOT EXISTS cooldowns (
  channel    TEXT NOT NULL,
  account_id INTEGER NOT NULL,
  model      TEXT NOT NULL DEFAULT '',
  reason     TEXT,
  until_ts   INTEGER NOT NULL,
  PRIMARY KEY (channel, account_id, model)
);

-- 出口节点。只有声明了 CapEgress 的渠道会用到（目前是 Grok 三条链路）。
CREATE TABLE IF NOT EXISTS egress_nodes (
  id         INTEGER PRIMARY KEY,
  channel    TEXT NOT NULL,
  kind       TEXT NOT NULL,            -- warp | socks5 | flaresolverr | direct
  addr       TEXT,
  status     TEXT,
  last_ok_at INTEGER,
  config     TEXT
);
CREATE INDEX IF NOT EXISTS idx_egress_channel ON egress_nodes(channel, kind);

-- 键值设置。面板可改的全局项。
CREATE TABLE IF NOT EXISTS settings (
  key   TEXT PRIMARY KEY,
  value TEXT                           -- JSON
);
`,
	},
}

const migrationsTable = `
CREATE TABLE IF NOT EXISTS schema_migrations (
  version    INTEGER PRIMARY KEY,
  name       TEXT NOT NULL,
  applied_at INTEGER NOT NULL
)`

// Migrate 按顺序执行未跑过的迁移。
//
// 每条迁移跑在自己的事务里：中途失败整条回滚，不会留下半截结构。
// 可重复调用——已跑过的版本会被跳过。
func Migrate(ctx context.Context, db DB) error {
	if _, err := db.ExecContext(ctx, migrationsTable); err != nil {
		return fmt.Errorf("建迁移记录表: %w", err)
	}

	applied, err := appliedVersions(ctx, db)
	if err != nil {
		return err
	}

	for _, m := range migrations {
		if applied[m.version] {
			continue
		}
		if err := applyOne(ctx, db, m); err != nil {
			return err
		}
	}
	return nil
}

func applyOne(ctx context.Context, db DB, m migration) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("迁移 %d(%s) 开启事务: %w", m.version, m.name, err)
	}
	defer func() { _ = tx.Rollback() }() // 已提交时是 no-op

	if _, err := tx.ExecContext(ctx, m.ddl); err != nil {
		return fmt.Errorf("迁移 %d(%s) 执行失败: %w", m.version, m.name, err)
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO schema_migrations(version, name, applied_at) VALUES(?,?,strftime('%s','now'))`,
		m.version, m.name); err != nil {
		return fmt.Errorf("记录迁移 %d: %w", m.version, err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("迁移 %d(%s) 提交: %w", m.version, m.name, err)
	}
	return nil
}

func appliedVersions(ctx context.Context, db DB) (map[int]bool, error) {
	rows, err := db.QueryContext(ctx, `SELECT version FROM schema_migrations`)
	if err != nil {
		return nil, fmt.Errorf("读迁移记录: %w", err)
	}
	defer func() { _ = rows.Close() }()

	out := make(map[int]bool)
	for rows.Next() {
		var v int
		if err := rows.Scan(&v); err != nil {
			return nil, fmt.Errorf("扫描迁移版本: %w", err)
		}
		out[v] = true
	}
	return out, rows.Err()
}

// Version 返回当前库的结构版本（已应用的最大 version）。
func Version(ctx context.Context, db DB) (int, error) {
	rows, err := db.QueryContext(ctx, `SELECT COALESCE(MAX(version), 0) FROM schema_migrations`)
	if err != nil {
		return 0, fmt.Errorf("读结构版本: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var v int
	if rows.Next() {
		if err := rows.Scan(&v); err != nil {
			return 0, fmt.Errorf("扫描结构版本: %w", err)
		}
	}
	return v, rows.Err()
}

// LatestVersion 返回代码里定义的最新结构版本。
func LatestVersion() int {
	max := 0
	for _, m := range migrations {
		if m.version > max {
			max = m.version
		}
	}
	return max
}
