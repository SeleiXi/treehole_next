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

func feedbackUserID(c *fiber.Ctx) int {
	if c != nil {
		if userID, err := GetCurrUserID(c); err == nil && userID != 0 {
			return userID
		}
	}
	user, err := GetCurrLoginUser(c)
	if err != nil || user == nil {
		return 0
	}
	return user.ID
}

func LoadHoleFeedbackSuppression(tx *gorm.DB, c *fiber.Ctx, holeIDs []int, now time.Time) HoleFeedbackSuppression {
	result := HoleFeedbackSuppression{
		Hard: map[int]bool{},
		Soft: map[int]bool{},
	}
	userID := feedbackUserID(c)
	if userID == 0 {
		return result
	}
	if now.IsZero() {
		now = time.Now()
	}
	if tx == nil {
		tx = DB
	}

	query := tx.Model(&FeedEvent{}).Where("user_id = ?", userID)
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
