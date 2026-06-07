package recsys

import (
	"sort"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/rs/zerolog/log"
	"treehole_next/config"
	"treehole_next/models"
	"treehole_next/recsys/modelrank"

	"gorm.io/gorm"
)

func rankCandidates(tx *gorm.DB, c *fiber.Ctx, holeIDs []int, now time.Time) ([]scoredHole, error) {
	return rankCandidatesForSize(tx, c, holeIDs, now, 1, true)
}

func rankCandidatesForSize(tx *gorm.DB, c *fiber.Ctx, holeIDs []int, now time.Time, minResults int, useModel bool) ([]scoredHole, error) {
	if len(holeIDs) == 0 {
		return nil, nil
	}

	var holes models.Holes
	if err := tx.Where("id IN ?", holeIDs).Find(&holes).Error; err != nil {
		return nil, err
	}

	features, err := loadFeatures(tx, holeIDs)
	if err != nil {
		return nil, err
	}
	feedback := loadUserFeedback(tx, c, holeIDs, now)
	holeTags := loadCandidateTags(tx, holeIDs)
	var model *modelrank.LinearModel
	if useModel {
		model = loadRecsysRankModel()
	}

	scored := make([]scoredHole, 0, len(holes))
	softSuppressed := make([]scoredHole, 0)
	for _, hole := range holes {
		if feedback.shouldHardSuppress(hole.ID) {
			continue
		}
		tagIDs := holeTags[hole.ID]
		baseRuleScore := scoreHole(hole, features[hole.ID], now)
		score := baseRuleScore + feedback.affinityScore(hole, tagIDs) - feedback.penalty(hole.ID)
		if model != nil {
			score = scoreCandidateWithModel(hole, features[hole.ID], feedback, tagIDs, now, baseRuleScore, model)
		}
		hole.SortScore = &score
		item := scoredHole{hole: hole, score: score, tagIDs: tagIDs}
		if feedback.shouldSuppress(hole.ID) {
			softSuppressed = append(softSuppressed, item)
			continue
		}
		scored = append(scored, item)
	}

	sortScored(scored)
	sortScored(softSuppressed)
	if minResults <= 0 {
		minResults = 1
	}
	if len(scored) < minResults {
		scored = append(scored, softSuppressed...)
	}
	return scored, nil
}

func loadRecsysRankModel() *modelrank.LinearModel {
	if !config.Config.RecsysModelRanking {
		return nil
	}
	model, err := modelrank.Load(config.Config.RecsysModelPath)
	if err != nil {
		log.Warn().Err(err).Str("path", config.Config.RecsysModelPath).Msg("could not load recsys rank model")
		return nil
	}
	if model != nil && !model.SupportsTask("home", "feed", "recommend") {
		log.Warn().Str("path", config.Config.RecsysModelPath).Str("task", model.Task).Msg("ignoring recsys rank model with incompatible task")
		return nil
	}
	return model
}

func sortScored(scored []scoredHole) {
	sort.SliceStable(scored, func(i, j int) bool {
		if scored[i].score == scored[j].score {
			return scored[i].hole.ID > scored[j].hole.ID
		}
		return scored[i].score > scored[j].score
	})
}

func loadCandidateTags(tx *gorm.DB, holeIDs []int) map[int][]int {
	result := map[int][]int{}
	if len(holeIDs) == 0 {
		return result
	}
	var rows []models.HoleTag
	if err := tx.Where("hole_id IN ?", holeIDs).Find(&rows).Error; err != nil {
		return result
	}
	for _, row := range rows {
		result[row.HoleID] = append(result[row.HoleID], row.TagID)
	}
	return result
}

func applyCursor(scored []scoredHole, cursorScore *float64, cursorID *int) []scoredHole {
	if cursorScore == nil || cursorID == nil {
		return scored
	}
	for i, item := range scored {
		if item.score < *cursorScore || (almostEqual(item.score, *cursorScore) && item.hole.ID < *cursorID) {
			return scored[i:]
		}
	}
	return nil
}

func almostEqual(a, b float64) bool {
	const epsilon = 0.000001
	if a > b {
		return a-b < epsilon
	}
	return b-a < epsilon
}
