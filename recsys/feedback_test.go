package recsys

import (
	"net/http"
	"testing"
	"time"

	"treehole_next/config"
	"treehole_next/models"

	"github.com/gofiber/fiber/v2"
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

func TestCurrentUserIDReturnsZeroForNilContext(t *testing.T) {
	oldMode := config.Config.Mode
	config.Config.Mode = "production"
	t.Cleanup(func() {
		config.Config.Mode = oldMode
	})

	assert.Equal(t, 0, currentUserID(nil))
}

func TestCurrentUserIDReturnsTestUserForNilContextInTestMode(t *testing.T) {
	oldMode := config.Config.Mode
	config.Config.Mode = "test"
	t.Cleanup(func() {
		config.Config.Mode = oldMode
	})

	assert.Equal(t, 1, currentUserID(nil))
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
	var events []models.FeedEvent
	assert.NoError(t, db.Find(&events).Error)
	for _, event := range events {
		assert.NotNil(t, event.RequestDedupKey)
	}

	LogImpressions(db, nil, holes, ModeRecommend, "request-2")
	assert.NoError(t, db.Model(&models.FeedEvent{}).Count(&count).Error)
	assert.EqualValues(t, 4, count)
}

func TestLogEventDedupesRequestKey(t *testing.T) {
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

	LogEvent(nil, db, 1, models.FeedEventOpen, ModeRecommend, -1, "open-request-1")
	LogEvent(nil, db, 1, models.FeedEventOpen, ModeRecommend, -1, "open-request-1")

	var count int64
	assert.NoError(t, db.Model(&models.FeedEvent{}).Count(&count).Error)
	assert.EqualValues(t, 1, count)
}

func TestLogEventFallsBackToQueryRequestID(t *testing.T) {
	oldMode := config.Config.Mode
	config.Config.Mode = "test"
	t.Cleanup(func() {
		config.Config.Mode = oldMode
	})

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{
		NamingStrategy: schema.NamingStrategy{SingularTable: true},
	})
	assert.NoError(t, err)
	assert.NoError(t, db.AutoMigrate(&models.FeedEvent{}, &models.SearchEvent{}))

	app := fiber.New()
	app.Get("/event", func(c *fiber.Ctx) error {
		LogEvent(c, db, 1, models.FeedEventReport, ModeRecommend, -1, "")
		return c.SendStatus(fiber.StatusNoContent)
	})
	req, err := http.NewRequest(http.MethodGet, "/event?request_id=query-request-1", nil)
	assert.NoError(t, err)
	res, err := app.Test(req, -1)
	assert.NoError(t, err)
	assert.Equal(t, fiber.StatusNoContent, res.StatusCode)

	var event models.FeedEvent
	assert.NoError(t, db.Where("event_type = ?", models.FeedEventReport).Take(&event).Error)
	assert.Equal(t, "query-request-1", event.RequestID)
	assert.NotNil(t, event.RequestDedupKey)
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

func TestRecallCandidatesKeepsSoftFatiguedHoles(t *testing.T) {
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
		{ID: 3, CreatedAt: now.Add(-3 * time.Hour), UpdatedAt: now.Add(-3 * time.Hour), DivisionID: 1, UserID: 12},
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
		for i := 0; i < openSuppressionThreshold; i++ {
			assert.NoError(t, db.Create(&models.FeedEvent{
				UserID:    1,
				HoleID:    hole.ID,
				EventType: models.FeedEventOpen,
				CreatedAt: now,
			}).Error)
		}
	}

	ids, err := recallCandidates(db, nil, HomeFeedRequest{Size: 3, Now: now}, []int{1})

	assert.NoError(t, err)
	assert.ElementsMatch(t, []int{1, 2, 3}, ids)
}

func TestRecallCandidatesExcludesOnlyHardNegatives(t *testing.T) {
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
		{ID: 3, CreatedAt: now.Add(-3 * time.Hour), UpdatedAt: now.Add(-3 * time.Hour), DivisionID: 1, UserID: 12},
	}
	assert.NoError(t, db.Create(&holes).Error)
	assert.NoError(t, db.Create(&[]models.FeedEvent{
		{UserID: 1, HoleID: 1, EventType: models.FeedEventImpression, CreatedAt: now},
		{UserID: 1, HoleID: 1, EventType: models.FeedEventOpen, CreatedAt: now},
		{UserID: 1, HoleID: 2, EventType: models.FeedEventHide, CreatedAt: now},
		{UserID: 1, HoleID: 3, EventType: models.FeedEventReport, CreatedAt: now},
	}).Error)

	ids, err := recallCandidates(db, nil, HomeFeedRequest{Size: 3, Now: now}, []int{1})

	assert.NoError(t, err)
	assert.Contains(t, ids, 1)
	assert.NotContains(t, ids, 2)
	assert.NotContains(t, ids, 3)
}

func TestRecallHotUsesHoleFeatureScoreBeforeRuleFallback(t *testing.T) {
	oldMode := config.Config.Mode
	config.Config.Mode = "test"
	t.Cleanup(func() {
		config.Config.Mode = oldMode
	})

	db := newRankerTestDB(t)
	now := time.Now()
	assert.NoError(t, db.Create(&[]models.Hole{
		{ID: 1, CreatedAt: now.Add(-time.Hour), UpdatedAt: now.Add(-time.Hour), DivisionID: 1, UserID: 10, Reply: 20},
		{ID: 2, CreatedAt: now.Add(-2 * time.Hour), UpdatedAt: now.Add(-2 * time.Hour), DivisionID: 1, UserID: 11, Reply: 1},
	}).Error)
	assert.NoError(t, db.Create(&[]models.HoleFeature{
		{HoleID: 1, HotScore: 1, QualityScore: 1, UpdatedAt: now},
		{HoleID: 2, HotScore: 10, QualityScore: 1, UpdatedAt: now},
	}).Error)

	ids, err := recallHot(db, nil, HomeFeedRequest{Size: 2, Now: now}, []int{1}, nil, 1)

	assert.NoError(t, err)
	assert.Equal(t, []int{2}, ids)
}

func TestRecallHotUsesOnlyFreshFeatureScores(t *testing.T) {
	oldMode := config.Config.Mode
	config.Config.Mode = "test"
	t.Cleanup(func() {
		config.Config.Mode = oldMode
	})

	db := newRankerTestDB(t)
	now := time.Date(2026, 6, 6, 10, 0, 0, 0, time.UTC)
	assert.NoError(t, db.Create(&[]models.Hole{
		{ID: 1, CreatedAt: now.Add(-time.Hour), UpdatedAt: now.Add(-time.Hour), DivisionID: 1, UserID: 10, Reply: 20},
		{ID: 2, CreatedAt: now.Add(-2 * time.Hour), UpdatedAt: now.Add(-2 * time.Hour), DivisionID: 1, UserID: 11, Reply: 1},
		{ID: 3, CreatedAt: now.Add(-3 * time.Hour), UpdatedAt: now.Add(-3 * time.Hour), DivisionID: 1, UserID: 12, Reply: 1},
	}).Error)
	assert.NoError(t, db.Create(&[]models.HoleFeature{
		{HoleID: 2, HotScore: 100, QualityScore: 100, UpdatedAt: now.Add(time.Minute)},
		{HoleID: 3, HotScore: 90, QualityScore: 90, UpdatedAt: now.Add(-featureMaxAge - time.Minute)},
	}).Error)

	ids, err := recallHot(db, nil, HomeFeedRequest{Size: 3, Now: now}, []int{1}, nil, 1)

	assert.NoError(t, err)
	assert.Equal(t, []int{1}, ids)
}

func TestRecallHotFallsBackWhenFeaturesAreMissing(t *testing.T) {
	oldMode := config.Config.Mode
	config.Config.Mode = "test"
	t.Cleanup(func() {
		config.Config.Mode = oldMode
	})

	db := newRankerTestDB(t)
	now := time.Now()
	assert.NoError(t, db.Create(&[]models.Hole{
		{ID: 1, CreatedAt: now.Add(-time.Hour), UpdatedAt: now.Add(-time.Hour), DivisionID: 1, UserID: 10, Reply: 20},
		{ID: 2, CreatedAt: now.Add(-2 * time.Hour), UpdatedAt: now.Add(-2 * time.Hour), DivisionID: 1, UserID: 11, Reply: 1},
	}).Error)

	ids, err := recallHot(db, nil, HomeFeedRequest{Size: 2, Now: now}, []int{1}, nil, 1)

	assert.NoError(t, err)
	assert.Equal(t, []int{1}, ids)
}

func TestRecallQualityFillsFeatureShortfallWithRuleFallback(t *testing.T) {
	oldMode := config.Config.Mode
	config.Config.Mode = "test"
	t.Cleanup(func() {
		config.Config.Mode = oldMode
	})

	db := newRankerTestDB(t)
	now := time.Now()
	assert.NoError(t, db.Create(&[]models.Hole{
		{ID: 1, CreatedAt: now.Add(-time.Hour), UpdatedAt: now.Add(-time.Hour), DivisionID: 1, UserID: 10, FavoriteCount: 20},
		{ID: 2, CreatedAt: now.Add(-2 * time.Hour), UpdatedAt: now.Add(-2 * time.Hour), DivisionID: 1, UserID: 11, FavoriteCount: 1},
	}).Error)
	assert.NoError(t, db.Create(&models.HoleFeature{
		HoleID:       2,
		QualityScore: 10,
		UpdatedAt:    now,
	}).Error)

	ids, err := recallQuality(db, nil, HomeFeedRequest{Size: 2, Now: now}, []int{1}, nil, 2)

	assert.NoError(t, err)
	assert.Equal(t, []int{2, 1}, ids)
}

func TestRecallQualityIgnoresZeroFeatureScore(t *testing.T) {
	oldMode := config.Config.Mode
	config.Config.Mode = "test"
	t.Cleanup(func() {
		config.Config.Mode = oldMode
	})

	db := newRankerTestDB(t)
	now := time.Now()
	assert.NoError(t, db.Create(&[]models.Hole{
		{ID: 1, CreatedAt: now.Add(-time.Hour), UpdatedAt: now.Add(-time.Hour), DivisionID: 1, UserID: 10, FavoriteCount: 20},
		{ID: 2, CreatedAt: now.Add(-2 * time.Hour), UpdatedAt: now.Add(-2 * time.Hour), DivisionID: 1, UserID: 11, FavoriteCount: 1},
	}).Error)
	assert.NoError(t, db.Create(&models.HoleFeature{
		HoleID:       2,
		QualityScore: 0,
		UpdatedAt:    now,
	}).Error)

	ids, err := recallQuality(db, nil, HomeFeedRequest{Size: 2, Now: now}, []int{1}, nil, 1)

	assert.NoError(t, err)
	assert.Equal(t, []int{1}, ids)
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

	scored, err := rankCandidatesForSize(db, nil, []int{1, 2}, now, 2, true)

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
