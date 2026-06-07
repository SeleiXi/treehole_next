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

func TestApplySearchFeedbackQuerySortDemotesOpenedAndFiltersHardNegatives(t *testing.T) {
	oldMode := config.Config.Mode
	config.Config.Mode = "test"
	t.Cleanup(func() {
		config.Config.Mode = oldMode
	})

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{
		NamingStrategy: schema.NamingStrategy{SingularTable: true},
	})
	assert.NoError(t, err)
	assert.NoError(t, db.AutoMigrate(&Floor{}, &FeedEvent{}))

	now := time.Now()
	assert.NoError(t, db.Create(&[]Floor{
		{ID: 10, HoleID: 1, Content: "needle fresh", UserID: 2, Anonyname: "a"},
		{ID: 20, HoleID: 2, Content: "needle reported", UserID: 2, Anonyname: "b"},
		{ID: 30, HoleID: 3, Content: "needle opened", UserID: 2, Anonyname: "c"},
	}).Error)
	assert.NoError(t, db.Create(&[]FeedEvent{
		{UserID: 1, HoleID: 2, EventType: FeedEventReport, CreatedAt: now},
		{UserID: 1, HoleID: 3, EventType: FeedEventOpen, CreatedAt: now},
	}).Error)

	var floors Floors
	err = applySearchFeedbackQuerySort(db.Model(&Floor{}), nil, now).
		Where("content LIKE ?", "%needle%").
		Order("id DESC").
		Find(&floors).Error
	assert.NoError(t, err)
	floors = applySearchFeedbackFatigue(db, nil, floors, 2, now)
	assert.Equal(t, []int{1, 3}, []int{floors[0].HoleID, floors[1].HoleID})
}

func TestHoleFeedbackSuppressionHardOnlyDropsSoftFatigue(t *testing.T) {
	suppression := HoleFeedbackSuppression{
		HardIDs: []int{1},
		SoftIDs: []int{2},
		Hard:    map[int]bool{1: true},
		Soft:    map[int]bool{2: true},
	}

	result := suppression.HardOnly()

	assert.Equal(t, []int{1}, result.HardIDs)
	assert.True(t, result.Hard[1])
	assert.Empty(t, result.SoftIDs)
	assert.Empty(t, result.Soft)
}
