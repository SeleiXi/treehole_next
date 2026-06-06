package models

import (
	"slices"
	"sync"
	"time"

	"treehole_next/utils"

	"github.com/gofiber/fiber/v2"
	"gorm.io/gorm"
)

type Division struct {
	/// saved fields
	ID        int       `json:"id" gorm:"primaryKey"`
	CreatedAt time.Time `json:"time_created" gorm:"not null"`
	UpdatedAt time.Time `json:"time_updated" gorm:"not null"`

	/// base info
	Name        string `json:"name" gorm:"unique;size:10"`
	Description string `json:"description" gorm:"size:64"`
	Hidden      bool   `json:"hidden" gorm:"not null;default:false"`

	// pinned holes in given order
	Pinned []int `json:"-" gorm:"serializer:json;size:100;not null;default:\"[]\""`

	/// association fields, should add foreign key

	// return pinned hole to frontend
	Holes Holes `json:"pinned"`

	/// generated field
	DivisionID int `json:"division_id" gorm:"-:all"`

	ShowInHomePage bool `json:"show_in_home_page" gorm:"not null;default:true"`
}

const homepageDivisionIDsCacheTTL = 5 * time.Minute

var homepageDivisionIDsCache = struct {
	sync.Mutex
	ids       []int
	expiresAt time.Time
}{}

func HomepageDivisionIDs(tx *gorm.DB, excludeDivisionIDs *[]int) (divisionIDs []int, err error) {
	divisionIDs, err = cachedHomepageDivisionIDs(tx)
	if err != nil || excludeDivisionIDs == nil || len(*excludeDivisionIDs) == 0 {
		return divisionIDs, err
	}

	excluded := make(map[int]bool, len(*excludeDivisionIDs))
	for _, id := range *excludeDivisionIDs {
		excluded[id] = true
	}
	filtered := make([]int, 0, len(divisionIDs))
	for _, id := range divisionIDs {
		if !excluded[id] {
			filtered = append(filtered, id)
		}
	}
	return filtered, nil
}

func cachedHomepageDivisionIDs(tx *gorm.DB) ([]int, error) {
	now := time.Now()
	homepageDivisionIDsCache.Lock()
	if !homepageDivisionIDsCache.expiresAt.IsZero() && now.Before(homepageDivisionIDsCache.expiresAt) {
		ids := slices.Clone(homepageDivisionIDsCache.ids)
		homepageDivisionIDsCache.Unlock()
		return ids, nil
	}
	homepageDivisionIDsCache.Unlock()

	if tx == nil {
		tx = DB
	}
	var ids []int
	err := tx.Model(&Division{}).
		Select("id").
		Where("show_in_home_page = ?", true).
		Order("id").
		Find(&ids).Error
	if err != nil {
		return nil, err
	}

	homepageDivisionIDsCache.Lock()
	homepageDivisionIDsCache.ids = slices.Clone(ids)
	homepageDivisionIDsCache.expiresAt = now.Add(homepageDivisionIDsCacheTTL)
	homepageDivisionIDsCache.Unlock()
	return slices.Clone(ids), nil
}

func ResetHomepageDivisionIDsCache() {
	homepageDivisionIDsCache.Lock()
	defer homepageDivisionIDsCache.Unlock()
	homepageDivisionIDsCache.ids = nil
	homepageDivisionIDsCache.expiresAt = time.Time{}
}

func (division *Division) GetID() int {
	return division.ID
}

type Divisions []*Division

func (divisions Divisions) Preprocess(c *fiber.Ctx) error {
	for _, division := range divisions {
		err := division.Preprocess(c)
		if err != nil {
			return err
		}
	}
	return nil //utils.SetCache("divisions", divisions, 0)
}

func (division *Division) Preprocess(c *fiber.Ctx) error {
	var pinned = division.Pinned
	division.Holes = make(Holes, 0, 10)
	if len(pinned) == 0 {
		return nil
	}
	DB.Find(&division.Holes, pinned)
	if len(division.Holes) == 0 {
		return nil
	}
	division.Holes = utils.OrderInGivenOrder(division.Holes, pinned)
	// division.Holes = division.Holes.RemoveIf(func(hole *Hole) bool {
	// 	return hole.Hidden
	// })
	return division.Holes.Preprocess(c)
}

func (division *Division) AfterFind(_ *gorm.DB) (err error) {
	division.DivisionID = division.ID
	return nil
}

func (division *Division) AfterCreate(_ *gorm.DB) (err error) {
	division.DivisionID = division.ID
	ResetHomepageDivisionIDsCache()
	return nil
}

func (division *Division) AfterUpdate(_ *gorm.DB) (err error) {
	ResetHomepageDivisionIDsCache()
	return nil
}

func (division *Division) AfterDelete(_ *gorm.DB) (err error) {
	ResetHomepageDivisionIDsCache()
	return nil
}
