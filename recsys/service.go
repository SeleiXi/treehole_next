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

	var holes models.Holes
	err := models.DB.Transaction(func(tx *gorm.DB) error {
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
		holes = rerank(scored, req.PageSize())
		logImpressions(tx, c, holes, req.RequestID)
		return nil
	})
	return holes, err
}
