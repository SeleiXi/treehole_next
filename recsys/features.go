package recsys

import (
	"treehole_next/models"

	"gorm.io/gorm"
)

func loadFeatures(tx *gorm.DB, holeIDs []int) (map[int]*models.HoleFeature, error) {
	var features []models.HoleFeature
	if len(holeIDs) == 0 {
		return map[int]*models.HoleFeature{}, nil
	}
	err := tx.Where("hole_id IN ?", holeIDs).Find(&features).Error
	if err != nil {
		return nil, err
	}
	result := make(map[int]*models.HoleFeature, len(features))
	for i := range features {
		feature := features[i]
		result[feature.HoleID] = &feature
	}
	return result, nil
}
