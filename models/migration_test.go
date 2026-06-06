package models

import (
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
}
