// Package user 提供用户域相关的 HTTP 处理器与路由注册。
package user

import (
	"github.com/ericthz/zebra-rag/internal/service"
	"github.com/ericthz/zebra-rag/pkg/log"
	"net/http"
	"strings"
	"time"

	"github.com/ericthz/zebra-rag/internal/model"

	"github.com/gin-gonic/gin"
)

// UserHandler 负责处理所有与普通用户相关的 API 请求。
type UserHandler struct {
	userService service.UserService
}

// NewUserHandler 创建一个新的 UserHandler 实例。
func NewUserHandler(userService service.UserService) *UserHandler {
	return &UserHandler{userService: userService}
}

// RegisterRequest 定义了用户注册 API 的请求体结构。
type RegisterRequest struct {
	Username string `json:"username" binding:"required"`
	Password string `json:"password" binding:"required"`
}

// Register 处理用户注册请求。
func (h *UserHandler) Register(c *gin.Context) {
	var req RegisterRequest
	// 绑定并验证 JSON 请求体
	if err := c.ShouldBindJSON(&req); err != nil {
		log.Warnf("Register: Invalid request payload, error: %v", err)
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    http.StatusBadRequest,
			"message": "无效的请求负载：用户名和密码不能为空",
		})
		return
	}

	// 调用 service 层执行注册逻辑
	user, err := h.userService.Register(req.Username, req.Password)
	if err != nil {
		log.Warnf("Register: User registration failed for '%s', error: %v", req.Username, err)
		c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
		return
	}

	log.Infof("User '%s' registered successfully", user.Username)
	c.JSON(http.StatusOK, gin.H{
		"code":    http.StatusOK,
		"message": "User registered successfully",
	})
}

// LoginRequest 定义了用户登录 API 的请求体结构。
type LoginRequest struct {
	Username string `json:"username" binding:"required"`
	Password string `json:"password" binding:"required"`
}

// Login 处理用户登录请求。
func (h *UserHandler) Login(c *gin.Context) {
	var req LoginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		log.Warnf("Login: Invalid request payload, error: %v", err)
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    http.StatusBadRequest,
			"message": "无效的请求负载：用户名和密码不能为空",
		})
		return
	}

	// 调用 service 层执行登录逻辑
	accessToken, refreshToken, err := h.userService.Login(req.Username, req.Password)
	if err != nil {
		log.Warnf("Login: User authentication failed for '%s', error: %v", req.Username, err)
		c.JSON(http.StatusUnauthorized, gin.H{
			"code":    http.StatusUnauthorized,
			"message": "无效的凭证",
		})
		return
	}

	log.Infof("User '%s' logged in successfully", req.Username)
	c.JSON(http.StatusOK, gin.H{
		"code":    http.StatusOK,
		"message": "Login successful",
		"data": gin.H{
			"token":        accessToken,
			"refreshToken": refreshToken,
		},
	})
}

// ProfileResponse 定义了获取用户个人信息 API 的响应体结构。
type ProfileResponse struct {
	ID         uint      `json:"id"`
	Username   string    `json:"username"`
	Role       string    `json:"role"`
	OrgTags    []string  `json:"orgTags"`
	PrimaryOrg string    `json:"primaryOrg"`
	CreatedAt  time.Time `json:"createdAt"`
	UpdatedAt  time.Time `json:"updatedAt"`
}

// GetProfile 获取当前登录用户的个人信息。
// 用户信息已经由 AuthMiddleware 注入到上下文中。
func (h *UserHandler) GetProfile(c *gin.Context) {
	// 从上下文中获取由 AuthMiddleware 注入的 User 对象
	user, exists := c.Get("user")
	if !exists {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "无法获取用户信息"})
		return
	}

	// 直接返回用户信息
	c.JSON(http.StatusOK, gin.H{"code": http.StatusOK, "data": user, "message": "success"})
}

// Logout 处理用户登出逻辑。
func (h *UserHandler) Logout(c *gin.Context) {
	authHeader := c.GetHeader("Authorization")
	tokenString := strings.TrimPrefix(authHeader, "Bearer ")

	err := h.userService.Logout(tokenString)
	if err != nil {
		log.Error("Logout: Failed to logout", err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    http.StatusInternalServerError,
			"message": "登出失败",
		})
		return
	}

	userValue, _ := c.Get("user")
	user := userValue.(*model.User)
	log.Infof("User '%s' logged out successfully", user.Username)
	c.JSON(http.StatusOK, gin.H{
		"code":    http.StatusOK,
		"message": "登出成功"})
}

// SetPrimaryOrgRequest 定义了设置主组织 API 的请求体结构。
type SetPrimaryOrgRequest struct {
	PrimaryOrg string `json:"primaryOrg" binding:"required"`
}

// SetPrimaryOrg 处理设置用户主组织的请求。
func (h *UserHandler) SetPrimaryOrg(c *gin.Context) {
	var req SetPrimaryOrgRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		log.Warnf("SetPrimaryOrg: Invalid request payload, error: %v", err)
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    http.StatusBadRequest,
			"message": "无效的请求负载",
		})
		return
	}

	// 优先从上下文中获取完整的用户对象（由 AuthMiddleware 注入）
	userValue, ok := c.Get("user")
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{
			"code":    http.StatusUnauthorized,
			"message": "未认证用户或无法获取用户信息",
		})
		return
	}
	user, ok := userValue.(*model.User)
	if !ok || user == nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    http.StatusInternalServerError,
			"message": "用户数据类型错误",
		})
		return
	}
	err := h.userService.SetUserPrimaryOrg(user.Username, req.PrimaryOrg)
	if err != nil {
		log.Warnf("SetPrimaryOrg: Failed for user '%s', error: %v", user.Username, err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    http.StatusInternalServerError,
			"message": err.Error(),
		})
		return
	}
	log.Infof("User '%s' set primary organization to '%s'", user.Username, req.PrimaryOrg)
	c.JSON(http.StatusOK, gin.H{
		"code":    http.StatusOK,
		"message": "主组织更新成功",
	})
}

// GetUserOrgTags 获取用户的组织标签信息。
func (h *UserHandler) GetUserOrgTags(c *gin.Context) {
	// 从上下文中获取由 AuthMiddleware 注入的 User 对象
	userValue, exists := c.Get("user")
	if !exists {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "无法获取用户信息"})
		return
	}

	user, ok := userValue.(*model.User)
	if !ok {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "用户数据类型错误"})
		return
	}

	tags, err := h.userService.GetUserOrgTags(user.Username)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": http.StatusOK, "data": tags, "message": "success"})
}

// UpdateProfileRequest 定义了更新个人资料 API 的请求体结构。
type UpdateProfileRequest struct {
	Nickname string `json:"nickname"`
	Email    string `json:"email"`
	Phone    string `json:"phone"`
	Bio      string `json:"bio"`
	Avatar   string `json:"avatar"`
}

// UpdateProfile 处理更新个人资料的请求。
func (h *UserHandler) UpdateProfile(c *gin.Context) {
	var req UpdateProfileRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": http.StatusBadRequest, "message": "无效的请求负载"})
		return
	}

	userValue, ok := c.Get("user")
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"code": http.StatusUnauthorized, "message": "未认证用户"})
		return
	}
	user, ok := userValue.(*model.User)
	if !ok || user == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": http.StatusInternalServerError, "message": "用户数据类型错误"})
		return
	}

	if err := h.userService.UpdateProfile(user.ID, req.Nickname, req.Email, req.Phone, req.Bio, req.Avatar); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": http.StatusBadRequest, "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": http.StatusOK, "message": "个人资料更新成功"})
}

// ChangePasswordRequest 定义了修改密码 API 的请求体结构。
type ChangePasswordRequest struct {
	OldPassword string `json:"oldPassword" binding:"required"`
	NewPassword string `json:"newPassword" binding:"required"`
}

// ChangePassword 处理修改密码的请求。
func (h *UserHandler) ChangePassword(c *gin.Context) {
	var req ChangePasswordRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": http.StatusBadRequest, "message": "无效的请求负载"})
		return
	}

	userValue, ok := c.Get("user")
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"code": http.StatusUnauthorized, "message": "未认证用户"})
		return
	}
	user, ok := userValue.(*model.User)
	if !ok || user == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": http.StatusInternalServerError, "message": "用户数据类型错误"})
		return
	}

	if err := h.userService.ChangePassword(user.ID, req.OldPassword, req.NewPassword); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": http.StatusBadRequest, "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": http.StatusOK, "message": "密码修改成功，请重新登录"})
}

// GetUserStats 获取当前用户的个人数据概览统计。
func (h *UserHandler) GetUserStats(c *gin.Context) {
	userValue, ok := c.Get("user")
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"code": http.StatusUnauthorized, "message": "未认证用户"})
		return
	}
	user, ok := userValue.(*model.User)
	if !ok || user == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": http.StatusInternalServerError, "message": "用户数据类型错误"})
		return
	}

	stats, err := h.userService.GetUserStats(user)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": http.StatusInternalServerError, "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": http.StatusOK, "data": stats, "message": "success"})
}
