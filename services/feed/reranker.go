package feed

import "treehole_next/models"

func rerank(scored []scoredHole, size int) models.Holes {
	if size <= 0 {
		size = 10
	}
	result := make(models.Holes, 0, size)
	used := make([]bool, len(scored))
	divisionRun := make(map[int]int)
	lastDivision := 0
	newPostTarget := max(1, size/5)
	newPostCount := 0

	for len(result) < size {
		index := pickNext(scored, used, lastDivision, divisionRun[lastDivision], newPostTarget, newPostCount)
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
		if hole.Reply <= 2 {
			newPostCount++
		}
		result = append(result, hole)
	}
	return result
}

func pickNext(scored []scoredHole, used []bool, lastDivision int, runCount int, newPostTarget int, newPostCount int) int {
	fallback := -1
	for i, item := range scored {
		if used[i] {
			continue
		}
		if fallback < 0 {
			fallback = i
		}
		if runCount >= 2 && item.hole.DivisionID == lastDivision {
			continue
		}
		if newPostCount < newPostTarget && item.hole.Reply > 2 {
			continue
		}
		return i
	}
	return fallback
}
