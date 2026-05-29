package recsys

import (
	"sort"
	"time"

	"treehole_next/models"

	"gorm.io/gorm"
)

func rankCandidates(tx *gorm.DB, holeIDs []int, now time.Time) ([]scoredHole, error) {
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

	scored := make([]scoredHole, 0, len(holes))
	for _, hole := range holes {
		score := scoreHole(hole, features[hole.ID], now)
		hole.SortScore = &score
		scored = append(scored, scoredHole{hole: hole, score: score})
	}

	sort.SliceStable(scored, func(i, j int) bool {
		if scored[i].score == scored[j].score {
			return scored[i].hole.ID > scored[j].hole.ID
		}
		return scored[i].score > scored[j].score
	})
	return scored, nil
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
