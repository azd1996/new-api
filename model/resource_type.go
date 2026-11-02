package model

import (
	"errors"
	"strings"
	"unicode/utf8"

	"gorm.io/gorm"
)

var ErrResourceTypeReferenced = errors.New("resource type is assigned to one or more channels")

type ResourceType struct {
	Id   int    `json:"id"`
	Name string `json:"name" gorm:"size:128;not null;uniqueIndex"`
}

func ListResourceTypes() ([]ResourceType, error) {
	var resourceTypes []ResourceType
	err := DB.Order("name ASC").Find(&resourceTypes).Error
	return resourceTypes, err
}

func (resourceType *ResourceType) Insert() error {
	resourceType.Name = strings.TrimSpace(resourceType.Name)
	if resourceType.Name == "" || utf8.RuneCountInString(resourceType.Name) > 128 {
		return errors.New("resource type name must be between 1 and 128 characters")
	}
	resourceType.Id = 0
	return DB.Create(resourceType).Error
}

func DeleteResourceType(id int) error {
	var count int64
	if err := DB.Model(&Channel{}).Where("resource_type_id = ?", id).Count(&count).Error; err != nil {
		return err
	}
	if count > 0 {
		return ErrResourceTypeReferenced
	}
	return DB.Delete(&ResourceType{}, id).Error
}

func (channel *Channel) validateResourceType() error {
	if channel.ResourceTypeID == nil || *channel.ResourceTypeID == 0 {
		channel.ResourceTypeID = nil
		return nil
	}
	var count int64
	if err := DB.Model(&ResourceType{}).Where("id = ?", *channel.ResourceTypeID).Count(&count).Error; err != nil {
		return err
	}
	if count == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}
