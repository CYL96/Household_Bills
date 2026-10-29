package store

import (
	"context"
	"embed"
	"fmt"
	"io/fs"
	"sort"
	"strconv"
	"strings"
)

// migrationLockID is a fixed application-wide advisory lock key. The lock is
// transaction-scoped, so PostgreSQL releases it automatically on commit or
// rollback even if the process crashes.
const migrationLockID int64 = 7_206_211_919_110

//go:embed migrations/*.sql
var migrationFS embed.FS

type migration struct {
	version int64
	name    string
	sql     string
}

// Migrate applies all pending embedded SQL migrations while holding a
// PostgreSQL advisory lock. Applying migrations and recording versions happen
// in one transaction so a failed migration leaves the schema untouched.
func (s *Store) Migrate(ctx context.Context) error {
	migrations, err := loadMigrations()
	if err != nil {
		return err
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("开启迁移事务失败: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock($1)", migrationLockID); err != nil {
		return fmt.Errorf("获取迁移锁失败: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version BIGINT PRIMARY KEY,
			applied_at TIMESTAMPTZ NOT NULL DEFAULT now()
		)`); err != nil {
		return fmt.Errorf("创建迁移记录表失败: %w", err)
	}

	rows, err := tx.Query(ctx, "SELECT version FROM schema_migrations")
	if err != nil {
		return fmt.Errorf("读取迁移记录失败: %w", err)
	}
	applied := make(map[int64]struct{})
	for rows.Next() {
		var version int64
		if err := rows.Scan(&version); err != nil {
			rows.Close()
			return fmt.Errorf("读取迁移版本失败: %w", err)
		}
		applied[version] = struct{}{}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return fmt.Errorf("读取迁移记录失败: %w", err)
	}

	for _, item := range migrations {
		if _, exists := applied[item.version]; exists {
			continue
		}
		// Embedded migration files are trusted, fixed SQL scripts and may
		// contain multiple statements. The simple query protocol executes the
		// script inside the surrounding pgx transaction.
		if _, err := tx.Conn().PgConn().Exec(ctx, item.sql).ReadAll(); err != nil {
			return fmt.Errorf("执行迁移 %s 失败: %w", item.name, err)
		}
		if _, err := tx.Exec(ctx,
			"INSERT INTO schema_migrations (version) VALUES ($1)", item.version); err != nil {
			return fmt.Errorf("记录迁移 %s 失败: %w", item.name, err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("提交迁移事务失败: %w", err)
	}
	return nil
}

func loadMigrations() ([]migration, error) {
	entries, err := fs.ReadDir(migrationFS, "migrations")
	if err != nil {
		return nil, fmt.Errorf("读取迁移目录失败: %w", err)
	}

	items := make([]migration, 0, len(entries))
	versions := make(map[int64]string)
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}
		parts := strings.SplitN(entry.Name(), "_", 2)
		if len(parts) != 2 {
			return nil, fmt.Errorf("迁移文件名无效: %s", entry.Name())
		}
		version, err := strconv.ParseInt(parts[0], 10, 64)
		if err != nil || version <= 0 {
			return nil, fmt.Errorf("迁移版本号无效: %s", entry.Name())
		}
		if previous, exists := versions[version]; exists {
			return nil, fmt.Errorf("迁移版本重复: %s 与 %s", previous, entry.Name())
		}
		versions[version] = entry.Name()

		body, err := migrationFS.ReadFile("migrations/" + entry.Name())
		if err != nil {
			return nil, fmt.Errorf("读取迁移 %s 失败: %w", entry.Name(), err)
		}
		items = append(items, migration{
			version: version,
			name:    entry.Name(),
			sql:     string(body),
		})
	}

	sort.Slice(items, func(i, j int) bool { return items[i].version < items[j].version })
	return items, nil
}
