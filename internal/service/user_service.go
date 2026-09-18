// Package service 包含了应用的业务逻辑层。
package service

import (
	"context"
	"errors"
	"fmt"
	"github.com/ericthz/zebra-rag/internal/infra/database"
	"github.com/ericthz/zebra-rag/internal/model"
	"github.com/ericthz/zebra-rag/internal/repository"
	"github.com/ericthz/zebra-rag/pkg/hash"
	"github.com/ericthz/zebra-rag/pkg/log"
	"github.com/ericthz/zebra-rag/pkg/token"
	"strings"
	"time"

	"gorm.io/gorm"
)

// UserService 接口定义了所有与用户相关的业务操作。
type UserService interface {
	Register(username, password string) (*model.User, error)
	Login(username, password string) (accessToken, refreshToken string, err error)
	GetProfile(username string) (*model.User, error)
	Logout(tokenString string) error
	SetUserPrimaryOrg(username, orgTag string) error
	GetUserOrgTags(username string) (map[string]interface{}, error)
	GetUserEffectiveOrgTags(user *model.User) ([]string, error)
	RefreshToken(refreshTokenString string) (newAccessToken, newRefreshToken string, err error)
	UpdateProfile(userID uint, nickname, email, phone, bio, avatar string) error
	ChangePassword(userID uint, oldPassword, newPassword string) error
	GetUserStats(user *model.User) (*UserStatsDTO, error)
}

// UserStatsDTO 用户个人中心的概览统计数据。
type UserStatsDTO struct {
	ConversationCount         int64 `json:"conversationCount"`         // 未归档会话数
	ArchivedConversationCount int64 `json:"archivedConversationCount"` // 已归档会话数
	DocumentCount             int64 `json:"documentCount"`             // 本人上传且处理完成的文档数
	AccessibleDocumentCount   int64 `json:"accessibleDocumentCount"`   // 可访问文档数（本人 + 公开 + 组织）
}

// userService 是 UserService 接口的实现。
type userService struct {
	userRepo         repository.UserRepository
	orgTagRepo       repository.OrgTagRepository
	conversationRepo repository.ConversationRepository
	uploadRepo       repository.UploadRepository
	jwtManager       *token.JWTManager
}

// NewUserService 创建一个新的 UserService 实例。
func NewUserService(userRepo repository.UserRepository, orgTagRepo repository.OrgTagRepository, conversationRepo repository.ConversationRepository, uploadRepo repository.UploadRepository, jwtManager *token.JWTManager) UserService {
	return &userService{
		userRepo:         userRepo,
		orgTagRepo:       orgTagRepo,
		conversationRepo: conversationRepo,
		uploadRepo:       uploadRepo,
		jwtManager:       jwtManager,
	}
}

// Register 处理用户注册的业务逻辑。
func (s *userService) Register(username, password string) (*model.User, error) {
	// 1. 检查用户名是否已存在
	_, err := s.userRepo.FindByUsername(username)
	if err == nil {
		return nil, errors.New("用户名已存在")
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}

	// 2. 对密码进行哈希处理
	hashedPassword, err := hash.HashPassword(password)
	if err != nil {
		return nil, err
	}

	// 3. 创建新用户（不包含组织信息）
	newUser := &model.User{
		Username: username,
		Password: hashedPassword,
		Role:     "USER", // 默认角色
	}

	// 4. 将用户存入数据库以生成ID
	err = s.userRepo.Create(newUser)
	if err != nil {
		return nil, err
	}

	// 5. 创建用户的个人组织标签 (与Java逻辑对齐)
	privateTagId := "PRIVATE_" + username
	privateTagName := username + "个人知识库"

	// 检查个人标签是否已存在
	_, err = s.orgTagRepo.FindByID(privateTagId)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		// 不存在则创建
		privateTag := &model.OrganizationTag{
			TagID:       privateTagId,
			Name:        privateTagName,
			Description: "用户的个人组织标签，仅用户本人可访问",
			CreatedBy:   newUser.ID, // 将创建者的ID设置为新用户自己的ID
		}
		if err := s.orgTagRepo.Create(privateTag); err != nil {
			// 此处应有回滚逻辑，但为简化，我们先记录错误
			log.Errorf("[UserService] 创建个人组织标签失败, username: %s, error: %v", username, err)
			return nil, fmt.Errorf("创建个人组织标签失败: %w", err)
		}
	} else if err != nil {
		// 处理查询时发生的其他错误
		log.Errorf("[UserService] 查询个人组织标签失败, username: %s, error: %v", username, err)
		return nil, fmt.Errorf("查询个人组织标签失败: %w", err)
	}

	// 6. 更新用户信息，为其分配私有标签
	newUser.OrgTags = privateTagId
	newUser.PrimaryOrg = privateTagId
	if err := s.userRepo.Update(newUser); err != nil {
		log.Errorf("[UserService] 更新用户组织标签失败, username: %s, error: %v", username, err)
		return nil, fmt.Errorf("更新用户组织标签失败: %w", err)
	}

	return newUser, nil
}

// Login 处理用户登录的业务逻辑。
func (s *userService) Login(username, password string) (accessToken, refreshToken string, err error) {
	// 1. 查找用户
	user, err := s.userRepo.FindByUsername(username)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return "", "", errors.New("invalid credentials")
		}
		return "", "", err
	}

	// 2. 验证密码
	if !hash.CheckPasswordHash(password, user.Password) {
		return "", "", errors.New("invalid credentials")
	}

	// 3. 生成 access token 和 refresh token
	accessToken, err = s.jwtManager.GenerateToken(user.ID, user.Username, user.Role)
	if err != nil {
		return "", "", err
	}
	refreshToken, err = s.jwtManager.GenerateRefreshToken(user.ID, user.Username, user.Role)
	if err != nil {
		return "", "", err
	}

	return accessToken, refreshToken, nil
}

// GetProfile 根据用户名获取用户详细信息。
func (s *userService) GetProfile(username string) (*model.User, error) {
	user, err := s.userRepo.FindByUsername(username)
	if err != nil {
		return nil, err
	}
	return user, nil
}

// Logout 处理用户登出逻辑，将 token 加入 Redis 黑名单。
func (s *userService) Logout(tokenString string) error {
	claims, err := s.jwtManager.VerifyToken(tokenString)
	if err != nil {
		return err
	}
	// 使用 Redis 实现一个简单的 token 黑名单。
	// token 的剩余有效期将作为 Redis key 的过期时间。
	expiration := time.Until(claims.ExpiresAt.Time)
	// 将 token 存入黑名单，值为 "true"，并设置过期时间
	return database.RDB.Set(context.Background(), "blacklist:"+tokenString, "true", expiration).Err()
}

// SetUserPrimaryOrg 设置用户的主组织。
func (s *userService) SetUserPrimaryOrg(username, orgTag string) error {
	user, err := s.userRepo.FindByUsername(username)
	if err != nil {
		return err
	}
	// 简化的验证：检查用户是否拥有该组织标签。
	// 在生产环境中，这里可能需要更复杂的逻辑来验证标签的有效性。
	if !strings.Contains(user.OrgTags, orgTag) {
		return errors.New("user does not belong to this organization")
	}
	user.PrimaryOrg = orgTag
	return s.userRepo.Update(user)
}

// GetUserOrgTags 获取用户的组织标签信息。
func (s *userService) GetUserOrgTags(username string) (map[string]interface{}, error) {
	// 这是一个简化版本。在生产环境中，可能会连接查询 organization_tags 表以获取更详细信息。
	user, err := s.userRepo.FindByUsername(username)
	if err != nil {
		return nil, err
	}

	var orgTags []string
	if user.OrgTags != "" {
		orgTags = strings.Split(user.OrgTags, ",")
	} else {
		orgTags = make([]string, 0)
	}

	var orgTagDetails []map[string]string
	if len(orgTags) > 0 {
		for _, tagID := range orgTags {
			tag, err := s.orgTagRepo.FindByID(tagID)
			if err == nil { // 忽略查找失败的标签
				tagDetail := map[string]string{
					"tagId":       tag.TagID,
					"name":        tag.Name,
					"description": tag.Description,
				}
				orgTagDetails = append(orgTagDetails, tagDetail)
			}
		}
	} else {
		orgTagDetails = make([]map[string]string, 0)
	}

	result := map[string]interface{}{
		"orgTags":       orgTags,
		"primaryOrg":    user.PrimaryOrg,
		"orgTagDetails": orgTagDetails,
	}
	return result, nil
}

// GetUserEffectiveOrgTags 获取用户的所有有效组织标签 (包括层级)。
// 该实现与 Java 项目逻辑一致，会递归查询所有父级组织标签。
func (s *userService) GetUserEffectiveOrgTags(user *model.User) ([]string, error) {
	if user.OrgTags == "" {
		return []string{}, nil
	}

	// 1. 获取所有组织标签，以便在内存中构建层级关系，避免循环查询数据库
	allTags, err := s.orgTagRepo.FindAll()
	if err != nil {
		log.Errorf("[UserService] 获取所有组织标签失败: %v", err)
		return nil, fmt.Errorf("无法获取组织标签列表: %w", err)
	}

	// 2. 构建一个 TagID -> ParentTagID 的快速查找映射
	parentMap := make(map[string]*string)
	for _, tag := range allTags {
		parentMap[tag.TagID] = tag.ParentTag
	}

	// 3. 使用 map 来存储所有有效的标签ID，自动处理重复项
	effectiveTags := make(map[string]struct{})

	// 4. 初始化一个队列，用于广度优先搜索所有父标签
	initialTags := strings.Split(user.OrgTags, ",")
	queue := make([]string, 0, len(initialTags))

	for _, tagID := range initialTags {
		if _, exists := effectiveTags[tagID]; !exists {
			effectiveTags[tagID] = struct{}{}
			queue = append(queue, tagID)
		}
	}

	// 5. 开始向上遍历，查找所有父标签
	for len(queue) > 0 {
		// 出队
		currentTagID := queue[0]
		queue = queue[1:]

		// 在映射中查找父标签
		parentTagPtr, ok := parentMap[currentTagID]
		if ok && parentTagPtr != nil {
			parentTagID := *parentTagPtr
			// 如果父标签未被处理过，则加入结果集并入队
			if _, exists := effectiveTags[parentTagID]; !exists {
				effectiveTags[parentTagID] = struct{}{}
				queue = append(queue, parentTagID)
			}
		}
	}

	// 6. 将 map 的键转换为 string 切片
	result := make([]string, 0, len(effectiveTags))
	for tagID := range effectiveTags {
		result = append(result, tagID)
	}

	return result, nil
}

// RefreshToken 验证 refresh token 并签发新的 access token 和 refresh token。
func (s *userService) RefreshToken(refreshTokenString string) (newAccessToken, newRefreshToken string, err error) {
	// 1. 验证 refresh token 是否有效
	claims, err := s.jwtManager.VerifyToken(refreshTokenString)
	if err != nil {
		return "", "", errors.New("invalid refresh token")
	}

	// 2. 检查用户是否存在
	user, err := s.userRepo.FindByUsername(claims.Username)
	if err != nil {
		return "", "", errors.New("user not found")
	}

	// 3. 签发新的 token
	newAccessToken, err = s.jwtManager.GenerateToken(user.ID, user.Username, user.Role)
	if err != nil {
		return "", "", err
	}
	newRefreshToken, err = s.jwtManager.GenerateRefreshToken(user.ID, user.Username, user.Role)
	if err != nil {
		return "", "", err
	}

	return newAccessToken, newRefreshToken, nil
}

// UpdateProfile 更新当前用户的个人资料字段。
func (s *userService) UpdateProfile(userID uint, nickname, email, phone, bio, avatar string) error {
	if _, err := s.userRepo.FindByID(userID); err != nil {
		return errors.New("用户不存在")
	}
	return s.userRepo.UpdateProfile(userID, strings.TrimSpace(nickname), strings.TrimSpace(email), strings.TrimSpace(phone), strings.TrimSpace(bio), strings.TrimSpace(avatar))
}

// ChangePassword 校验旧密码并更新密码。
func (s *userService) ChangePassword(userID uint, oldPassword, newPassword string) error {
	user, err := s.userRepo.FindByID(userID)
	if err != nil {
		return errors.New("用户不存在")
	}
	if !hash.CheckPasswordHash(oldPassword, user.Password) {
		return errors.New("原密码不正确")
	}
	if len(newPassword) < 6 {
		return errors.New("新密码长度至少为 6 位")
	}
	if newPassword == oldPassword {
		return errors.New("新密码不能与原密码相同")
	}
	hashedPassword, err := hash.HashPassword(newPassword)
	if err != nil {
		return err
	}
	return s.userRepo.UpdatePassword(userID, hashedPassword)
}

// GetUserStats 统计当前用户的会话与文档数据，用于个人中心概览。
func (s *userService) GetUserStats(user *model.User) (*UserStatsDTO, error) {
	activeConversations, err := s.conversationRepo.ListConversations(context.Background(), user.ID)
	if err != nil {
		return nil, fmt.Errorf("获取会话列表失败: %w", err)
	}
	archivedConversations, err := s.conversationRepo.ListArchivedConversations(context.Background(), user.ID)
	if err != nil {
		return nil, fmt.Errorf("获取归档会话列表失败: %w", err)
	}

	effectiveTags := make([]string, 0)
	if user.OrgTags != "" {
		for _, tag := range strings.Split(user.OrgTags, ",") {
			if tag != "" {
				effectiveTags = append(effectiveTags, tag)
			}
		}
	}

	documentCount, err := s.uploadRepo.CountFilesByUserID(user.ID)
	if err != nil {
		return nil, fmt.Errorf("统计文档数量失败: %w", err)
	}
	accessibleDocumentCount, err := s.uploadRepo.CountAccessibleFiles(user.ID, effectiveTags)
	if err != nil {
		return nil, fmt.Errorf("统计可访问文档数量失败: %w", err)
	}

	return &UserStatsDTO{
		ConversationCount:         int64(len(activeConversations)),
		ArchivedConversationCount: int64(len(archivedConversations)),
		DocumentCount:             documentCount,
		AccessibleDocumentCount:   accessibleDocumentCount,
	}, nil
}
