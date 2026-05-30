package recsys

import (
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/opentreehole/go-common"
	"gorm.io/gorm"

	"treehole_next/models"
)

const (
	clickLookback      = 30 * 24 * time.Hour
	negativeLookback   = 90 * 24 * time.Hour
	impressionLookback = 7 * 24 * time.Hour
)

type userFeedback struct {
	userID      int
	clicked     map[int]bool
	negative    map[int]bool
	impressions map[int]int
	divisions   map[int]float64
	tags        map[int]float64
}

func loadUserFeedback(tx *gorm.DB, c *fiber.Ctx, holeIDs []int, now time.Time) userFeedback {
	feedback := userFeedback{
		clicked:     map[int]bool{},
		negative:    map[int]bool{},
		impressions: map[int]int{},
		divisions:   map[int]float64{},
		tags:        map[int]float64{},
	}
	userID, err := common.GetUserID(c)
	if err != nil || userID == 0 {
		return feedback
	}
	feedback.userID = userID

	query := tx.Model(&models.FeedEvent{}).Where("user_id = ?", userID)
	if len(holeIDs) != 0 {
		query = query.Where("hole_id IN ?", holeIDs)
	}

	var events []models.FeedEvent
	if err := query.
		Where("created_at >= ?", now.Add(-negativeLookback)).
		Find(&events).Error; err != nil {
		return feedback
	}
	for _, event := range events {
		switch event.EventType {
		case models.FeedEventClick:
			if event.CreatedAt.After(now.Add(-clickLookback)) {
				feedback.clicked[event.HoleID] = true
			}
		case models.FeedEventHide, models.FeedEventReport:
			feedback.negative[event.HoleID] = true
		case models.FeedEventImpression:
			if event.CreatedAt.After(now.Add(-impressionLookback)) {
				feedback.impressions[event.HoleID]++
			}
		}
	}
	loadAffinities(tx, &feedback, now)
	return feedback
}

func (feedback userFeedback) shouldSuppress(holeID int) bool {
	return feedback.clicked[holeID] || feedback.negative[holeID] || feedback.impressions[holeID] >= 3
}

func (feedback userFeedback) penalty(holeID int) float64 {
	if feedback.negative[holeID] {
		return 1_000_000
	}
	if feedback.clicked[holeID] {
		return 10_000
	}
	return float64(feedback.impressions[holeID]) * 1.75
}

func (feedback userFeedback) affinityScore(hole *models.Hole, tagIDs []int) float64 {
	score := feedback.divisions[hole.DivisionID]
	for _, tagID := range tagIDs {
		score += feedback.tags[tagID]
	}
	if score > 8 {
		return 8
	}
	return score
}

func loadAffinities(tx *gorm.DB, feedback *userFeedback, now time.Time) {
	type eventHole struct {
		HoleID     int
		EventType  string
		DivisionID int
	}
	var rows []eventHole
	err := tx.Table("feed_event").
		Select("feed_event.hole_id, feed_event.event_type, hole.division_id").
		Joins("JOIN hole ON hole.id = feed_event.hole_id").
		Where("feed_event.user_id = ?", feedback.userID).
		Where("feed_event.event_type IN ?", []string{
			models.FeedEventClick,
			models.FeedEventReply,
			models.FeedEventFavorite,
			models.FeedEventSubscribe,
		}).
		Where("feed_event.created_at >= ?", now.Add(-negativeLookback)).
		Limit(300).
		Find(&rows).Error
	if err != nil || len(rows) == 0 {
		return
	}

	holeWeights := map[int]float64{}
	for _, row := range rows {
		weight := positiveEventWeight(row.EventType)
		feedback.divisions[row.DivisionID] += weight
		holeWeights[row.HoleID] += weight
	}
	var tags []models.HoleTag
	if err := tx.Where("hole_id IN ?", mapKeys(holeWeights)).Find(&tags).Error; err != nil {
		return
	}
	for _, tag := range tags {
		feedback.tags[tag.TagID] += holeWeights[tag.HoleID] * 0.7
	}
}

func positiveEventWeight(eventType string) float64 {
	switch eventType {
	case models.FeedEventFavorite, models.FeedEventSubscribe:
		return 1.2
	case models.FeedEventReply:
		return 1.0
	case models.FeedEventClick:
		return 0.35
	default:
		return 0
	}
}

func mapKeys[T comparable, V any](m map[T]V) []T {
	keys := make([]T, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	return keys
}

func RecentSuppressedHoleIDs(tx *gorm.DB, c *fiber.Ctx, now time.Time) []int {
	feedback := loadUserFeedback(tx, c, nil, now)
	if feedback.userID == 0 {
		return nil
	}
	result := make([]int, 0, len(feedback.clicked)+len(feedback.negative))
	seen := map[int]bool{}
	for id := range feedback.clicked {
		seen[id] = true
		result = append(result, id)
	}
	for id := range feedback.negative {
		if !seen[id] {
			result = append(result, id)
		}
	}
	return result
}
