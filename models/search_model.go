package models

import (
	"math"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/gofiber/fiber/v2"
	"github.com/rs/zerolog/log"

	"treehole_next/config"
	"treehole_next/recsys/modelrank"
)

const searchFeatureMaxAge = 15 * time.Minute

type searchHoleContext struct {
	hole       Hole
	feature    HoleFeature
	hasFeature bool
	tagCount   int
}

func applySearchModelRerank(c *fiber.Ctx, keyword string, floors Floors, baseRanks map[int]int, baseScores map[int]*float64, now time.Time) Floors {
	if !config.Config.SearchModelRanking || len(floors) <= 1 {
		return floors
	}
	model, err := modelrank.Load(config.Config.SearchModelPath)
	if err != nil {
		log.Warn().Err(err).Str("path", config.Config.SearchModelPath).Msg("could not load search rank model")
		return floors
	}
	if model == nil {
		return floors
	}
	if now.IsZero() {
		now = time.Now()
	}

	contexts := loadSearchHoleContexts(floors)
	scored := make([]struct {
		floor    *Floor
		score    float64
		baseRank int
	}, 0, len(floors))
	for position, floor := range floors {
		if floor == nil {
			continue
		}
		baseRank, ok := baseRanks[floor.ID]
		if !ok {
			baseRank = position
		}
		score := model.Score(searchModelFeatures(keyword, floor, contexts[floor.HoleID], baseRank, baseScores[floor.ID], now))
		scored = append(scored, struct {
			floor    *Floor
			score    float64
			baseRank int
		}{floor: floor, score: score, baseRank: baseRank})
	}
	sort.SliceStable(scored, func(i, j int) bool {
		if scored[i].score == scored[j].score {
			return scored[i].baseRank < scored[j].baseRank
		}
		return scored[i].score > scored[j].score
	})

	result := make(Floors, 0, len(scored))
	for _, item := range scored {
		result = append(result, item.floor)
	}
	return result
}

func loadSearchHoleContexts(floors Floors) map[int]searchHoleContext {
	holeIDs := make([]int, 0, len(floors))
	seen := map[int]bool{}
	for _, floor := range floors {
		if floor == nil || floor.HoleID == 0 || seen[floor.HoleID] {
			continue
		}
		seen[floor.HoleID] = true
		holeIDs = append(holeIDs, floor.HoleID)
	}
	if len(holeIDs) == 0 {
		return map[int]searchHoleContext{}
	}

	contexts := make(map[int]searchHoleContext, len(holeIDs))
	var holes []Hole
	if err := DB.Model(&Hole{}).
		Select("id, created_at, updated_at, view, reply, hidden, locked, frozen, good, division_id, favorite_count, subscription_count").
		Where("id IN ?", holeIDs).
		Find(&holes).Error; err != nil {
		return contexts
	}
	for _, hole := range holes {
		contexts[hole.ID] = searchHoleContext{hole: hole}
	}

	var features []HoleFeature
	if err := DB.Where("hole_id IN ?", holeIDs).Find(&features).Error; err == nil {
		for _, feature := range features {
			ctx := contexts[feature.HoleID]
			ctx.feature = feature
			ctx.hasFeature = true
			contexts[feature.HoleID] = ctx
		}
	}

	type tagCountRow struct {
		HoleID int
		Count  int
	}
	var tagCounts []tagCountRow
	if err := DB.Table("hole_tags").
		Select("hole_id, COUNT(*) AS count").
		Where("hole_id IN ?", holeIDs).
		Group("hole_id").
		Find(&tagCounts).Error; err == nil {
		for _, row := range tagCounts {
			ctx := contexts[row.HoleID]
			ctx.tagCount = row.Count
			contexts[row.HoleID] = ctx
		}
	}
	return contexts
}

func searchModelFeatures(keyword string, floor *Floor, ctx searchHoleContext, baseRank int, baseScore *float64, now time.Time) map[string]float64 {
	queryHash, queryLength, queryTermCount := searchQueryStats(keyword)
	_ = queryHash
	hole := ctx.hole
	ageHours := math.Max(now.Sub(hole.CreatedAt).Hours(), 0)
	updateHours := math.Max(now.Sub(hole.UpdatedAt).Hours(), 0)
	floorAgeHours := math.Max(now.Sub(floor.CreatedAt).Hours(), 0)
	normalizedQuery := strings.ToLower(strings.TrimSpace(keyword))
	normalizedContent := strings.ToLower(floor.Content)

	features := map[string]float64{
		"base_rank":             float64(baseRank),
		"inverse_base_rank":     1 / float64(baseRank+1),
		"query_length":          float64(queryLength),
		"query_term_count":      float64(queryTermCount),
		"content_length_log":    math.Log1p(float64(utf8.RuneCountInString(floor.Content))),
		"floor_age_hours_log":   math.Log1p(floorAgeHours),
		"floor_like_log":        math.Log1p(float64(floor.Like)),
		"floor_dislike_log":     math.Log1p(float64(floor.Dislike)),
		"floor_ranking":         float64(floor.Ranking),
		"hole_reply_log":        math.Log1p(float64(hole.Reply)),
		"hole_view_log":         math.Log1p(float64(hole.View)),
		"hole_favorite_log":     math.Log1p(float64(hole.FavoriteCount)),
		"hole_subscription_log": math.Log1p(float64(hole.SubscriptionCount)),
		"hole_age_hours_log":    math.Log1p(ageHours),
		"hole_update_hours_log": math.Log1p(updateHours),
		"hole_freshness_24h":    24.0 / (24.0 + updateHours),
		"hole_division_id":      float64(hole.DivisionID),
		"hole_tag_count":        float64(ctx.tagCount),
	}
	if baseScore != nil {
		features["base_score"] = *baseScore
		features["base_score_log"] = math.Log1p(math.Max(*baseScore, 0))
	}
	if floor.Ranking == 0 {
		features["is_first_floor"] = 1
	}
	if hole.Good {
		features["hole_is_good"] = 1
	}
	if hole.Locked {
		features["hole_is_locked"] = 1
	}
	if hole.Frozen {
		features["hole_is_frozen"] = 1
	}
	if normalizedQuery != "" && strings.Contains(normalizedContent, normalizedQuery) {
		features["query_exact_substring"] = 1
	}
	if ctx.hasFeature && ctx.feature.UpdatedAt.After(now.Add(-searchFeatureMaxAge)) {
		features["feature_hot_score"] = ctx.feature.HotScore
		features["feature_quality_score"] = ctx.feature.QualityScore
		features["feature_controversy_score"] = ctx.feature.ControversyScore
		features["feature_reply_24h_log"] = math.Log1p(float64(ctx.feature.Reply24h))
		features["feature_view_24h_log"] = math.Log1p(float64(ctx.feature.View24h))
	}
	return features
}
