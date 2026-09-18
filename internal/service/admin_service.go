// Package service 包含了应用的业务逻辑层。
package service

import (
	"context"
	"errors"
	"fmt"
	"github.com/ericthz/zebra-rag/internal/model"
	"github.com/ericthz/zebra-rag/internal/repository"
	"github.com/ericthz/zebra-rag/pkg/hash"
	"github.com/ericthz/zebra-rag/pkg/log"
	"strings"
	"time"

	"gorm.io/gorm"
)

// UserListResponse 定义了用户列表 API 的响应结构。
type UserListResponse struct {
	Content       []UserDetailResponse `json:"content"`
	TotalElements int64                `json:"totalElements"`
	TotalPages    int                  `json:"totalPages"`
	Size          int                  `json:"size"`
	Number        int                  `json:"number"`
}

// UserDetailResponse 定义了用户列表项的详细结构。
type UserDetailResponse struct {
	UserID     uint            `json:"userId"`
	Username   string          `json:"username"`
	Nickname   string          `json:"nickname"`
	Role       string          `json:"role"`
	Email      string          `json:"email"`
	Phone      string          `json:"phone"`
	OrgTags    []OrgTagDetail  `json:"orgTags"`
	PrimaryOrg string          `json:"primaryOrg"`
	Status     int             `json:"status"`
	CreatedAt  model.LocalTime `json:"createdAt"`
}

// OrgTagDetail 定义了组织标签的详细信息。
type OrgTagDetail struct {
	TagID string `json:"tagId"`
	Name  string `json:"name"`
}

// ConversationHistoryDTO 管理端按会话分组返回的对话记录。
type ConversationHistoryDTO struct {
	ConversationID string                   `json:"conversationId"`
	Username       string                   `json:"username"`
	Nickname       string                   `json:"nickname"`
	Title          string                   `json:"title"`
	UpdatedAt      string                   `json:"updatedAt"`
	MessageCount   int                      `json:"messageCount"`
	Messages       []map[string]interface{} `json:"messages"`
}

// AdminService 接口定义了所有管理员相关的业务操作。
type AdminService interface {
	// Organization Tag Management
	CreateOrganizationTag(tagID, name, description, parentTag string, creator *model.User) (*model.OrganizationTag, error)
	ListOrganizationTags() ([]model.OrganizationTag, error)
	GetOrganizationTagTree() ([]*model.OrganizationTagNode, error)
	UpdateOrganizationTag(tagID string, name, description, parentTag string) (*model.OrganizationTag, error)
	DeleteOrganizationTag(tagID string) error

	// User Management
	AssignOrgTagsToUser(userID uint, orgTags []string) error
	CreateUser(username, password, role string) (*model.User, error)
	DeleteUser(userID uint) error
	ListUsers(page, size int) (*UserListResponse, error)
	GetAllConversations(ctx context.Context, userID *uint, archived bool, startTime, endTime *time.Time) ([]ConversationHistoryDTO, error)
	DeleteConversation(ctx context.Context, userID uint, conversationID string) error
}

// adminService 是 AdminService 接口的实现。
type adminService struct {
	orgTagRepo       repository.OrgTagRepository
	userRepo         repository.UserRepository
	conversationRepo repository.ConversationRepository
}

// NewAdminService 创建一个新的 AdminService 实例。
func NewAdminService(orgTagRepo repository.OrgTagRepository, userRepo repository.UserRepository, conversationRepo repository.ConversationRepository) AdminService {
	return &adminService{
		orgTagRepo:       orgTagRepo,
		userRepo:         userRepo,
		conversationRepo: conversationRepo,
	}
}

// CreateOrganizationTag 处理创建新组织标签的逻辑。
func (s *adminService) CreateOrganizationTag(tagID, name, description, parentTag string, creator *model.User) (*model.OrganizationTag, error) {
	// 检查 TagID 是否已存在
	_, err := s.orgTagRepo.FindByID(tagID)
	if err == nil {
		// 如果 err 为 nil，说明找到了记录，因此 TagID 已存在
		return nil, errors.New("tagID 已存在")
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		// 如果是其他类型的数据库错误，则直接返回
		return nil, err
	}

	tag := &model.OrganizationTag{
		TagID:       tagID,
		Name:        name,
		Description: description,
		CreatedBy:   creator.ID,
	}
	if parentTag != "" {
		tag.ParentTag = &parentTag
	}

	if err := s.orgTagRepo.Create(tag); err != nil {
		return nil, err
	}
	return tag, nil
}

// GetOrganizationTagTree retrieves all tags and organizes them into a tree structure.
func (s *adminService) GetOrganizationTagTree() ([]*model.OrganizationTagNode, error) {
	tags, err := s.orgTagRepo.FindAll()
	if err != nil {
		return nil, err
	}

	nodes := make(map[string]*model.OrganizationTagNode)
	var tree []*model.OrganizationTagNode

	for _, tag := range tags {
		nodes[tag.TagID] = &model.OrganizationTagNode{
			TagID:       tag.TagID,
			Name:        tag.Name,
			Description: tag.Description,
			ParentTag:   tag.ParentTag,
			Children:    []*model.OrganizationTagNode{},
		}
	}

	for _, node := range nodes {
		if node.ParentTag != nil && *node.ParentTag != "" {
			if parent, ok := nodes[*node.ParentTag]; ok {
				parent.Children = append(parent.Children, node)
			}
		} else {
			tree = append(tree, node)
		}
	}
	return tree, nil
}

// ListOrganizationTags 返回所有组织标签的列表。
func (s *adminService) ListOrganizationTags() ([]model.OrganizationTag, error) {
	return s.orgTagRepo.FindAll()
}

// UpdateOrganizationTag updates an existing organization tag.
func (s *adminService) UpdateOrganizationTag(tagID string, name, description, parentTag string) (*model.OrganizationTag, error) {
	tag, err := s.orgTagRepo.FindByID(tagID)
	if err != nil {
		return nil, errors.New("tag not found")
	}

	tag.Name = name
	tag.Description = description
	if parentTag != "" {
		tag.ParentTag = &parentTag
	} else {
		tag.ParentTag = nil
	}

	if err := s.orgTagRepo.Update(tag); err != nil {
		return nil, err
	}
	return tag, nil
}

// DeleteOrganizationTag deletes an organization tag by its ID.
func (s *adminService) DeleteOrganizationTag(tagID string) error {
	return s.orgTagRepo.Delete(tagID)
}

// AssignOrgTagsToUser 为指定用户分配一组组织标签。
func (s *adminService) AssignOrgTagsToUser(userID uint, orgTags []string) error {
	user, err := s.userRepo.FindByID(userID)
	if err != nil {
		return err
	}
	user.OrgTags = strings.Join(orgTags, ",")
	return s.userRepo.Update(user)
}

// CreateUser 由管理员创建用户，默认角色为 USER。
func (s *adminService) CreateUser(username, password, role string) (*model.User, error) {
	if username == "" || password == "" {
		return nil, errors.New("用户名和密码不能为空")
	}
	if _, err := s.userRepo.FindByUsername(username); err == nil {
		return nil, errors.New("用户名已存在")
	}
	hashedPassword, err := hash.HashPassword(password)
	if err != nil {
		return nil, err
	}
	if role == "" {
		role = "USER"
	}
	if role != "USER" && role != "ADMIN" {
		return nil, errors.New("角色只能是 USER 或 ADMIN")
	}
	newUser := &model.User{
		Username: username,
		Password: hashedPassword,
		Role:     role,
	}
	if err := s.userRepo.Create(newUser); err != nil {
		return nil, err
	}
	privateTagID := "PRIVATE_" + username
	if _, err := s.orgTagRepo.FindByID(privateTagID); errors.Is(err, gorm.ErrRecordNotFound) {
		privateTag := &model.OrganizationTag{
			TagID:       privateTagID,
			Name:        username + "个人知识库",
			Description: "用户的个人组织标签，仅用户本人可访问",
			CreatedBy:   newUser.ID,
		}
		if err := s.orgTagRepo.Create(privateTag); err != nil {
			log.Errorf("[AdminService] 创建用户个人组织标签失败, username: %s, error: %v", username, err)
		}
	}
	return newUser, nil
}

// DeleteUser 由管理员删除用户及其个人组织标签。
func (s *adminService) DeleteUser(userID uint) error {
	user, err := s.userRepo.FindByID(userID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return errors.New("用户不存在")
		}
		return err
	}
	privateTagID := "PRIVATE_" + user.Username
	_ = s.orgTagRepo.Delete(privateTagID)
	return s.userRepo.Delete(userID)
}

// ListUsers 以分页的形式返回用户列表
func (s *adminService) ListUsers(page, size int) (*UserListResponse, error) {
	offset := (page - 1) * size
	users, total, err := s.userRepo.FindWithPagination(offset, size)
	if err != nil {
		return nil, err
	}

	var userResponses []UserDetailResponse
	for _, u := range users {
		// 获取组织标签详情
		orgTagDetails := make([]OrgTagDetail, 0) // 初始化为空数组，而不是 nil
		if u.OrgTags != "" {
			tagIDs := strings.Split(u.OrgTags, ",")
			for _, tagID := range tagIDs {
				tag, err := s.orgTagRepo.FindByID(tagID)
				if err != nil { // 忽略找不到的标签
					continue
				}
				orgTagDetails = append(orgTagDetails, OrgTagDetail{
					TagID: tag.TagID,
					Name:  tag.Name,
				})
			}
		}

		// 转换角色为状态码
		status := 1 // 默认为 USER
		if u.Role == "ADMIN" {
			status = 0
		}

		userResponses = append(userResponses, UserDetailResponse{
			UserID:     u.ID,
			Username:   u.Username,
			Nickname:   u.Nickname,
			Role:       u.Role,
			Email:      u.Email,
			Phone:      u.Phone,
			OrgTags:    orgTagDetails,
			PrimaryOrg: u.PrimaryOrg,
			Status:     status,
			CreatedAt:  model.LocalTime(u.CreatedAt),
		})
	}

	totalPages := 0
	if total > 0 && size > 0 {
		totalPages = (int(total) + size - 1) / size
	}

	response := &UserListResponse{
		Content:       userResponses,
		TotalElements: total,
		TotalPages:    totalPages,
		Size:          size,
		Number:        page,
	}
	return response, nil
}

// GetAllConversations 按会话分组返回指定用户或全部用户的对话记录，支持归档过滤与时间范围过滤。
func (s *adminService) GetAllConversations(ctx context.Context, userID *uint, archived bool, startTime, endTime *time.Time) ([]ConversationHistoryDTO, error) {
	var allConversations []ConversationHistoryDTO
	if userID != nil {
		user, err := s.userRepo.FindByID(*userID)
		if err != nil {
			return nil, errors.New("user not found")
		}
		return s.getConversationsForUser(ctx, user, archived, startTime, endTime)
	}

	mappings, err := s.conversationRepo.GetAllUserConversationMappings(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get user conversation mappings from redis: %w", err)
	}

	for uid := range mappings {
		user, err := s.userRepo.FindByID(uid)
		if err != nil {
			continue
		}
		userConversations, err := s.getConversationsForUser(ctx, user, archived, startTime, endTime)
		if err != nil {
			continue
		}
		allConversations = append(allConversations, userConversations...)
	}
	return allConversations, nil
}

func (s *adminService) getConversationsForUser(ctx context.Context, user *model.User, archived bool, startTime, endTime *time.Time) ([]ConversationHistoryDTO, error) {
	var conversations []repository.ConversationMeta
	var err error
	if archived {
		conversations, err = s.conversationRepo.ListArchivedConversations(ctx, user.ID)
	} else {
		conversations, err = s.conversationRepo.ListConversations(ctx, user.ID)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to list conversations: %w", err)
	}

	userConversations := make([]ConversationHistoryDTO, 0, len(conversations))
	for _, meta := range conversations {
		_, history, err := s.conversationRepo.GetConversation(ctx, user.ID, meta.ConversationID)
		if err != nil {
			continue
		}

		messages := make([]map[string]interface{}, 0, len(history))
		for _, msg := range history {
			// 时间过滤
			if startTime != nil && msg.Timestamp.Before(*startTime) {
				continue
			}
			if endTime != nil && msg.Timestamp.After(*endTime) {
				continue
			}
			messages = append(messages, map[string]interface{}{
				"role":      msg.Role,
				"content":   msg.Content,
				"timestamp": msg.Timestamp.Format("2006-01-02T15:04:05"),
			})
		}
		if len(messages) == 0 {
			continue
		}
		userConversations = append(userConversations, ConversationHistoryDTO{
			ConversationID: meta.ConversationID,
			Username:       user.Username,
			Nickname:       user.Nickname,
			Title:          meta.Title,
			UpdatedAt:      meta.UpdatedAt.Format("2006-01-02T15:04:05"),
			MessageCount:   meta.MessageCount,
			Messages:       messages,
		})
	}
	return userConversations, nil
}

// DeleteConversation 管理员删除指定用户的会话。
func (s *adminService) DeleteConversation(ctx context.Context, userID uint, conversationID string) error {
	return s.conversationRepo.DeleteConversation(ctx, userID, conversationID)
}
