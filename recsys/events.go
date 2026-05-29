package recsys

import (
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/opentreehole/go-common"
	"github.com/rs/zerolog/log"
	"gorm.io/gorm"

	"treehole_next/models"
)

func logImpressions(tx *gorm.DB, c *fiber.Ctx, holes models.Holes, requestID string) {
	if len(holes) == 0 {
		return
	}
	userID, err := common.GetUserID(c)
	if err != nil {
		userID = 0
	}
	now := time.Now()
	events := make([]models.FeedEvent, 0, len(holes))
	for i, hole := range holes {
		events = append(events, models.FeedEvent{
			UserID:    userID,
			HoleID:    hole.ID,
			EventType: EventImpression,
			FeedMode:  ModeRecommend,
			Position:  i,
			RequestID: requestID,
			CreatedAt: now,
		})
	}
	if err := tx.Create(&events).Error; err != nil {
		log.Warn().Err(err).Msg("could not write feed impressions")
	}
}
