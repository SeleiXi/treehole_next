package main

import (
	"flag"

	"github.com/rs/zerolog/log"

	"treehole_next/config"
	"treehole_next/models"
)

func main() {
	scope := flag.String("scope", "recsys", "migration scope")
	flag.Parse()

	config.InitConfig()

	switch *scope {
	case "recsys":
	default:
		log.Fatal().Str("scope", *scope).Msg("unknown database migration scope")
	}

	db := models.OpenConfiguredDB()
	if err := models.MigrateRecsysTables(db); err != nil {
		log.Fatal().Err(err).Str("scope", *scope).Msg("database migration failed")
	}

	log.Info().Str("scope", *scope).Msg("database migration complete")
}
