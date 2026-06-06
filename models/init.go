package models

import (
	"time"

	drivermysql "github.com/go-sql-driver/mysql"
	"github.com/rs/zerolog/log"

	"treehole_next/config"

	"gorm.io/gorm/logger"
	"gorm.io/plugin/dbresolver"

	gormmysql "gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/schema"
)

var DB *gorm.DB

var gormConfig = &gorm.Config{
	NamingStrategy: schema.NamingStrategy{
		SingularTable: true, // use singular table name, table for `User` would be `user` with this option enabled
	},
	Logger: logger.New(
		&log.Logger,
		logger.Config{
			SlowThreshold:             time.Second,  // 慢 SQL 阈值
			LogLevel:                  logger.Error, // 日志级别
			IgnoreRecordNotFoundError: true,         // 忽略ErrRecordNotFound（记录未找到）错误
			Colorful:                  false,        // 禁用彩色打印
		},
	),
}

// Read/Write Splitting
func mysqlDB() *gorm.DB {
	// set source databases
	source := gormmysql.Open(withMySQLTimeoutDefaults(config.Config.DbURL))
	db, err := gorm.Open(source, gormConfig)
	if err != nil {
		log.Fatal().Err(err).Send()
	}

	// set replica databases
	var replicas []gorm.Dialector
	for _, url := range config.Config.MysqlReplicaURLs {
		replicas = append(replicas, gormmysql.Open(withMySQLTimeoutDefaults(url)))
	}
	err = db.Use(dbresolver.Register(dbresolver.Config{
		Sources:  []gorm.Dialector{source},
		Replicas: replicas,
		Policy:   dbresolver.RandomPolicy{},
	}))
	if err != nil {
		log.Fatal().Err(err).Send()
	}
	return db
}

func InitDB() {
	var err error
	switch config.Config.Mode {
	case "production":
		DB = mysqlDB()
	case "test":
		fallthrough
	case "bench":
		DB = memoryDB()
	case "dev":
		if config.Config.DbURL == "" {
			DB = sqliteDB()
		} else {
			DB = mysqlDB()
		}
	default:
		log.Fatal().Msg("unknown mode")
	}

	switch config.Config.Mode {
	case "test":
		fallthrough
	case "dev":
		DB = DB.Debug()
	}

	err = DB.SetupJoinTable(&User{}, "UserLikedFloors", &FloorLike{})
	if err != nil {
		log.Fatal().Err(err).Send()
	}

	err = DB.SetupJoinTable(&Hole{}, "Mapping", &AnonynameMapping{})
	if err != nil {
		log.Fatal().Err(err).Send()
	}

	err = DB.SetupJoinTable(&Message{}, "Users", &MessageUser{})
	if err != nil {
		log.Fatal().Err(err).Send()
	}

	err = DB.SetupJoinTable(&User{}, "UserSubscription", &UserSubscription{})
	if err != nil {
		log.Fatal().Err(err).Send()
	}

	if config.Config.DisableAutoMigrate {
		log.Info().Msg("auto migrate disabled")
	} else {
		// models must be registered here to migrate into the database
		err = DB.AutoMigrate(
			&Division{},
			&Tag{},
			&User{},
			&Floor{},
			&Hole{},
			&Report{},
			&Punishment{},
			&ReportPunishment{},
			&Message{},
			&FloorHistory{},
			&AdminLog{},
			&UserFavorite{},
			&FavoriteGroup{},
			&UrlHostnameBlacklist{},
			&HoleFeature{},
			&FeedEvent{},
			&SearchEvent{},
		)
		if err != nil {
			log.Fatal().Err(err).Send()
		}
	}

	err = DB.Model(&UrlHostnameBlacklist{}).Pluck("hostname", &config.Config.UrlHostnameBlacklist).Error
	if err != nil {
		log.Fatal().Err(err).Send()
	}
}

func withMySQLTimeoutDefaults(dsn string) string {
	cfg, err := drivermysql.ParseDSN(dsn)
	if err != nil {
		log.Warn().Err(err).Msg("failed to parse mysql dsn for timeout defaults")
		return dsn
	}
	if cfg.Timeout == 0 {
		cfg.Timeout = 5 * time.Second
	}
	if cfg.ReadTimeout == 0 {
		cfg.ReadTimeout = 30 * time.Second
	}
	if cfg.WriteTimeout == 0 {
		cfg.WriteTimeout = 30 * time.Second
	}
	return cfg.FormatDSN()
}
