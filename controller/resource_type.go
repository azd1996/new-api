package controller

import (
	"errors"
	"net/http"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
)

func GetResourceTypes(c *gin.Context) {
	resourceTypes, err := model.ListResourceTypes()
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, resourceTypes)
}

func CreateResourceType(c *gin.Context) {
	var resourceType model.ResourceType
	if err := common.DecodeJson(c.Request.Body, &resourceType); err != nil {
		common.ApiError(c, err)
		return
	}
	if err := resourceType.Insert(); err != nil {
		common.ApiError(c, err)
		return
	}
	recordManageAudit(c, "channel.resource_type.create", map[string]any{
		"resource_type_id": resourceType.Id,
		"name":             resourceType.Name,
	})
	common.ApiSuccess(c, &resourceType)
}

func DeleteResourceType(c *gin.Context) {
	var request struct {
		ID int `uri:"id" binding:"required,min=1"`
	}
	if err := c.ShouldBindUri(&request); err != nil {
		common.ApiError(c, err)
		return
	}
	if err := model.DeleteResourceType(request.ID); err != nil {
		if errors.Is(err, model.ErrResourceTypeReferenced) {
			c.JSON(http.StatusConflict, gin.H{"success": false, "message": err.Error()})
			return
		}
		common.ApiError(c, err)
		return
	}
	recordManageAudit(c, "channel.resource_type.delete", map[string]any{"resource_type_id": request.ID})
	common.ApiSuccess(c, nil)
}
