package recsys

import (
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"gorm.io/gorm"

	"treehole_next/models"
)

func GetHomeFeed(c *fiber.Ctx, req HomeFeedRequest) (models.Holes, error) {
	if req.Now.IsZero() {
		req.Now = time.Now()
	}
	if req.RequestID == "" {
		req.RequestID = uuid.NewString()
	}
	userID := currentUserID(c)

	var holes models.Holes
	err := models.DB.Transaction(func(tx *gorm.DB) error {
		if req.CursorID != nil {
			cached, ok, err := loadFeedSnapshotPage(tx, c, userID, req.RequestID, *req.CursorID, req.PageSize(), req.Now)
			if err != nil {
				return err
			}
			if ok {
				holes = cached
				logImpressions(tx, c, holes, req.RequestID)
				return nil
			}
		}

		divisionIDs, err := models.HomepageDivisionIDs(tx, req.ExcludeDivisionIDs)
		if err != nil {
			return err
		}
		if len(divisionIDs) == 0 {
			return nil
		}

		candidateIDs, err := recallCandidates(tx, c, req, divisionIDs)
		if err != nil {
			return err
		}
		scored, err := rankCandidates(tx, c, candidateIDs, req.Now)
		if err != nil {
			return err
		}
		scored = applyCursor(scored, req.CursorScore, req.CursorID)
		ordered := snapshotOrder(scored, req.PageSize(), req.Now)
		saveFeedSnapshot(userID, req.RequestID, ordered, req.Now)
		if len(ordered) > req.PageSize() {
			ordered = ordered[:req.PageSize()]
		}
		holes = scoredHoles(ordered)
		logImpressions(tx, c, holes, req.RequestID)
		return nil
	})
	return holes, err
}

func scoredHoles(scored []scoredHole) models.Holes {
	holes := make(models.Holes, 0, len(scored))
	for _, item := range scored {
		holes = append(holes, item.hole)
	}
	return holes
}
