package models

import (
	"net/http/httptest"
	"testing"
	"time"

	"treehole_next/config"

	"github.com/gofiber/fiber/v2"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/schema"
)

func TestUpdateAISummaryAvailabilityUsesBatchedFloorStats(t *testing.T) {
	oldDB := DB
	oldEnableTestLogin := config.Config.EnableTestLogin
	oldTestAccessToken := config.Config.TestAccessToken
	oldTestLoginUserID := config.Config.TestLoginUserID
	oldWhiteListUserIDs := config.Config.WhiteListUserIds
	oldWhiteListRate := config.Config.WhiteListRate
	oldSummaryFloorLimit := config.Config.SummaryFloorLimit
	oldSummaryContentLimit := config.Config.SummaryContentLimit
	t.Cleanup(func() {
		DB = oldDB
		config.Config.EnableTestLogin = oldEnableTestLogin
		config.Config.TestAccessToken = oldTestAccessToken
		config.Config.TestLoginUserID = oldTestLoginUserID
		config.Config.WhiteListUserIds = oldWhiteListUserIDs
		config.Config.WhiteListRate = oldWhiteListRate
		config.Config.SummaryFloorLimit = oldSummaryFloorLimit
		config.Config.SummaryContentLimit = oldSummaryContentLimit
	})

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{
		NamingStrategy: schema.NamingStrategy{SingularTable: true},
	})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&Floor{}))
	DB = db

	config.Config.EnableTestLogin = true
	config.Config.TestAccessToken = "test-token"
	config.Config.TestLoginUserID = 42
	config.Config.WhiteListUserIds = nil
	config.Config.WhiteListRate = 1
	config.Config.SummaryFloorLimit = 15
	config.Config.SummaryContentLimit = 10

	now := time.Now()
	require.NoError(t, db.Create(&[]Floor{
		{ID: 1, HoleID: 1, Content: "01234567890", UserID: 1, Anonyname: "a", CreatedAt: now, UpdatedAt: now},
		{ID: 2, HoleID: 2, Content: "short", UserID: 1, Anonyname: "b", CreatedAt: now, UpdatedAt: now, IsSensitive: true},
		{ID: 3, HoleID: 3, Content: "01234567890", UserID: 1, Anonyname: "c", CreatedAt: now, UpdatedAt: now},
	}).Error)

	holes := Holes{
		{ID: 1, Reply: 1},
		{ID: 2, Reply: 20},
		{ID: 3, Reply: 20, Tags: Tags{{Name: "*hidden"}}},
	}
	app := fiber.New()
	app.Get("/", func(c *fiber.Ctx) error {
		require.NoError(t, holes.updateAISummaryAvailability(c))
		return c.SendStatus(fiber.StatusNoContent)
	})
	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Authorization", "Bearer test-token")
	resp, err := app.Test(req)
	require.NoError(t, err)
	require.Equal(t, fiber.StatusNoContent, resp.StatusCode)

	require.True(t, holes[0].AISummaryAvailable)
	require.False(t, holes[1].AISummaryAvailable)
	require.False(t, holes[2].AISummaryAvailable)
}
