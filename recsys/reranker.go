package recsys

import (
	"time"

	"treehole_next/models"
)

const freshPostWindow = 48 * time.Hour

func rerank(scored []scoredHole, size int, now time.Time) models.Holes {
	if size <= 0 {
		size = 10
	}
	result := make(models.Holes, 0, size)
	used := make([]bool, len(scored))
	divisionRun := make(map[int]int)
	divisionUse := make(map[int]int)
	tagUse := make(map[int]int)
	lastDivision := 0
	lastAuthor := 0
	newPostTarget := max(1, size/5)
	newPostCount := 0
	maxPerDivision := max(2, size/2)
	maxPerTag := max(2, size/3)

	for len(result) < size {
		index := pickNext(scored, used, rerankState{
			now:            now,
			lastDivision:   lastDivision,
			lastAuthor:     lastAuthor,
			divisionRun:    divisionRun[lastDivision],
			divisionUse:    divisionUse,
			tagUse:         tagUse,
			maxPerDivision: maxPerDivision,
			maxPerTag:      maxPerTag,
			newPostTarget:  newPostTarget,
			newPostCount:   newPostCount,
		})
		if index < 0 {
			break
		}
		used[index] = true
		hole := scored[index].hole
		if hole.DivisionID == lastDivision {
			divisionRun[lastDivision]++
		} else {
			lastDivision = hole.DivisionID
			divisionRun[lastDivision] = 1
		}
		divisionUse[hole.DivisionID]++
		lastAuthor = hole.UserID
		for _, tagID := range scored[index].tagIDs {
			tagUse[tagID]++
		}
		if isFreshPost(hole, now) {
			newPostCount++
		}
		result = append(result, hole)
	}
	return result
}

type rerankState struct {
	now            time.Time
	lastDivision   int
	lastAuthor     int
	divisionRun    int
	divisionUse    map[int]int
	tagUse         map[int]int
	maxPerDivision int
	maxPerTag      int
	newPostTarget  int
	newPostCount   int
}

func pickNext(scored []scoredHole, used []bool, state rerankState) int {
	fallback := -1
	for i, item := range scored {
		if used[i] {
			continue
		}
		if fallback < 0 {
			fallback = i
		}
		if state.divisionRun >= 2 && item.hole.DivisionID == state.lastDivision {
			continue
		}
		if state.lastAuthor != 0 && item.hole.UserID == state.lastAuthor {
			continue
		}
		if state.divisionUse[item.hole.DivisionID] >= state.maxPerDivision {
			continue
		}
		if hasOverusedTag(item.tagIDs, state.tagUse, state.maxPerTag) {
			continue
		}
		if state.newPostCount < state.newPostTarget && !isFreshPost(item.hole, state.now) {
			continue
		}
		return i
	}
	return fallback
}

func isFreshPost(hole *models.Hole, now time.Time) bool {
	return !hole.CreatedAt.IsZero() && !hole.CreatedAt.Before(now.Add(-freshPostWindow))
}

func hasOverusedTag(tagIDs []int, tagUse map[int]int, maxPerTag int) bool {
	for _, tagID := range tagIDs {
		if tagUse[tagID] >= maxPerTag {
			return true
		}
	}
	return false
}
