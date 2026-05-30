package recsys

import (
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/rs/zerolog/log"
	"gorm.io/gorm"

	"treehole_next/models"
)

func currentUserID(c *fiber.Ctx) int {
	user, err := models.GetCurrLoginUser(c)
	if err != nil || user == nil {
		return 0
	}
	return user.ID
}

func LogEvent(c *fiber.Ctx, tx *gorm.DB, holeID int, eventType string, feedMode string, position int, requestID string) {
	if tx == nil {
		tx = models.DB
	}
	userID := currentUserID(c)
	if userID == 0 || holeID == 0 || eventType == "" {
		return
	}
	if feedMode == "" {
		feedMode = ModeClassic
	}
	event := models.FeedEvent{
		UserID:    userID,
		HoleID:    holeID,
		EventType: eventType,
		FeedMode:  feedMode,
		Position:  position,
		RequestID: requestID,
		CreatedAt: time.Now(),
	}
	if err := tx.Create(&event).Error; err != nil {
		log.Warn().Err(err).Int("hole_id", holeID).Str("event_type", eventType).Msg("could not write feed event")
	}
}

func logImpressions(tx *gorm.DB, c *fiber.Ctx, holes models.Holes, requestID string) {
	LogImpressions(tx, c, holes, ModeRecommend, requestID)
}

func LogImpressions(tx *gorm.DB, c *fiber.Ctx, holes models.Holes, feedMode string, requestID string) {
	if len(holes) == 0 {
		return
	}
	if feedMode == "" {
		feedMode = ModeClassic
	}
	userID := currentUserID(c)
	now := time.Now()
	events := make([]models.FeedEvent, 0, len(holes))
	for i, hole := range holes {
		events = append(events, models.FeedEvent{
			UserID:    userID,
			HoleID:    hole.ID,
			EventType: models.FeedEventImpression,
			FeedMode:  feedMode,
			Position:  i,
			RequestID: requestID,
			CreatedAt: now,
		})
	}
	if err := tx.Create(&events).Error; err != nil {
		log.Warn().Err(err).Msg("could not write feed impressions")
	}
}
