package floor

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/schema"

	"treehole_next/config"
	"treehole_next/models"
	"treehole_next/utils"
)

func TestSearchFloorsOldExposesGeneratedRequestID(t *testing.T) {
	oldDB := models.DB
	oldES := models.ES
	oldMode := config.Config.Mode
	oldTestLogin := config.Config.EnableTestLogin
	oldTestUserID := config.Config.TestLoginUserID
	oldTestAccessToken := config.Config.TestAccessToken
	oldOpenSearch := config.DynamicConfig.OpenSearch.Load()
	t.Cleanup(func() {
		models.DB = oldDB
		models.ES = oldES
		config.Config.Mode = oldMode
		config.Config.EnableTestLogin = oldTestLogin
		config.Config.TestLoginUserID = oldTestUserID
		config.Config.TestAccessToken = oldTestAccessToken
		config.DynamicConfig.OpenSearch.Store(oldOpenSearch)
	})

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{
		NamingStrategy: schema.NamingStrategy{SingularTable: true},
	})
	require.NoError(t, err)
	require.NoError(t, db.Exec(`
		CREATE TABLE hole (
			id INTEGER PRIMARY KEY,
			hidden BOOLEAN NOT NULL DEFAULT 0
		)
	`).Error)
	require.NoError(t, db.Exec(`
		CREATE TABLE floor (
			id INTEGER PRIMARY KEY,
			hole_id INTEGER NOT NULL,
			content TEXT,
			created_at DATETIME
		)
	`).Error)
	require.NoError(t, db.AutoMigrate(&models.FeedEvent{}, &models.FloorLike{}))
	models.DB = db
	models.ES = nil
	config.Config.Mode = "test"
	config.Config.EnableTestLogin = true
	config.Config.TestLoginUserID = 42
	config.Config.TestAccessToken = "treehole-test-access"
	config.DynamicConfig.OpenSearch.Store(true)

	app := fiber.New()
	app.Get("/floors", ListFloorsOld)
	req := httptest.NewRequest(http.MethodGet, "/floors?s=needle&length=10", nil)
	req.Header.Set("Authorization", "Bearer "+config.Config.TestAccessToken)

	resp, err := app.Test(req)

	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	requestID := resp.Header.Get(utils.RequestIDHeader)
	assert.NotEmpty(t, requestID)
	assert.Equal(t, requestID, resp.Header.Get("X-Request-ID"))
}
