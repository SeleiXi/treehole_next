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

type SearchResultLogItem struct {
	FloorID   int
	HoleID    int
	BaseRank  int
	BaseScore *float64
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
