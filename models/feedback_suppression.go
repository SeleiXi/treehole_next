package models

import (
	"time"

	"github.com/gofiber/fiber/v2"
	"gorm.io/gorm"
)

const (
	feedbackOpenLookback     = 7 * 24 * time.Hour
	feedbackNegativeLookback = 90 * 24 * time.Hour
)

type HoleFeedbackSuppression struct {
	HardIDs []int
	SoftIDs []int
	Hard    map[int]bool
	Soft    map[int]bool
}

func LoadHoleFeedbackSuppression(tx *gorm.DB, c *fiber.Ctx, holeIDs []int, now time.Time) HoleFeedbackSuppression {
	result := HoleFeedbackSuppression{
		Hard: map[int]bool{},
		Soft: map[int]bool{},
	}
	user, err := GetCurrLoginUser(c)
	if err != nil || user == nil || user.ID == 0 {
		return result
	}
	if now.IsZero() {
		now = time.Now()
	}
	if tx == nil {
		tx = DB
	}

	query := tx.Model(&FeedEvent{}).Where("user_id = ?", user.ID)
	if len(holeIDs) != 0 {
		query = query.Where("hole_id IN ?", holeIDs)
	}

	var events []FeedEvent
	if err := query.
		Where("created_at >= ?", now.Add(-feedbackNegativeLookback)).
		Find(&events).Error; err != nil {
		return result
	}

	for _, event := range events {
		switch event.EventType {
		case FeedEventHide, FeedEventReport:
			if !result.Hard[event.HoleID] {
				result.Hard[event.HoleID] = true
				result.HardIDs = append(result.HardIDs, event.HoleID)
			}
		case FeedEventOpen, FeedEventClick:
			if event.CreatedAt.After(now.Add(-feedbackOpenLookback)) && !result.Soft[event.HoleID] {
				result.Soft[event.HoleID] = true
				result.SoftIDs = append(result.SoftIDs, event.HoleID)
			}
		}
	}
	return result
}
