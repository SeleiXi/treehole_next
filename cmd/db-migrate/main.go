package main

import (
	"github.com/rs/zerolog/log"

	"treehole_next/config"
	"treehole_next/models"
)

func main() {
	config.InitConfig()
	config.Config.AutoMigrate = true
	config.Config.DisableAutoMigrate = false

	models.InitDB()
	log.Info().Msg("database migration complete")
}
