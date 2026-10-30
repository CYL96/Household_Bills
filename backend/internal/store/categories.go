package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// CategoryEntry is one category in a household snapshot. The JSON tags are
// part of both the persisted JSONB payload and the API response. Optional
// fields are omitted instead of emitted as null, matching the wire contract.
type CategoryEntry struct {
	Name          string   `json:"name"`
	Visible       bool     `json:"visible"`
	ParentName    string   `json:"parent_name,omitempty"`
	Icon          string   `json:"icon,omitempty"`
	DefaultName   string   `json:"default_name,omitempty"`
	PreviousNames []string `json:"previous_names,omitempty"`
}

// CategoryPreferences is the complete configuration for one kind. Array order
// is meaningful and is preserved in JSONB.
type CategoryPreferences struct {
	Entries         []CategoryEntry `json:"entries"`
	DeletedDefaults []string        `json:"deleted_defaults"`
}

// CategorySettings is the complete household snapshot returned by reads and
// writes. Revision is zero only when the household has never initialized its
// settings.
type CategorySettings struct {
	Revision int64
	Expense  CategoryPreferences
	Income   CategoryPreferences
}

// GetCategorySettings loads the household snapshot without creating one.
// initialized=false means the caller should materialize client defaults until
// the first save establishes a server revision.
func (s *Store) GetCategorySettings(
	ctx context.Context, householdID int64,
) (settings CategorySettings, initialized bool, err error) {
	var expenseJSON, incomeJSON []byte
	err = s.pool.QueryRow(ctx, `
		SELECT revision, expense, income
		FROM household_category_settings
		WHERE household_id = $1`,
		householdID).Scan(&settings.Revision, &expenseJSON, &incomeJSON)
	if errors.Is(err, pgx.ErrNoRows) {
		return CategorySettings{}, false, nil
	}
	if err != nil {
		return CategorySettings{}, false, fmt.Errorf("读取分类设置失败: %w", err)
	}
	if err := json.Unmarshal(expenseJSON, &settings.Expense); err != nil {
		return CategorySettings{}, false, fmt.Errorf("解析支出分类设置失败: %w", err)
	}
	if err := json.Unmarshal(incomeJSON, &settings.Income); err != nil {
		return CategorySettings{}, false, fmt.Errorf("解析收入分类设置失败: %w", err)
	}
	return settings, true, nil
}

// ReplaceCategorySettings atomically replaces both category kinds. A single
// INSERT or UPDATE is the unit of atomicity, so readers never observe one kind
// from the new snapshot and the other from the old one.
//
// base_revision=0 inserts the first snapshot. A competing first insert loses
// on the primary key and returns ErrCategoryConflict. Positive revisions use a
// conditional UPDATE: PostgreSQL serializes concurrent writers on the row and
// rechecks the revision after the lock, so exactly one competing save wins.
func (s *Store) ReplaceCategorySettings(
	ctx context.Context,
	householdID, baseRevision int64,
	expense, income CategoryPreferences,
) (CategorySettings, error) {
	if baseRevision < 0 {
		return CategorySettings{}, ErrCategoryConflict
	}
	expenseJSON, err := json.Marshal(expense)
	if err != nil {
		return CategorySettings{}, fmt.Errorf("编码支出分类设置失败: %w", err)
	}
	incomeJSON, err := json.Marshal(income)
	if err != nil {
		return CategorySettings{}, fmt.Errorf("编码收入分类设置失败: %w", err)
	}

	var revision int64
	if baseRevision == 0 {
		err = s.pool.QueryRow(ctx, `
			INSERT INTO household_category_settings
				(household_id, revision, expense, income)
			VALUES ($1, 1, $2, $3)
			RETURNING revision`,
			householdID, expenseJSON, incomeJSON).Scan(&revision)
		if isUniqueViolation(err, "household_category_settings_pkey") {
			return CategorySettings{}, ErrCategoryConflict
		}
	} else {
		err = s.pool.QueryRow(ctx, `
			UPDATE household_category_settings
			SET expense = $3,
			    income = $4,
			    revision = revision + 1,
			    updated_at = now()
			WHERE household_id = $1 AND revision = $2
			RETURNING revision`,
			householdID, baseRevision, expenseJSON, incomeJSON).Scan(&revision)
		if errors.Is(err, pgx.ErrNoRows) {
			return CategorySettings{}, ErrCategoryConflict
		}
	}
	if err != nil {
		return CategorySettings{}, fmt.Errorf("保存分类设置失败: %w", err)
	}
	return CategorySettings{
		Revision: revision,
		Expense:  expense,
		Income:   income,
	}, nil
}
