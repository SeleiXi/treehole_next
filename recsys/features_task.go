package recsys

import (
	"context"
	"math"
	"time"

	"github.com/rs/zerolog/log"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"treehole_next/models"
)

type replyAggregate struct {
	HoleID   int
	Reply1h  int
	Reply6h  int
	Reply24h int
}

type viewAggregate struct {
	HoleID  int
	View1h  int
	View24h int
}

func RefreshFeatures(tx *gorm.DB, now time.Time) error {
	var holes []models.Hole
	if err := tx.Model(&models.Hole{}).Find(&holes).Error; err != nil {
		return err
	}

	replyAggs := map[int]replyAggregate{}
	var replies []replyAggregate
	if err := tx.Table("floor").
		Select(`
			hole_id,
			SUM(CASE WHEN created_at >= ? THEN 1 ELSE 0 END) AS reply1h,
			SUM(CASE WHEN created_at >= ? THEN 1 ELSE 0 END) AS reply6h,
			SUM(CASE WHEN created_at >= ? THEN 1 ELSE 0 END) AS reply24h
		`, now.Add(-time.Hour), now.Add(-6*time.Hour), now.Add(-24*time.Hour)).
		Where("ranking > 0").
		Group("hole_id").
		Find(&replies).Error; err != nil {
		return err
	}
	for _, item := range replies {
		replyAggs[item.HoleID] = item
	}

	viewAggs := map[int]viewAggregate{}
	var views []viewAggregate
	if err := tx.Model(&models.FeedEvent{}).
		Select(`
			hole_id,
			SUM(CASE WHEN created_at >= ? THEN 1 ELSE 0 END) AS view1h,
			SUM(CASE WHEN created_at >= ? THEN 1 ELSE 0 END) AS view24h
		`, now.Add(-time.Hour), now.Add(-24*time.Hour)).
		Where("event_type IN ?", []string{models.FeedEventImpression, models.FeedEventClick}).
		Group("hole_id").
		Find(&views).Error; err != nil {
		return err
	}
	for _, item := range views {
		viewAggs[item.HoleID] = item
	}

	features := make([]models.HoleFeature, 0, len(holes))
	for _, hole := range holes {
		replies := replyAggs[hole.ID]
		views := viewAggs[hole.ID]
		feature := models.HoleFeature{
			HoleID:            hole.ID,
			Reply1h:           replies.Reply1h,
			Reply6h:           replies.Reply6h,
			Reply24h:          replies.Reply24h,
			View1h:            views.View1h,
			View24h:           views.View24h,
			FavoriteCount:     hole.FavoriteCount,
			SubscriptionCount: hole.SubscriptionCount,
			HotScore:          hotFeatureScore(hole, replies, views, now),
			QualityScore:      qualityFeatureScore(hole),
			ControversyScore:  controversyFeatureScore(hole),
			UpdatedAt:         now,
		}
		features = append(features, feature)
	}
	if len(features) == 0 {
		return nil
	}
	return tx.Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "hole_id"}},
		DoUpdates: clause.AssignmentColumns([]string{
			"reply1h", "reply6h", "reply24h",
			"view1h", "view24h",
			"favorite_count", "subscription_count",
			"hot_score", "quality_score", "controversy_score", "updated_at",
		}),
	}).Create(&features).Error
}

func UpdateFeatures(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()
	refresh := func() {
		if err := RefreshFeatures(models.DB, time.Now()); err != nil {
			log.Warn().Err(err).Msg("refresh recsys features failed")
		}
	}
	refresh()
	for {
		select {
		case <-ticker.C:
			refresh()
		case <-ctx.Done():
			refresh()
			log.Info().Msg("task UpdateFeatures stopped...")
			return
		}
	}
}

func hotFeatureScore(hole models.Hole, replies replyAggregate, views viewAggregate, now time.Time) float64 {
	updateHours := math.Max(now.Sub(hole.UpdatedAt).Hours(), 0)
	recency := 24.0 / (24.0 + updateHours)
	return 2.8*math.Log1p(float64(replies.Reply1h)) +
		2.0*math.Log1p(float64(replies.Reply6h)) +
		1.2*math.Log1p(float64(replies.Reply24h)) +
		0.4*math.Log1p(float64(views.View1h)) +
		0.25*math.Log1p(float64(views.View24h)) +
		1.8*recency
}

func qualityFeatureScore(hole models.Hole) float64 {
	score := 1.6*math.Log1p(float64(hole.FavoriteCount)) +
		1.2*math.Log1p(float64(hole.SubscriptionCount))
	if hole.Good {
		score += 2
	}
	return score
}

func controversyFeatureScore(hole models.Hole) float64 {
	if hole.View == 0 {
		return 0
	}
	return math.Log1p(float64(hole.Reply)) / math.Log1p(float64(hole.View))
}
