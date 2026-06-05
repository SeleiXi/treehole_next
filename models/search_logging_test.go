package models

import (
	"strings"
	"testing"
	"time"

	"treehole_next/config"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/schema"
)

func TestLogSearchImpressionsHashesQueryAndDedupesRequest(t *testing.T) {
	oldLogging := config.Config.SearchEventLogging
	oldTestLogin := config.Config.EnableTestLogin
	oldTestUserID := config.Config.TestLoginUserID
	oldMode := config.Config.Mode
	config.Config.SearchEventLogging = true
	config.Config.EnableTestLogin = true
	config.Config.TestLoginUserID = 42
	config.Config.Mode = "production"
	t.Cleanup(func() {
		config.Config.SearchEventLogging = oldLogging
		config.Config.EnableTestLogin = oldTestLogin
		config.Config.TestLoginUserID = oldTestUserID
		config.Config.Mode = oldMode
	})

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{
		NamingStrategy: schema.NamingStrategy{SingularTable: true},
	})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&SearchEvent{}))

	items := []SearchResultLogItem{
		{FloorID: 10, HoleID: 1, BaseRank: 0},
		{FloorID: 20, HoleID: 2, BaseRank: 1},
	}
	LogSearchImpressions(db, nil, "测试 query", false, "db", "search-request-1", items)
	LogSearchImpressions(db, nil, "测试 query", false, "db", "search-request-1", items)

	var count int64
	require.NoError(t, db.Model(&SearchEvent{}).Count(&count).Error)
	assert.EqualValues(t, 2, count)

	var events []SearchEvent
	require.NoError(t, db.Order("position asc").Find(&events).Error)
	require.Len(t, events, 2)
	assert.Len(t, events[0].QueryHash, 64)
	assert.NotEqual(t, "测试 query", events[0].QueryHash)
	assert.Equal(t, 8, events[0].QueryLength)
	assert.Equal(t, 2, events[0].QueryTermCount)
	assert.NotNil(t, events[0].RequestDedupKey)
}

func TestLogSearchActionCopiesImpressionContextAndDedupes(t *testing.T) {
	oldLogging := config.Config.SearchEventLogging
	config.Config.SearchEventLogging = true
	t.Cleanup(func() {
		config.Config.SearchEventLogging = oldLogging
	})

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{
		NamingStrategy: schema.NamingStrategy{SingularTable: true},
	})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&SearchEvent{}))

	score := 3.25
	queryHash := strings.Repeat("a", 64)
	require.NoError(t, db.Create(&SearchEvent{
		UserID:         42,
		QueryHash:      queryHash,
		QueryLength:    4,
		QueryTermCount: 1,
		Accurate:       true,
		Source:         "elastic",
		FloorID:        10,
		HoleID:         1,
		EventType:      SearchEventImpression,
		Position:       2,
		BaseRank:       12,
		BaseScore:      &score,
		RequestID:      "search-request-1",
		CreatedAt:      time.Now(),
	}).Error)

	LogSearchAction(db, 42, 1, SearchEventOpen, "search-request-1")
	LogSearchAction(db, 42, 1, SearchEventOpen, "search-request-1")
	LogSearchAction(db, 42, 1, SearchEventOpen, "")

	var actions []SearchEvent
	require.NoError(t, db.Where("event_type = ?", SearchEventOpen).Find(&actions).Error)
	require.Len(t, actions, 1)
	assert.Equal(t, queryHash, actions[0].QueryHash)
	assert.Equal(t, 10, actions[0].FloorID)
	assert.Equal(t, 1, actions[0].HoleID)
	assert.Equal(t, 2, actions[0].Position)
	assert.Equal(t, 12, actions[0].BaseRank)
	require.NotNil(t, actions[0].BaseScore)
	assert.Equal(t, score, *actions[0].BaseScore)
	assert.NotNil(t, actions[0].RequestDedupKey)
}
