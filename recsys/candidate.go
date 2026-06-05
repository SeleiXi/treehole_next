package recsys

import (
	"treehole_next/models"

	"github.com/gofiber/fiber/v2"
	"gorm.io/gorm"
)

type candidateSet struct {
	ids  []int
	seen map[int]bool
}

func newCandidateSet() *candidateSet {
	return &candidateSet{
		ids:  make([]int, 0),
		seen: make(map[int]bool),
	}
}

func (set *candidateSet) add(ids []int) {
	for _, id := range ids {
		if id == 0 || set.seen[id] {
			continue
		}
		set.seen[id] = true
		set.ids = append(set.ids, id)
	}
}

func recallCandidates(tx *gorm.DB, c *fiber.Ctx, req HomeFeedRequest, divisionIDs []int) ([]int, error) {
	poolSize := req.PageSize() * 12
	if poolSize < 80 {
		poolSize = 80
	}

	set := newCandidateSet()
	suppressedIDs := RecentHardSuppressedHoleIDs(tx, c, req.Now)
	recalls := []func() ([]int, error){
		func() ([]int, error) { return recallActive(tx, c, req, divisionIDs, suppressedIDs, poolSize/2) },
		func() ([]int, error) { return recallFresh(tx, c, req, divisionIDs, suppressedIDs, poolSize/3) },
		func() ([]int, error) { return recallHot(tx, c, req, divisionIDs, suppressedIDs, poolSize/3) },
		func() ([]int, error) { return recallQuality(tx, c, req, divisionIDs, suppressedIDs, poolSize/3) },
		func() ([]int, error) { return recallExplore(tx, c, req, divisionIDs, suppressedIDs, poolSize/6) },
	}

	for _, recall := range recalls {
		ids, err := recall()
		if err != nil {
			return nil, err
		}
		set.add(ids)
	}
	return set.ids, nil
}

func baseCandidateQuery(tx *gorm.DB, c *fiber.Ctx, req HomeFeedRequest, divisionIDs []int, suppressedIDs []int) (*gorm.DB, error) {
	query, err := models.MakeHoleQuerySet(c, tx)
	if err != nil {
		return nil, err
	}
	query = query.Where("hole.division_id IN ?", divisionIDs)
	if len(suppressedIDs) != 0 {
		query = query.Where("hole.id NOT IN ?", suppressedIDs)
	}
	if req.CreatedStart != nil {
		query = query.Where("hole.created_at >= ?", req.CreatedStart.Time)
	}
	if req.CreatedEnd != nil {
		query = query.Where("hole.created_at <= ?", req.CreatedEnd.Time)
	}
	if len(req.Tags) != 0 {
		tagIDs, err := resolveTagIDs(tx, req.Tags)
		if err != nil {
			return nil, err
		}
		query = query.Where("hole.id IN (?)", tx.Table("hole_tags").
			Select("hole_id").
			Where("tag_id IN ?", tagIDs).
			Group("hole_id").
			Having("COUNT(DISTINCT tag_id) = ?", len(tagIDs)))
	}
	return query, nil
}

func recallActive(tx *gorm.DB, c *fiber.Ctx, req HomeFeedRequest, divisionIDs []int, suppressedIDs []int, limit int) ([]int, error) {
	query, err := baseCandidateQuery(tx, c, req, divisionIDs, suppressedIDs)
	if err != nil {
		return nil, err
	}
	var ids []int
	err = query.Order("hole.updated_at desc").Limit(limit).Pluck("hole.id", &ids).Error
	return ids, err
}

func recallFresh(tx *gorm.DB, c *fiber.Ctx, req HomeFeedRequest, divisionIDs []int, suppressedIDs []int, limit int) ([]int, error) {
	query, err := baseCandidateQuery(tx, c, req, divisionIDs, suppressedIDs)
	if err != nil {
		return nil, err
	}
	var ids []int
	err = query.Order("hole.created_at desc").Limit(limit).Pluck("hole.id", &ids).Error
	return ids, err
}

func recallQuality(tx *gorm.DB, c *fiber.Ctx, req HomeFeedRequest, divisionIDs []int, suppressedIDs []int, limit int) ([]int, error) {
	query, err := baseCandidateQuery(tx, c, req, divisionIDs, suppressedIDs)
	if err != nil {
		return nil, err
	}
	var ids []int
	err = query.
		Order("(CASE WHEN hole.good THEN 1 ELSE 0 END) desc").
		Order("hole.favorite_count desc").
		Order("hole.subscription_count desc").
		Order("hole.updated_at desc").
		Limit(limit).
		Pluck("hole.id", &ids).Error
	return ids, err
}

func recallHot(tx *gorm.DB, c *fiber.Ctx, req HomeFeedRequest, divisionIDs []int, suppressedIDs []int, limit int) ([]int, error) {
	query, err := baseCandidateQuery(tx, c, req, divisionIDs, suppressedIDs)
	if err != nil {
		return nil, err
	}
	var ids []int
	err = query.
		Order("(hole.reply * 12 + hole.favorite_count * 24 + hole.subscription_count * 18 + hole.view * 0.25) desc").
		Order("hole.updated_at desc").
		Limit(limit).
		Pluck("hole.id", &ids).Error
	return ids, err
}

func recallExplore(tx *gorm.DB, c *fiber.Ctx, req HomeFeedRequest, divisionIDs []int, suppressedIDs []int, limit int) ([]int, error) {
	query, err := baseCandidateQuery(tx, c, req, divisionIDs, suppressedIDs)
	if err != nil {
		return nil, err
	}
	var ids []int
	err = query.Order("hole.id desc").Limit(limit).Pluck("hole.id", &ids).Error
	return ids, err
}

func resolveTagIDs(tx *gorm.DB, names []string) ([]int, error) {
	var tags []models.Tag
	err := tx.Model(&models.Tag{}).Where("name IN ?", names).Find(&tags).Error
	if err != nil {
		return nil, err
	}
	ids := make([]int, 0, len(tags))
	for _, tag := range tags {
		ids = append(ids, tag.ID)
	}
	return ids, nil
}
