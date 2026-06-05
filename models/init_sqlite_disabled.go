//go:build production

package models

import (
	"github.com/rs/zerolog/log"
	"gorm.io/gorm"
)

func sqliteDB() *gorm.DB {
	log.Fatal().Msg("sqlite db is disabled in production builds")
	return nil
}

func memoryDB() *gorm.DB {
	log.Fatal().Msg("memory db is disabled in production builds")
	return nil
}
