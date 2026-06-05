package recsys

import (
	"testing"
	"time"

	"treehole_next/config"
	"treehole_next/models"

	"github.com/stretchr/testify/assert"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/schema"
)

func TestHomeFeedCursorOnlyMissingSnapshotReturnsEmpty(t *testing.T) {
	oldMode := config.Config.Mode
	oldDB := models.DB
	config.Config.Mode = "test"
	t.Cleanup(func() {
		config.Config.Mode = oldMode
		models.DB = oldDB
	})

	db := newHomeFeedServiceTestDB(t)
	models.DB = db

	now := time.Now()
	cursorID := 1
	holes, err := GetHomeFeed(nil, HomeFeedRequest{
		Size:      2,
		CursorID:  &cursorID,
		RequestID: "missing-snapshot",
		Now:       now,
	})

	assert.NoError(t, err)
	assert.Empty(t, holes)
}

func TestHomeFeedMissingSnapshotFallsBackWithScoreCursor(t *testing.T) {
	oldMode := config.Config.Mode
	oldDB := models.DB
	config.Config.Mode = "test"
	t.Cleanup(func() {
		config.Config.Mode = oldMode
		models.DB = oldDB
	})

	db := newHomeFeedServiceTestDB(t)
	models.DB = db

	now := time.Now()
	cursorID := 999
	cursorScore := 999.0
	holes, err := GetHomeFeed(nil, HomeFeedRequest{
		Size:        2,
		CursorID:    &cursorID,
		CursorScore: &cursorScore,
		RequestID:   "missing-snapshot-score-cursor",
		Now:         now,
	})

	assert.NoError(t, err)
	assert.NotEmpty(t, holes)
	assert.LessOrEqual(t, len(holes), 2)
}

func newHomeFeedServiceTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{
		NamingStrategy: schema.NamingStrategy{SingularTable: true},
	})
	assert.NoError(t, err)
	assert.NoError(t, db.AutoMigrate(&models.Division{}, &models.FeedEvent{}, &models.HoleFeature{}, &models.HoleTag{}))
	assert.NoError(t, db.Exec(`
		CREATE TABLE hole (
			id INTEGER PRIMARY KEY,
			created_at DATETIME NOT NULL,
			updated_at DATETIME NOT NULL,
			deleted_at DATETIME,
			view INTEGER NOT NULL DEFAULT 0,
			reply INTEGER NOT NULL DEFAULT 0,
			hidden BOOLEAN NOT NULL DEFAULT 0,
			locked BOOLEAN NOT NULL DEFAULT 0,
			frozen BOOLEAN NOT NULL DEFAULT 0,
			good BOOLEAN NOT NULL DEFAULT 0,
			no_purge BOOLEAN NOT NULL DEFAULT 0,
			division_id INTEGER NOT NULL,
			user_id INTEGER NOT NULL,
			favorite_count INTEGER NOT NULL DEFAULT 0,
			subscription_count INTEGER NOT NULL DEFAULT 0
		)
	`).Error)

	now := time.Now()
	assert.NoError(t, db.Create(&models.Division{
		ID:             1,
		CreatedAt:      now,
		UpdatedAt:      now,
		Name:           "home",
		ShowInHomePage: true,
	}).Error)
	assert.NoError(t, db.Create(&[]models.Hole{
		{ID: 1, CreatedAt: now.Add(-time.Hour), UpdatedAt: now.Add(-time.Hour), DivisionID: 1, UserID: 10},
		{ID: 2, CreatedAt: now.Add(-2 * time.Hour), UpdatedAt: now.Add(-2 * time.Hour), DivisionID: 1, UserID: 11},
		{ID: 3, CreatedAt: now.Add(-3 * time.Hour), UpdatedAt: now.Add(-3 * time.Hour), DivisionID: 1, UserID: 12},
	}).Error)
	return db
}
