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

func TestFeedbackOpenIsSoftFatigue(t *testing.T) {
	feedback := userFeedback{
		opened:      map[int]int{1: 1},
		negative:    map[int]bool{},
		impressions: map[int]int{},
	}

	assert.False(t, feedback.shouldSuppress(1))
	assert.Positive(t, feedback.penalty(1))
}

func TestFeedbackSuppressesExplicitAndRepeatedExposure(t *testing.T) {
	feedback := userFeedback{
		opened:      map[int]int{1: openSuppressionThreshold},
		negative:    map[int]bool{2: true},
		impressions: map[int]int{3: impressionSuppressionThreshold},
	}

	assert.True(t, feedback.shouldSuppress(1))
	assert.True(t, feedback.shouldSuppress(2))
	assert.True(t, feedback.shouldSuppress(3))
}

func TestLogImpressionsDedupesRequest(t *testing.T) {
	oldMode := config.Config.Mode
	config.Config.Mode = "test"
	t.Cleanup(func() {
		config.Config.Mode = oldMode
	})

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{
		NamingStrategy: schema.NamingStrategy{SingularTable: true},
	})
	assert.NoError(t, err)
	assert.NoError(t, db.AutoMigrate(&models.FeedEvent{}))

	holes := models.Holes{&models.Hole{ID: 1}, &models.Hole{ID: 2}}
	LogImpressions(db, nil, holes, ModeRecommend, "request-1")
	LogImpressions(db, nil, holes, ModeRecommend, "request-1")

	var count int64
	assert.NoError(t, db.Model(&models.FeedEvent{}).Count(&count).Error)
	assert.EqualValues(t, 2, count)

	LogImpressions(db, nil, holes, ModeRecommend, "request-2")
	assert.NoError(t, db.Model(&models.FeedEvent{}).Count(&count).Error)
	assert.EqualValues(t, 4, count)
}

func TestRecentHardSuppressedOnlyReturnsNegativeFeedback(t *testing.T) {
	oldMode := config.Config.Mode
	config.Config.Mode = "test"
	t.Cleanup(func() {
		config.Config.Mode = oldMode
	})

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{
		NamingStrategy: schema.NamingStrategy{SingularTable: true},
	})
	assert.NoError(t, err)
	assert.NoError(t, db.AutoMigrate(&models.FeedEvent{}))

	now := time.Now()
	events := []models.FeedEvent{
		{UserID: 1, HoleID: 1, EventType: models.FeedEventImpression, CreatedAt: now},
		{UserID: 1, HoleID: 2, EventType: models.FeedEventOpen, CreatedAt: now},
		{UserID: 1, HoleID: 3, EventType: models.FeedEventHide, CreatedAt: now},
		{UserID: 1, HoleID: 4, EventType: models.FeedEventReport, CreatedAt: now},
	}
	assert.NoError(t, db.Create(&events).Error)

	ids := RecentHardSuppressedHoleIDs(db, nil, now)

	assert.ElementsMatch(t, []int{3, 4}, ids)
}

func TestRankCandidatesFallsBackToSoftSuppressedHoles(t *testing.T) {
	oldMode := config.Config.Mode
	config.Config.Mode = "test"
	t.Cleanup(func() {
		config.Config.Mode = oldMode
	})

	db := newRankerTestDB(t)
	now := time.Now()
	holes := []models.Hole{
		{ID: 1, CreatedAt: now.Add(-time.Hour), UpdatedAt: now.Add(-time.Hour), DivisionID: 1, UserID: 10},
		{ID: 2, CreatedAt: now.Add(-2 * time.Hour), UpdatedAt: now.Add(-2 * time.Hour), DivisionID: 1, UserID: 11},
	}
	assert.NoError(t, db.Create(&holes).Error)
	for _, hole := range holes {
		for i := 0; i < impressionSuppressionThreshold; i++ {
			assert.NoError(t, db.Create(&models.FeedEvent{
				UserID:    1,
				HoleID:    hole.ID,
				EventType: models.FeedEventImpression,
				CreatedAt: now,
			}).Error)
		}
	}

	scored, err := rankCandidatesForSize(db, nil, []int{1, 2}, now, 2)

	assert.NoError(t, err)
	assert.Len(t, scored, 2)
}

func newRankerTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{
		NamingStrategy: schema.NamingStrategy{SingularTable: true},
	})
	assert.NoError(t, err)
	assert.NoError(t, db.AutoMigrate(&models.FeedEvent{}, &models.HoleFeature{}, &models.HoleTag{}))
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
	return db
}
