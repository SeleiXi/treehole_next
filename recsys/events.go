package recsys

import (
	"strconv"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/rs/zerolog/log"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"treehole_next/config"
	"treehole_next/models"
)

func currentUserID(c *fiber.Ctx) int {
	if c == nil {
		if config.Config.Mode == "dev" || config.Config.Mode == "test" {
			return 1
		}
		return 0
	}
	user, err := models.GetCurrLoginUser(c)
	if err != nil || user == nil {
		return 0
	}
	return user.ID
}

func validFeedEventType(eventType string) bool {
	switch eventType {
	case models.FeedEventImpression,
		models.FeedEventOpen,
		models.FeedEventClick,
		models.FeedEventReply,
		models.FeedEventFavorite,
		models.FeedEventSubscribe,
		models.FeedEventHide,
		models.FeedEventReport:
		return true
	default:
		return false
	}
}

func feedEventDedupKey(userID int, holeID int, eventType string, requestID string) *string {
	requestID = strings.TrimSpace(requestID)
	if userID == 0 || holeID == 0 || eventType == "" || requestID == "" {
		return nil
	}
	key := strconv.Itoa(userID) + ":" + eventType + ":" + requestID + ":" + strconv.Itoa(holeID)
	return &key
}

func LogEvent(c *fiber.Ctx, tx *gorm.DB, holeID int, eventType string, feedMode string, position int, requestID string) {
	if tx == nil {
		tx = models.DB
	}
	userID := currentUserID(c)
	if userID == 0 || holeID == 0 || !validFeedEventType(eventType) {
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
		RequestDedupKey: feedEventDedupKey(
			userID,
			holeID,
			eventType,
			requestID,
		),
		CreatedAt: time.Now(),
	}
	if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&event).Error; err != nil {
		log.Warn().Err(err).Int("hole_id", holeID).Str("event_type", eventType).Msg("could not write feed event")
	}
	models.LogSearchAction(tx, userID, holeID, eventType, requestID)
}

func logImpressions(tx *gorm.DB, c *fiber.Ctx, holes models.Holes, requestID string) {
	LogImpressions(tx, c, holes, ModeRecommend, requestID)
}

func LogImpressions(tx *gorm.DB, c *fiber.Ctx, holes models.Holes, feedMode string, requestID string) {
	if len(holes) == 0 {
		return
	}
	if tx == nil {
		tx = models.DB
	}
	userID := currentUserID(c)
	if userID == 0 {
		return
	}
	if feedMode == "" {
		feedMode = ModeClassic
	}
	holeIDs := make([]int, 0, len(holes))
	for _, hole := range holes {
		if hole.ID != 0 {
			holeIDs = append(holeIDs, hole.ID)
		}
	}
	if len(holeIDs) == 0 {
		return
	}

	seenInRequest := map[int]bool{}
	now := time.Now()
	events := make([]models.FeedEvent, 0, len(holes))
	for i, hole := range holes {
		if hole.ID == 0 || seenInRequest[hole.ID] {
			continue
		}
		seenInRequest[hole.ID] = true
		events = append(events, models.FeedEvent{
			UserID:          userID,
			HoleID:          hole.ID,
			EventType:       models.FeedEventImpression,
			FeedMode:        feedMode,
			Position:        i,
			RequestID:       requestID,
			RequestDedupKey: feedEventDedupKey(userID, hole.ID, models.FeedEventImpression, requestID),
			CreatedAt:       now,
		})
	}
	if len(events) == 0 {
		return
	}
	write := func() {
		if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&events).Error; err != nil {
			log.Warn().Err(err).Msg("could not write feed impressions")
		}
	}
	if config.Config.Mode == "production" && tx == models.DB {
		go write()
		return
	}
	write()
}
