package models

import (
	"testing"

	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/schema"
)

func TestHomepageDivisionIDsCachesAndInvalidates(t *testing.T) {
	ResetHomepageDivisionIDsCache()
	t.Cleanup(ResetHomepageDivisionIDsCache)

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{
		NamingStrategy: schema.NamingStrategy{SingularTable: true},
	})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&Division{}))
	require.NoError(t, db.Create(&[]Division{
		{ID: 1, Name: "one", ShowInHomePage: true},
		{ID: 2, Name: "two", ShowInHomePage: false},
		{ID: 3, Name: "three", ShowInHomePage: true},
	}).Error)
	require.NoError(t, db.Model(&Division{ID: 2}).Update("show_in_home_page", false).Error)

	ids, err := HomepageDivisionIDs(db, nil)
	require.NoError(t, err)
	require.Equal(t, []int{1, 3}, ids)

	exclude := []int{3}
	ids, err = HomepageDivisionIDs(db, &exclude)
	require.NoError(t, err)
	require.Equal(t, []int{1}, ids)

	require.NoError(t, db.Model(&Division{ID: 2}).Update("show_in_home_page", true).Error)
	ids, err = HomepageDivisionIDs(db, nil)
	require.NoError(t, err)
	require.Equal(t, []int{1, 2, 3}, ids)
}

func TestHomepageDivisionIDsReturnsCacheCopies(t *testing.T) {
	ResetHomepageDivisionIDsCache()
	t.Cleanup(ResetHomepageDivisionIDsCache)

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{
		NamingStrategy: schema.NamingStrategy{SingularTable: true},
	})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&Division{}))
	require.NoError(t, db.Create(&Division{ID: 1, Name: "one", ShowInHomePage: true}).Error)

	ids, err := HomepageDivisionIDs(db, nil)
	require.NoError(t, err)
	require.Equal(t, []int{1}, ids)
	ids[0] = 99

	ids, err = HomepageDivisionIDs(db, nil)
	require.NoError(t, err)
	require.Equal(t, []int{1}, ids)
}
