package main

import (
	"flag"
	"time"

	drivermysql "github.com/go-sql-driver/mysql"
	"github.com/rs/zerolog/log"

	"treehole_next/config"
	"treehole_next/models"
)

func main() {
	scope := flag.String("scope", "recsys", "migration scope")
	mysqlReadTimeout := flag.Duration("mysql-read-timeout", 10*time.Minute, "minimum MySQL read timeout for long-running DDL")
	flag.Parse()

	config.InitConfig()
	config.Config.DbURL = mysqlDSNWithMinimumReadTimeout(config.Config.DbURL, *mysqlReadTimeout)

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

func mysqlDSNWithMinimumReadTimeout(dsn string, minReadTimeout time.Duration) string {
	if dsn == "" || minReadTimeout <= 0 {
		return dsn
	}
	cfg, err := drivermysql.ParseDSN(dsn)
	if err != nil {
		log.Warn().Err(err).Msg("failed to parse mysql dsn for migration timeout")
		return dsn
	}
	if cfg.ReadTimeout < minReadTimeout {
		cfg.ReadTimeout = minReadTimeout
	}
	return cfg.FormatDSN()
}
