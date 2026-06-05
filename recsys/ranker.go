package recsys

import (
	"sort"
	"time"

	"github.com/gofiber/fiber/v2"
	"treehole_next/models"

	"gorm.io/gorm"
)

func rankCandidates(tx *gorm.DB, c *fiber.Ctx, holeIDs []int, now time.Time) ([]scoredHole, error) {
	return rankCandidatesForSize(tx, c, holeIDs, now, 1)
}

func rankCandidatesForSize(tx *gorm.DB, c *fiber.Ctx, holeIDs []int, now time.Time, minResults int) ([]scoredHole, error) {
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

	scored := make([]scoredHole, 0, len(holes))
	softSuppressed := make([]scoredHole, 0)
	for _, hole := range holes {
		if feedback.shouldHardSuppress(hole.ID) {
			continue
		}
		tagIDs := holeTags[hole.ID]
		score := scoreCandidate(hole, features[hole.ID], feedback, tagIDs, now)
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
