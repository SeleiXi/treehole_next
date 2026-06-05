package models

import (
	"testing"
	"time"

	"treehole_next/config"

	"github.com/stretchr/testify/assert"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/schema"
)

func TestApplySearchFeedbackFatigueDemotesOpenedAndFiltersNegatives(t *testing.T) {
	oldMode := config.Config.Mode
	config.Config.Mode = "test"
	t.Cleanup(func() {
		config.Config.Mode = oldMode
	})

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{
		NamingStrategy: schema.NamingStrategy{SingularTable: true},
	})
	assert.NoError(t, err)
	assert.NoError(t, db.AutoMigrate(&FeedEvent{}))

	now := time.Now()
	assert.NoError(t, db.Create(&[]FeedEvent{
		{UserID: 1, HoleID: 1, EventType: FeedEventOpen, CreatedAt: now},
		{UserID: 1, HoleID: 3, EventType: FeedEventReport, CreatedAt: now},
	}).Error)

	floors := Floors{
		{ID: 10, HoleID: 1},
		{ID: 20, HoleID: 2},
		{ID: 30, HoleID: 3},
	}

	result := applySearchFeedbackFatigue(db, nil, floors, 2, now)

	assert.Equal(t, Floors{
		{ID: 20, HoleID: 2},
		{ID: 10, HoleID: 1},
	}, result)
}
