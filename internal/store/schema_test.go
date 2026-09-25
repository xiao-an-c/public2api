package store

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite" // 纯 Go 驱动，无 cgo
)

// newTestDB 建一个临时文件库。用文件而不是 :memory:，因为 database/sql 是连接池，
// 每条连接拿到的 :memory: 库是彼此独立的，迁移与断言可能落在不同连接上。
func newTestDB(t *testing.T) *sql.DB {
	t.Helper()
	path := filepath.Join(t.TempDir(), "test.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("打开测试库：%v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func TestMigrateCreatesSchema(t *testing.T) {
	ctx := context.Background()
	db := newTestDB(t)

	if err := Migrate(ctx, db); err != nil {
		t.Fatalf("迁移失败：%v", err)
	}

	got, err := Version(ctx, db)
	if err != nil {
		t.Fatalf("读版本：%v", err)
	}
	if got != LatestVersion() {
		t.Fatalf("结构版本 = %d，期望 %d", got, LatestVersion())
	}

	// 迁移该建出来的表，一张都不能少。
	want := []string{
		"accounts", "api_keys", "channels", "cooldowns",
		"egress_nodes", "maintenance_runs", "maintenance_tasks",
		"request_logs", "settings",
	}
	have := map[string]bool{}
	rows, err := db.QueryContext(ctx, `SELECT name FROM sqlite_master WHERE type='table'`)
	if err != nil {
		t.Fatalf("列举表：%v", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatal(err)
		}
		have[name] = true
	}
	for _, name := range want {
		if !have[name] {
			t.Errorf("缺少表 %s", name)
		}
	}
}

// TestMigrateIsIdempotent 钉住「可重复调用」这条约定：
// 每次启动都会跑迁移，已应用的版本必须被跳过而不是重放。
func TestMigrateIsIdempotent(t *testing.T) {
	ctx := context.Background()
	db := newTestDB(t)

	for i := 1; i <= 3; i++ {
		if err := Migrate(ctx, db); err != nil {
			t.Fatalf("第 %d 次迁移失败：%v", i, err)
		}
	}

	var count int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM schema_migrations`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != len(migrations) {
		t.Fatalf("迁移记录数 = %d，期望 %d（重复应用了）", count, len(migrations))
	}
}

// TestAccountDeduplicationKey 钉住账号去重键：同一个渠道下、同一种账号类型、
// 同一份凭据只能有一行。跨渠道或跨类型的相同凭据是**允许**的——
// 同一个人的 WorkBuddy 国内号与国际号是两份独立凭据，不该互相压制。
func TestAccountDeduplicationKey(t *testing.T) {
	ctx := context.Background()
	db := newTestDB(t)
	if err := Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}

	insert := func(channel, kind, sourceKey string) error {
		_, err := db.ExecContext(ctx, `
INSERT INTO accounts(channel, kind, uid, source_key, status, created_at, updated_at)
VALUES(?,?,?,?, 'active', 0, 0)`, channel, kind, "uid-1", sourceKey)
		return err
	}

	if err := insert("wbp-cn", "wbp", "key-a"); err != nil {
		t.Fatalf("首次插入应当成功：%v", err)
	}
	if err := insert("wbp-cn", "wbp", "key-a"); err == nil {
		t.Error("同一渠道+类型+凭据指纹重复插入应当被拒绝")
	}
	// 换渠道：同一份凭据指纹在另一个渠道下是另一个账号。
	if err := insert("wbp-global", "wbp", "key-a"); err != nil {
		t.Errorf("跨渠道的相同凭据指纹应当允许：%v", err)
	}
	// 换账号类型：同理。
	if err := insert("wbp-cn", "grok_web", "key-a"); err != nil {
		t.Errorf("跨账号类型的相同凭据指纹应当允许：%v", err)
	}
}

// TestCooldownTwoLevels 钉住冷却表的设计：账号级与账号×模型级共用一张表，
// 靠主键里的 model 区分（空串 = 账号级）。这样两级可以并存——
// 一个账号可能整体可用、但某个模型还在限额。
func TestCooldownTwoLevels(t *testing.T) {
	ctx := context.Background()
	db := newTestDB(t)
	if err := Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}

	insert := func(model, reason string) error {
		_, err := db.ExecContext(ctx, `
INSERT INTO cooldowns(channel, account_id, model, reason, until_ts)
VALUES('grok', 1, ?, ?, 100)`, model, reason)
		return err
	}

	if err := insert("", "账号级：整体限流"); err != nil {
		t.Fatalf("账号级冷却插入失败：%v", err)
	}
	if err := insert("grok-4", "模型级：该模型限额"); err != nil {
		t.Fatalf("模型级冷却插入失败：%v（两级应当并存）", err)
	}
	if err := insert("grok-4", "重复"); err == nil {
		t.Error("同一账号+模型的冷却重复插入应当被拒绝")
	}

	var count int
	if err := db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM cooldowns WHERE channel='grok' AND account_id=1`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Fatalf("冷却行数 = %d，期望 2（账号级 1 + 模型级 1）", count)
	}
}

// TestLatestVersionMatchesMigrations 防止 migrations 切片被改成非递增或跳号。
func TestLatestVersionMatchesMigrations(t *testing.T) {
	if len(migrations) == 0 {
		t.Fatal("迁移列表为空")
	}
	for i, m := range migrations {
		if m.version != i+1 {
			t.Errorf("第 %d 条迁移的 version = %d，期望 %d（必须从 1 起严格递增）",
				i, m.version, i+1)
		}
		if m.name == "" || m.ddl == "" {
			t.Errorf("迁移 %d 缺少 name 或 ddl", m.version)
		}
	}
}
