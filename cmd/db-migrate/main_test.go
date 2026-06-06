package main

import (
	"testing"
	"time"

	drivermysql "github.com/go-sql-driver/mysql"
	"github.com/stretchr/testify/require"
)

func TestMySQLDSNWithMinimumReadTimeoutRaisesShortTimeout(t *testing.T) {
	dsn := "user:pass@tcp(127.0.0.1:3306)/treehole?parseTime=true&readTimeout=60s&writeTimeout=30s"

	updated := mysqlDSNWithMinimumReadTimeout(dsn, 10*time.Minute)
	cfg, err := drivermysql.ParseDSN(updated)

	require.NoError(t, err)
	require.Equal(t, 10*time.Minute, cfg.ReadTimeout)
	require.Equal(t, 30*time.Second, cfg.WriteTimeout)
}

func TestMySQLDSNWithMinimumReadTimeoutKeepsLongerTimeout(t *testing.T) {
	dsn := "user:pass@tcp(127.0.0.1:3306)/treehole?parseTime=true&readTimeout=20m"

	updated := mysqlDSNWithMinimumReadTimeout(dsn, 10*time.Minute)
	cfg, err := drivermysql.ParseDSN(updated)

	require.NoError(t, err)
	require.Equal(t, 20*time.Minute, cfg.ReadTimeout)
}
