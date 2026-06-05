package recsys

import (
	"strconv"
	"sync"
	"time"

	"github.com/gofiber/fiber/v2"
	"gorm.io/gorm"

	"treehole_next/models"
)

const feedSnapshotTTL = 10 * time.Minute

type feedSnapshotItem struct {
	holeID int
	score  float64
}

type feedSnapshot struct {
	expiresAt time.Time
	items     []feedSnapshotItem
}

var feedSnapshots = struct {
	sync.Mutex
	values map[string]feedSnapshot
}{
	values: map[string]feedSnapshot{},
}

func feedSnapshotKey(userID int, requestID string) string {
	return strconv.Itoa(userID) + ":" + requestID
}

func loadFeedSnapshotPage(tx *gorm.DB, c *fiber.Ctx, userID int, requestID string, cursorID int, size int, now time.Time) (models.Holes, bool, error) {
	if userID == 0 || requestID == "" || cursorID == 0 {
		return nil, false, nil
	}
	snapshot, ok := getFeedSnapshot(userID, requestID, now)
	if !ok {
		return nil, false, nil
	}
	start := -1
	for i, item := range snapshot.items {
		if item.holeID == cursorID {
			start = i + 1
			break
		}
	}
	if start < 0 || start >= len(snapshot.items) {
		return nil, true, nil
	}
	end := start + size
	if end > len(snapshot.items) {
		end = len(snapshot.items)
	}
	pageItems := snapshot.items[start:end]
	ids := make([]int, 0, len(pageItems))
	scores := make(map[int]float64, len(pageItems))
	for _, item := range pageItems {
		ids = append(ids, item.holeID)
		scores[item.holeID] = item.score
	}

	query, err := models.MakeHoleQuerySet(c, tx)
	if err != nil {
		return nil, false, err
	}
	var holes models.Holes
	if err := query.Where("hole.id IN ?", ids).Find(&holes).Error; err != nil {
		return nil, false, err
	}
	byID := make(map[int]*models.Hole, len(holes))
	for _, hole := range holes {
		score := scores[hole.ID]
		hole.SortScore = &score
		byID[hole.ID] = hole
	}
	ordered := make(models.Holes, 0, len(holes))
	for _, id := range ids {
		if hole := byID[id]; hole != nil {
			ordered = append(ordered, hole)
		}
	}
	return ordered, true, nil
}

func saveFeedSnapshot(userID int, requestID string, scored []scoredHole, now time.Time) {
	if userID == 0 || requestID == "" || len(scored) == 0 {
		return
	}
	items := make([]feedSnapshotItem, 0, len(scored))
	for _, item := range scored {
		items = append(items, feedSnapshotItem{holeID: item.hole.ID, score: item.score})
	}

	feedSnapshots.Lock()
	defer feedSnapshots.Unlock()
	pruneFeedSnapshotsLocked(now)
	feedSnapshots.values[feedSnapshotKey(userID, requestID)] = feedSnapshot{
		expiresAt: now.Add(feedSnapshotTTL),
		items:     items,
	}
}

func getFeedSnapshot(userID int, requestID string, now time.Time) (feedSnapshot, bool) {
	feedSnapshots.Lock()
	defer feedSnapshots.Unlock()
	pruneFeedSnapshotsLocked(now)
	snapshot, ok := feedSnapshots.values[feedSnapshotKey(userID, requestID)]
	return snapshot, ok
}

func pruneFeedSnapshotsLocked(now time.Time) {
	for key, snapshot := range feedSnapshots.values {
		if snapshot.expiresAt.Before(now) {
			delete(feedSnapshots.values, key)
		}
	}
}

func snapshotOrder(scored []scoredHole, pageSize int, now time.Time) []scoredHole {
	if len(scored) == 0 {
		return nil
	}
	firstPage := rerank(scored, pageSize, now)
	selected := make(map[int]bool, len(firstPage))
	byID := make(map[int]scoredHole, len(scored))
	for _, item := range scored {
		byID[item.hole.ID] = item
	}

	ordered := make([]scoredHole, 0, len(scored))
	for _, hole := range firstPage {
		if item, ok := byID[hole.ID]; ok {
			ordered = append(ordered, item)
			selected[hole.ID] = true
		}
	}
	for _, item := range scored {
		if !selected[item.hole.ID] {
			ordered = append(ordered, item)
		}
	}
	return ordered
}
