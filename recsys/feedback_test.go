package recsys

import (
	"testing"

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
