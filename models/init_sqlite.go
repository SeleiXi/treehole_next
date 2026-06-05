//go:build !production

package models

import (
	"os"

	"github.com/rs/zerolog/log"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func sqliteDB() *gorm.DB {
	err := os.MkdirAll("data", 0750)
	if err != nil {
		log.Fatal().Err(err).Send()
	}
	db, err := gorm.Open(sqlite.Open("data/sqlite.db"), gormConfig)
	if err != nil {
		log.Fatal().Err(err).Send()
	}
	// https://github.com/go-gorm/gorm/issues/3709
	phyDB, err := db.DB()
	if err != nil {
		log.Fatal().Err(err).Send()
	}
	phyDB.SetMaxOpenConns(1)
	return db
}

func memoryDB() *gorm.DB {
	db, err := gorm.Open(sqlite.Open("file::memory:?cache=shared"), gormConfig)
	if err != nil {
		log.Fatal().Err(err).Send()
	}
	// https://github.com/go-gorm/gorm/issues/3709
	phyDB, err := db.DB()
	if err != nil {
		log.Fatal().Err(err).Send()
	}
	phyDB.SetMaxOpenConns(1)
	return db
}
