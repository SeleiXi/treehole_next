package models

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/schema"
)

func TestMigrateRecsysTablesCreatesOperationalTables(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{
		NamingStrategy: schema.NamingStrategy{SingularTable: true},
	})
	require.NoError(t, err)

	require.NoError(t, MigrateRecsysTables(db))

	require.True(t, db.Migrator().HasTable(&HoleFeature{}))
	require.True(t, db.Migrator().HasTable(&FeedEvent{}))
	require.True(t, db.Migrator().HasTable(&SearchEvent{}))
	require.True(t, db.Migrator().HasIndex(&FeedEvent{}, "idx_feed_event_user_hole_type_created"))
	require.True(t, db.Migrator().HasIndex(&SearchEvent{}, "idx_search_event_req_user_query_floor_type_created"))
	require.True(t, db.Migrator().HasIndex(&SearchEvent{}, "idx_search_event_type_created"))
	require.True(t, db.Migrator().HasIndex(&SearchEvent{}, "idx_search_event_user_floor_type_created"))
}

func TestRecsysMySQLCreateStatementsUseStableCollation(t *testing.T) {
	for _, statement := range recsysMySQLCreateStatements() {
		require.Contains(t, strings.ToLower(statement), "default charset=utf8mb4 collate=utf8mb4_unicode_ci")
	}
}

func TestRecsysMySQLCreateStatementsIncludeTrainingIndexes(t *testing.T) {
	statements := strings.Join(recsysMySQLCreateStatements(), "\n")

	require.Contains(t, statements, "idx_feed_event_user_hole_type_created")
	require.Contains(t, statements, "idx_search_event_req_user_query_floor_type_created")
	require.Contains(t, statements, "idx_search_event_type_created")
	require.Contains(t, statements, "idx_search_event_user_floor_type_created")
}

func TestRecsysMySQLOperationalIndexesIncludeTrainingIndexes(t *testing.T) {
	indexes := recsysMySQLOperationalIndexes()
	names := make([]string, 0, len(indexes))
	for _, index := range indexes {
		names = append(names, index.name)
	}

	require.Contains(t, names, "idx_feed_event_user_hole_type_created")
	require.Contains(t, names, "idx_search_event_req_user_query_floor_type_created")
	require.Contains(t, names, "idx_search_event_type_created")
	require.Contains(t, names, "idx_search_event_user_floor_type_created")
}
