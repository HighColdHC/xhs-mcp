package main

import (
	"github.com/gin-gonic/gin"
	"github.com/xpzouying/xiaohongshu-mcp/license"
)

// licenseManager 全局授权管理器
var licenseManager *license.Manager

// initLicenseManager 初始化授权管理器
func initLicenseManager(dataDir string) {
	licenseManager = license.NewManager(dataDir)
}

// handleLicenseStatus 获取授权状态
func handleLicenseStatus(c *gin.Context) {
	status := licenseManager.GetStatus()
	respondSuccess(c, status, "")
}

// handleLicenseActivate 使用卡密激活
func handleLicenseActivate(c *gin.Context) {
	var req struct {
		Key string `json:"key" binding:"required"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(200, gin.H{
			"success": false,
			"data":    nil,
			"message": "请求参数错误",
		})
		return
	}

	if err := licenseManager.Activate(req.Key); err != nil {
		c.JSON(200, gin.H{
			"success": false,
			"data":    nil,
			"message": err.Error(),
		})
		return
	}

	status := licenseManager.GetStatus()
	respondSuccess(c, status, "激活成功")
}

// handleMachineID 获取机器码
func handleMachineID(c *gin.Context) {
	machineID, err := license.GetMachineID()
	if err != nil {
		c.JSON(200, gin.H{
			"success": false,
			"data":    nil,
			"message": "获取机器码失败: " + err.Error(),
		})
		return
	}

	respondSuccess(c, map[string]string{
		"machine_id": machineID,
	}, "")
}

// registerLicenseRoutes 注册授权相关路由
func (s *AppServer) registerLicenseRoutes(r *gin.RouterGroup) {
	license := r.Group("/license")
	{
		license.GET("/status", handleLicenseStatus)
		license.POST("/activate", handleLicenseActivate)
		license.GET("/machine-id", handleMachineID)
	}
}
