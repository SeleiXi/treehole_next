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

func TestHomeFeedSameRequestIDReusesFirstPageSnapshot(t *testing.T) {
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
	first, err := GetHomeFeed(nil, HomeFeedRequest{
		Size:      2,
		RequestID: "same-request",
		Now:       now,
	})
	assert.NoError(t, err)
	assert.Len(t, first, 2)

	second, err := GetHomeFeed(nil, HomeFeedRequest{
		Size:      2,
		RequestID: "same-request",
		Now:       now.Add(time.Second),
	})
	assert.NoError(t, err)
	assert.Equal(t, holeIDs(first), holeIDs(second))

	var count int64
	assert.NoError(t, db.Model(&models.FeedEvent{}).Where("request_id = ?", "same-request").Count(&count).Error)
	assert.EqualValues(t, len(first), count)
}

func TestHomeFeedSmallCorpusSurvivesSaturatedSoftFeedback(t *testing.T) {
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
	var holes []models.Hole
	assert.NoError(t, db.Find(&holes).Error)
	for _, hole := range holes {
		for i := 0; i < impressionSuppressionThreshold; i++ {
			assert.NoError(t, db.Create(&models.FeedEvent{
				UserID:    1,
				HoleID:    hole.ID,
				EventType: models.FeedEventImpression,
				CreatedAt: now,
			}).Error)
		}
		for i := 0; i < openSuppressionThreshold; i++ {
			assert.NoError(t, db.Create(&models.FeedEvent{
				UserID:    1,
				HoleID:    hole.ID,
				EventType: models.FeedEventOpen,
				CreatedAt: now,
			}).Error)
		}
	}

	feed, err := GetHomeFeed(nil, HomeFeedRequest{
		Size:      3,
		RequestID: "soft-saturated-small-corpus",
		Now:       now,
	})

	assert.NoError(t, err)
	assert.Len(t, feed, 3)
}

func holeIDs(holes models.Holes) []int {
	ids := make([]int, 0, len(holes))
	for _, hole := range holes {
		ids = append(ids, hole.ID)
	}
	return ids
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
