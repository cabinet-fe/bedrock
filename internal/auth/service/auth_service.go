package service

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"bedrock/internal/auth/model"
	"bedrock/internal/auth/repository"
	"bedrock/internal/pkg"
	"bedrock/internal/platform/config"
	rbacmodel "bedrock/internal/rbac/model"
	rbacservice "bedrock/internal/rbac/service"
)

// TokenPair holds access and refresh tokens.
type TokenPair struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
}

// Claims holds JWT claims.
type Claims struct {
	UserID       uint   `json:"user_id"`
	Username     string `json:"username"`
	IsSuperAdmin bool   `json:"is_super_admin"`
	jwt.RegisteredClaims
}

// AuthService handles JWT generation/parsing and login orchestration.
type AuthService struct {
	users      *repository.UserRepository
	perm       *rbacservice.PermissionService
	roles      *rbacservice.RoleService
	secret     []byte
	accessTTL  time.Duration
	refreshTTL time.Duration
	refreshKey []byte
}

func NewAuthService(cfg *config.Config, users *repository.UserRepository, perm *rbacservice.PermissionService, roles *rbacservice.RoleService) (*AuthService, error) {
	if cfg == nil || cfg.JWT.Secret == "" {
		return nil, fmt.Errorf("jwt secret is required")
	}
	secret := []byte(cfg.JWT.Secret)

	accessTTL := 15 * time.Minute
	if cfg.JWT.AccessTTL != "" {
		d, err := time.ParseDuration(cfg.JWT.AccessTTL)
		if err != nil {
			return nil, fmt.Errorf("invalid access_ttl: %w", err)
		}
		accessTTL = d
	}

	refreshTTL := 7 * 24 * time.Hour
	if cfg.JWT.RefreshTTL != "" {
		d, err := time.ParseDuration(cfg.JWT.RefreshTTL)
		if err != nil {
			return nil, fmt.Errorf("invalid refresh_ttl: %w", err)
		}
		refreshTTL = d
	}

	return &AuthService{
		users:      users,
		perm:       perm,
		roles:      roles,
		secret:     secret,
		accessTTL:  accessTTL,
		refreshTTL: refreshTTL,
		refreshKey: secret,
	}, nil
}

// RefreshTTL is the refresh token lifetime (also used as cookie Max-Age).
func (s *AuthService) RefreshTTL() time.Duration {
	return s.refreshTTL
}

func (s *AuthService) GenerateTokenPair(user *model.User) (accessToken, refreshToken string, err error) {
	now := time.Now()

	accessClaims := Claims{
		UserID:       user.ID,
		Username:     user.Username,
		IsSuperAdmin: user.IsSuperAdmin,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(now.Add(s.accessTTL)),
			IssuedAt:  jwt.NewNumericDate(now),
			NotBefore: jwt.NewNumericDate(now),
		},
	}
	accessTokenObj := jwt.NewWithClaims(jwt.SigningMethodHS256, accessClaims)
	accessToken, err = accessTokenObj.SignedString(s.secret)
	if err != nil {
		return "", "", err
	}

	refreshClaims := Claims{
		UserID:       user.ID,
		Username:     user.Username,
		IsSuperAdmin: user.IsSuperAdmin,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(now.Add(s.refreshTTL)),
			IssuedAt:  jwt.NewNumericDate(now),
			NotBefore: jwt.NewNumericDate(now),
		},
	}
	refreshTokenObj := jwt.NewWithClaims(jwt.SigningMethodHS256, refreshClaims)
	refreshToken, err = refreshTokenObj.SignedString(s.refreshKey)
	if err != nil {
		return "", "", err
	}
	return accessToken, refreshToken, nil
}

func (s *AuthService) ParseToken(tokenString string) (*Claims, error) {
	return s.parse(tokenString, s.secret)
}

func (s *AuthService) ParseRefreshToken(tokenString string) (*Claims, error) {
	return s.parse(tokenString, s.refreshKey)
}

func (s *AuthService) parse(tokenString string, key []byte) (*Claims, error) {
	token, err := jwt.ParseWithClaims(tokenString, &Claims{}, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return key, nil
	})
	if err != nil {
		return nil, err
	}
	claims, ok := token.Claims.(*Claims)
	if !ok || !token.Valid {
		return nil, fmt.Errorf("invalid token")
	}
	return claims, nil
}

func (s *AuthService) Authenticate(username, password string) (*model.User, error) {
	user, err := s.users.FindByUsername(username)
	if err != nil {
		return nil, errors.New("用户名或密码错误")
	}
	if !user.IsActive {
		return nil, errors.New("账户已被禁用")
	}
	if !pkg.CheckPassword(password, user.PasswordHash) {
		return nil, errors.New("用户名或密码错误")
	}
	return user, nil
}

// Register creates a self-service account bound to the chosen builtin role
// (data scope self). Login-strength rules only apply to new passwords.
func (s *AuthService) Register(username, password, roleCode string) (*model.User, error) {
	username = strings.TrimSpace(username)
	if n := len([]rune(username)); n < 3 || n > 50 {
		return nil, errors.New("用户名长度需在 3-50 个字符之间")
	}
	if len(password) < 8 {
		return nil, errors.New("密码至少 8 位")
	}
	if !rbacmodel.IsSelectableBuiltinRoleCode(strings.TrimSpace(roleCode)) {
		return nil, errors.New("无效的角色选择")
	}
	roleCode = strings.TrimSpace(roleCode)
	if _, err := s.users.FindByUsername(username); err == nil {
		return nil, errors.New("用户名已被占用")
	}
	hash, err := pkg.HashPassword(password)
	if err != nil {
		return nil, err
	}
	user := &model.User{
		Username:     username,
		PasswordHash: hash,
		DisplayName:  username,
		IsActive:     true,
	}
	if err := s.users.Create(user); err != nil {
		// uniqueIndex race on username may land here before the pre-check sees it.
		return nil, errors.New("创建用户失败")
	}
	if s.roles != nil {
		if err := s.roles.EnsureBuiltinRoleBound(user.ID, roleCode); err != nil {
			return nil, fmt.Errorf("绑定注册角色失败: %w", err)
		}
	}
	return user, nil
}

func (s *AuthService) GetByID(id uint) (*model.User, error) {
	return s.users.FindByID(id)
}

// MePayload is returned by GET /auth/me.
type MePayload struct {
	User        *model.User               `json:"user"`
	Permissions []string                  `json:"permissions"`
	Menus       []rbacmodel.MenuGroupNode `json:"menus"`
}

func (s *AuthService) Me(userID uint) (*MePayload, error) {
	user, err := s.users.FindByID(userID)
	if err != nil {
		return nil, err
	}
	if !user.IsActive {
		return nil, errors.New("账户已被禁用")
	}
	perms := []string{}
	menus := []rbacmodel.MenuGroupNode{}
	if s.perm != nil {
		perms, err = s.perm.ResolvePermissions(userID, user.IsSuperAdmin)
		if err != nil {
			return nil, err
		}
		menus, err = s.perm.TrimMenus(userID, user.IsSuperAdmin)
		if err != nil {
			return nil, err
		}
	}
	if perms == nil {
		perms = []string{}
	}
	if menus == nil {
		menus = []rbacmodel.MenuGroupNode{}
	}
	return &MePayload{
		User:        user,
		Permissions: perms,
		Menus:       menus,
	}, nil
}
