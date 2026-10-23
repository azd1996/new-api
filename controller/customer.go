package controller

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"

	"github.com/gin-gonic/gin"
)

func GetAllCustomers(c *gin.Context) {
	pageInfo := common.GetPageQuery(c)
	customers, total, err := model.GetAllCustomers(pageInfo.GetStartIdx(), pageInfo.GetPageSize())
	if err != nil {
		common.ApiError(c, err)
		return
	}
	pageInfo.SetTotal(int(total))
	pageInfo.SetItems(customers)
	common.ApiSuccess(c, pageInfo)
}

func SearchCustomers(c *gin.Context) {
	keyword := c.Query("keyword")
	pageInfo := common.GetPageQuery(c)
	customers, total, err := model.SearchCustomers(keyword, pageInfo.GetStartIdx(), pageInfo.GetPageSize())
	if err != nil {
		common.ApiError(c, err)
		return
	}
	pageInfo.SetTotal(int(total))
	pageInfo.SetItems(customers)
	common.ApiSuccess(c, pageInfo)
}

func GetCustomer(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		common.ApiError(c, err)
		return
	}
	customer, err := model.GetCustomerById(id)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "", "data": customer})
}

func AddCustomer(c *gin.Context) {
	customer := model.Customer{}
	if err := c.ShouldBindJSON(&customer); err != nil {
		common.ApiError(c, err)
		return
	}
	customer.Id = 0
	if err := customer.Insert(); err != nil {
		common.ApiError(c, err)
		return
	}
	recordManageAudit(c, "customer.create", map[string]interface{}{
		"customer_id": customer.CustomerId,
		"name":        customer.Name,
	})
	common.ApiSuccess(c, customer)
}

func UpdateCustomer(c *gin.Context) {
	customer := model.Customer{}
	if err := c.ShouldBindJSON(&customer); err != nil {
		common.ApiError(c, err)
		return
	}
	if customer.Id == 0 {
		common.ApiError(c, errors.New("id 为空！"))
		return
	}
	if err := customer.Update(); err != nil {
		common.ApiError(c, err)
		return
	}
	recordManageAudit(c, "customer.update", map[string]interface{}{
		"customer_id": customer.CustomerId,
		"name":        customer.Name,
	})
	common.ApiSuccess(c, customer)
}

func DeleteCustomer(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if err := model.DeleteCustomerById(id); err != nil {
		common.ApiError(c, err)
		return
	}
	recordManageAudit(c, "customer.delete", map[string]interface{}{"id": id})
	common.ApiSuccess(c, nil)
}
