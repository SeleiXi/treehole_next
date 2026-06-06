package tests

import (
	"testing"

	. "treehole_next/models"

	"github.com/stretchr/testify/assert"
	"golang.org/x/exp/slices"
)

func TestListFavorites(t *testing.T) {
	var holes Holes
	testAPIModel(t, "get", "/api/user/favorites", 200, &holes)
	assert.EqualValues(t, 10, len(holes))
}

func TestAddFavorite(t *testing.T) {
	data := Map{"hole_id": 11}
	testAPI(t, "post", "/api/user/favorites", 201, data)
	testAPI(t, "post", "/api/user/favorites", 201, data) // duplicated, refresh updated_at
}

func TestAddFavoriteLogsBodyRequestID(t *testing.T) {
	requestID := "test-favorite-body-request"
	data := Map{"hole_id": 12, "request_id": requestID}

	testAPI(t, "post", "/api/user/favorites", 201, data)

	var count int64
	err := DB.Model(&FeedEvent{}).
		Where("request_id = ? AND event_type = ? AND hole_id = ?", requestID, FeedEventFavorite, 12).
		Count(&count).Error
	assert.Nil(t, err)
	assert.EqualValues(t, 1, count)
}

func TestModifyFavorites(t *testing.T) {
	data := Map{"hole_ids": []int{1, 2, 5, 6, 7}}
	testAPI(t, "put", "/api/user/favorites", 201, data)
	testAPI(t, "put", "/api/user/favorites", 201, data) // duplicated
	var userFavorites []UserFavorite
	DB.Where("user_id = ?", 1).Find(&userFavorites)
	assert.EqualValues(t, 5, len(userFavorites))
}

func TestDeleteFavorite(t *testing.T) {
	data := Map{"hole_id": 1}
	testAPI(t, "delete", "/api/user/favorites", 200, data)
	var userFavorites []UserFavorite
	DB.Where("user_id = ?", 1).Find(&userFavorites)
	assert.EqualValues(t, false, slices.Contains(userFavorites, UserFavorite{UserID: 1, HoleID: 1}))
	favouriteLen := len(userFavorites)

	testAPI(t, "delete", "/api/user/favorites", 200, data) // duplicated
	DB.Where("user_id = ?", 1).Find(&userFavorites)
	assert.EqualValues(t, favouriteLen, len(userFavorites))
}
