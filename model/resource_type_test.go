package model

import (
	"strings"
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupResourceTypeTest(t *testing.T) {
	t.Helper()
	originalDB := DB
	t.Cleanup(func() { DB = originalDB })

	var err error
	DB, err = gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, DB.AutoMigrate(&ResourceType{}, &Channel{}, &Ability{}))
}

func TestResourceTypeCatalogAndChannelAssociation(t *testing.T) {
	setupResourceTypeTest(t)

	resourceType := ResourceType{Name: "  claude-bedrock  "}
	require.NoError(t, resourceType.Insert())
	assert.Equal(t, "claude-bedrock", resourceType.Name)

	unicodeName := ResourceType{Name: strings.Repeat("类", 128)}
	require.NoError(t, unicodeName.Insert())

	channel := Channel{
		Name:           "Claude Bedrock",
		Key:            "secret",
		Models:         "claude-sonnet",
		Group:          "default",
		ResourceTypeID: &resourceType.Id,
	}
	require.NoError(t, channel.Insert())

	channels, err := SearchChannels("Claude Bedrock", "default", "claude-sonnet", false)
	require.NoError(t, err)
	require.Len(t, channels, 1)
	assert.Equal(t, "claude-bedrock", channels[0].ResourceTypeName)

	stored, err := GetChannelById(channel.Id, true)
	require.NoError(t, err)
	stored.ResourceTypeID = nil
	require.NoError(t, stored.Update())

	var updated Channel
	require.NoError(t, DB.First(&updated, channel.Id).Error)
	assert.Nil(t, updated.ResourceTypeID)

	require.NoError(t, DeleteResourceType(resourceType.Id))
}

func TestDeleteResourceTypeRejectsAssignedCatalogEntry(t *testing.T) {
	setupResourceTypeTest(t)

	resourceType := ResourceType{Name: "claude-anthropic"}
	require.NoError(t, resourceType.Insert())
	channel := Channel{
		Name:           "Claude Anthropic",
		Key:            "secret",
		Models:         "claude-sonnet",
		Group:          "default",
		ResourceTypeID: &resourceType.Id,
	}
	require.NoError(t, DB.Create(&channel).Error)

	err := DeleteResourceType(resourceType.Id)
	require.ErrorIs(t, err, ErrResourceTypeReferenced)

	resourceTypes, err := ListResourceTypes()
	require.NoError(t, err)
	require.Len(t, resourceTypes, 1)
	assert.Equal(t, resourceType.Name, resourceTypes[0].Name)
}

func TestResourceTypeInsertRejectsBlankAndOverlongNames(t *testing.T) {
	setupResourceTypeTest(t)

	for _, name := range []string{"   ", strings.Repeat("a", 129)} {
		resourceType := ResourceType{Name: name}
		require.Error(t, resourceType.Insert())
	}
}
