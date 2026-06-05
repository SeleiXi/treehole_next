package models

import (
	"crypto/sha256"
	"encoding/hex"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"treehole_next/config"

	"github.com/gofiber/fiber/v2"
	"github.com/rs/zerolog/log"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const searchActionLookback = 2 * time.Hour

type SearchResultLogItem struct {
	FloorID   int
	HoleID    int
	BaseRank  int
	BaseScore *float64
}

func validSearchActionEventType(eventType string) bool {
	switch eventType {
	case SearchEventOpen,
		SearchEventClick,
		SearchEventReply,
		SearchEventFavorite,
		SearchEventSubscribe,
		SearchEventHide,
		SearchEventReport:
		return true
	default:
		return false
	}
}

func LogSearchImpressions(tx *gorm.DB, c *fiber.Ctx, keyword string, accurate bool, source string, requestID string, items []SearchResultLogItem) {
	if !config.Config.SearchEventLogging || len(items) == 0 {
		return
	}
	if tx == nil {
		tx = DB
	}
	userID := feedbackUserID(c)
	if userID == 0 {
		return
	}

	queryHash, queryLength, queryTermCount := searchQueryStats(keyword)
	if queryHash == "" {
		return
	}
	if source == "" {
		source = "unknown"
	}

	now := time.Now()
	seen := map[string]bool{}
	events := make([]SearchEvent, 0, len(items))
	for position, item := range items {
		if item.FloorID == 0 || item.HoleID == 0 {
			continue
		}
		key := searchEventDedupKey(userID, queryHash, item.FloorID, SearchEventImpression, requestID)
		keyValue := ""
		if key != nil {
			keyValue = *key
		} else {
			keyValue = strconv.Itoa(item.FloorID)
		}
		if seen[keyValue] {
			continue
		}
		seen[keyValue] = true
		events = append(events, SearchEvent{
			UserID:          userID,
			QueryHash:       queryHash,
			QueryLength:     queryLength,
			QueryTermCount:  queryTermCount,
			Accurate:        accurate,
			Source:          source,
			FloorID:         item.FloorID,
			HoleID:          item.HoleID,
			EventType:       SearchEventImpression,
			Position:        position,
			BaseRank:        item.BaseRank,
			BaseScore:       item.BaseScore,
			RequestID:       requestID,
			RequestDedupKey: key,
			CreatedAt:       now,
		})
	}
	if len(events) == 0 {
		return
	}
	if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&events).Error; err != nil {
		log.Warn().Err(err).Msg("could not write search impressions")
	}
}

func LogSearchAction(tx *gorm.DB, userID int, holeID int, eventType string, requestID string) {
	if !config.Config.SearchEventLogging || userID == 0 || holeID == 0 || !validSearchActionEventType(eventType) {
		return
	}
	requestID = strings.TrimSpace(requestID)
	if requestID == "" {
		return
	}
	if tx == nil {
		tx = DB
	}

	now := time.Now()
	var impressions []SearchEvent
	if err := tx.Model(&SearchEvent{}).
		Where("user_id = ?", userID).
		Where("hole_id = ?", holeID).
		Where("request_id = ?", requestID).
		Where("event_type = ?", SearchEventImpression).
		Where("created_at >= ?", now.Add(-searchActionLookback)).
		Order("position ASC, id ASC").
		Find(&impressions).Error; err != nil {
		log.Warn().Err(err).Str("request_id", requestID).Int("hole_id", holeID).Msg("could not load search impressions for action")
		return
	}
	if len(impressions) == 0 {
		return
	}

	events := make([]SearchEvent, 0, len(impressions))
	seen := map[int]bool{}
	for _, impression := range impressions {
		if impression.FloorID == 0 || impression.QueryHash == "" || seen[impression.FloorID] {
			continue
		}
		seen[impression.FloorID] = true
		events = append(events, SearchEvent{
			UserID:          userID,
			QueryHash:       impression.QueryHash,
			QueryLength:     impression.QueryLength,
			QueryTermCount:  impression.QueryTermCount,
			Accurate:        impression.Accurate,
			Source:          impression.Source,
			FloorID:         impression.FloorID,
			HoleID:          impression.HoleID,
			EventType:       eventType,
			Position:        impression.Position,
			BaseRank:        impression.BaseRank,
			BaseScore:       impression.BaseScore,
			RequestID:       requestID,
			RequestDedupKey: searchEventDedupKey(userID, impression.QueryHash, impression.FloorID, eventType, requestID),
			CreatedAt:       now,
		})
	}
	if len(events) == 0 {
		return
	}
	if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&events).Error; err != nil {
		log.Warn().Err(err).Str("request_id", requestID).Int("hole_id", holeID).Str("event_type", eventType).Msg("could not write search action")
	}
}

func searchQueryStats(keyword string) (string, int, int) {
	normalized := strings.ToLower(strings.TrimSpace(keyword))
	if normalized == "" {
		return "", 0, 0
	}
	hash := sha256.Sum256([]byte(normalized))
	terms := strings.Fields(normalized)
	termCount := len(terms)
	if termCount == 0 {
		termCount = 1
	}
	return hex.EncodeToString(hash[:]), utf8.RuneCountInString(normalized), termCount
}

func searchEventDedupKey(userID int, queryHash string, floorID int, eventType string, requestID string) *string {
	requestID = strings.TrimSpace(requestID)
	if userID == 0 || queryHash == "" || floorID == 0 || eventType == "" || requestID == "" {
		return nil
	}
	key := strconv.Itoa(userID) + ":" + eventType + ":" + requestID + ":" + queryHash + ":" + strconv.Itoa(floorID)
	return &key
}
