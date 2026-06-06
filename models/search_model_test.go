package models

import (
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"

	"treehole_next/config"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/schema"
)

func TestApplySearchModelRerankUsesModelWhenEnabled(t *testing.T) {
	oldDB := DB
	oldEnabled := config.Config.SearchModelRanking
	oldPath := config.Config.SearchModelPath
	t.Cleanup(func() {
		DB = oldDB
		config.Config.SearchModelRanking = oldEnabled
		config.Config.SearchModelPath = oldPath
	})

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{
		NamingStrategy: schema.NamingStrategy{SingularTable: true},
	})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&Hole{}, &HoleFeature{}, &HoleTag{}))
	DB = db

	now := time.Now()
	require.NoError(t, db.Create(&[]Hole{
		{ID: 1, CreatedAt: now.Add(-2 * time.Hour), UpdatedAt: now.Add(-2 * time.Hour), DivisionID: 1},
		{ID: 2, CreatedAt: now.Add(-time.Hour), UpdatedAt: now.Add(-time.Hour), DivisionID: 1},
	}).Error)

	path := filepath.Join(t.TempDir(), "search-model.json")
	require.NoError(t, os.WriteFile(path, []byte(`{
		"version": "test-search",
		"weights": {
			"base_rank": -0.1,
			"floor_like_log": 10
		}
	}`), 0o644))
	config.Config.SearchModelRanking = true
	config.Config.SearchModelPath = path

	floors := Floors{
		{ID: 1, HoleID: 1, CreatedAt: now.Add(-2 * time.Hour), Like: 0, Content: "needle"},
		{ID: 2, HoleID: 2, CreatedAt: now.Add(-time.Hour), Like: 100, Content: "needle"},
	}
	reranked := applySearchModelRerank(nil, "needle", floors, map[int]int{1: 0, 2: 1}, nil, false, "db", now)

	require.Len(t, reranked, 2)
	assert.Equal(t, 2, reranked[0].ID)
}

func TestApplySearchModelRerankReturnsOriginalWhenDisabled(t *testing.T) {
	oldEnabled := config.Config.SearchModelRanking
	t.Cleanup(func() {
		config.Config.SearchModelRanking = oldEnabled
	})
	config.Config.SearchModelRanking = false

	floors := Floors{{ID: 1}, {ID: 2}}
	reranked := applySearchModelRerank(nil, "needle", floors, nil, nil, false, "db", time.Now())

	assert.Equal(t, floors, reranked)
}

func TestApplySearchModelRerankIgnoresIncompatibleTask(t *testing.T) {
	oldDB := DB
	oldEnabled := config.Config.SearchModelRanking
	oldPath := config.Config.SearchModelPath
	t.Cleanup(func() {
		DB = oldDB
		config.Config.SearchModelRanking = oldEnabled
		config.Config.SearchModelPath = oldPath
	})

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{
		NamingStrategy: schema.NamingStrategy{SingularTable: true},
	})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&Hole{}, &HoleFeature{}, &HoleTag{}))
	DB = db

	now := time.Now()
	require.NoError(t, db.Create(&[]Hole{
		{ID: 1, CreatedAt: now.Add(-2 * time.Hour), UpdatedAt: now.Add(-2 * time.Hour), DivisionID: 1},
		{ID: 2, CreatedAt: now.Add(-time.Hour), UpdatedAt: now.Add(-time.Hour), DivisionID: 1},
	}).Error)

	path := filepath.Join(t.TempDir(), "home-model.json")
	require.NoError(t, os.WriteFile(path, []byte(`{
		"version": "test-home",
		"task": "home",
		"weights": {
			"floor_like_log": 10
		}
	}`), 0o644))
	config.Config.SearchModelRanking = true
	config.Config.SearchModelPath = path

	floors := Floors{
		{ID: 1, HoleID: 1, CreatedAt: now.Add(-2 * time.Hour), Like: 0, Content: "needle"},
		{ID: 2, HoleID: 2, CreatedAt: now.Add(-time.Hour), Like: 100, Content: "needle"},
	}
	reranked := applySearchModelRerank(nil, "needle", floors, map[int]int{1: 0, 2: 1}, nil, false, "db", now)

	assert.Equal(t, floors, reranked)
}

func TestSearchModelFeaturesIncludeAccurateAndSource(t *testing.T) {
	now := time.Now()
	floor := &Floor{ID: 1, HoleID: 1, CreatedAt: now.Add(-time.Hour), Content: "needle"}
	ctx := searchHoleContext{
		hole: Hole{ID: 1, CreatedAt: now.Add(-time.Hour), UpdatedAt: now.Add(-time.Hour)},
	}

	features := searchModelFeatures("needle", floor, ctx, 0, nil, true, "elastic", now)

	assert.Equal(t, 1.0, features["accurate"])
	assert.Equal(t, 1.0, features["source_elastic"])
	assert.Equal(t, 1.0, features["query_exact_substring"])

	features = searchModelFeatures("needle", floor, ctx, 0, nil, false, "db", now)
	assert.NotContains(t, features, "accurate")
	assert.NotContains(t, features, "source_elastic")
}

func TestSearchModelFeaturesIgnoreFutureFeatureSnapshot(t *testing.T) {
	now := time.Date(2026, 6, 6, 10, 0, 0, 0, time.UTC)
	floor := &Floor{
		ID:        1,
		HoleID:    1,
		CreatedAt: now.Add(-time.Hour),
		Content:   "needle",
	}
	ctx := searchHoleContext{
		hole: Hole{
			ID:        1,
			Reply:     3,
			View:      7,
			CreatedAt: now.Add(-2 * time.Hour),
			UpdatedAt: now.Add(-time.Hour),
		},
		feature: HoleFeature{
			UpdatedAt:    now.Add(time.Minute),
			HotScore:     20,
			QualityScore: 10,
			Reply24h:     100,
			View24h:      200,
		},
		hasFeature: true,
	}

	features := searchModelFeatures("needle", floor, ctx, 0, nil, false, "db", now)
	assert.NotContains(t, features, "feature_hot_score")
	assert.NotContains(t, features, "feature_reply_24h_log")

	ctx.feature.UpdatedAt = now.Add(-time.Minute)
	features = searchModelFeatures("needle", floor, ctx, 0, nil, false, "db", now)
	assert.Equal(t, ctx.feature.HotScore, features["feature_hot_score"])
	assert.Equal(t, math.Log1p(float64(ctx.feature.Reply24h)), features["feature_reply_24h_log"])
}
