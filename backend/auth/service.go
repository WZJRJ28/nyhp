package auth

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"
)

var (
	// ErrInvalidCredentials signals wrong email or password.
	ErrInvalidCredentials = errors.New("auth: invalid credentials")
	// ErrWeakPassword signals password doesn't meet requirements.
	ErrWeakPassword = errors.New("auth: password must be at least 8 characters")
)

// Service handles authentication business logic.
type Service struct {
	repo      Repository
	jwtSecret []byte
}

// LoginResult bundles the token and domain user returned after a successful login.
type LoginResult struct {
	Token string
	User  User
}

// NewService creates a new authentication service.
func NewService(repo Repository, jwtSecret string) *Service {
	return &Service{
		repo:      repo,
		jwtSecret: []byte(jwtSecret),
	}
}

// Register creates a new user account.
func (s *Service) Register(ctx context.Context, req RegisterRequest) (*User, error) {
	// Validate password strength
	if len(req.Password) < 8 {
		return nil, ErrWeakPassword
	}

	// Validate required fields
	if req.Email == "" || req.FullName == "" {
		return nil, fmt.Errorf("auth: email and full_name are required")
	}

	// Hash password
	passwordHash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		return nil, fmt.Errorf("auth: hash password: %w", err)
	}

	// Create user
	role := Role(strings.TrimSpace(string(req.Role)))
	if role == "" {
		role = RoleAgent
	}
	if !isValidRole(role) {
		return nil, fmt.Errorf("auth: invalid role %q", role)
	}

	user, err := s.repo.CreateUser(ctx, CreateUserParams{
		Email:        req.Email,
		FullName:     req.FullName,
		PasswordHash: string(passwordHash),
		Role:         role,
	})
	if err != nil {
		return nil, err
	}

	return &user, nil
}

// Login authenticates a user and returns a JWT token.
func (s *Service) Login(ctx context.Context, req LoginRequest) (LoginResult, error) {
	// Get user by email
	user, err := s.repo.GetUserByEmail(ctx, req.Email)
	if err != nil {
		if errors.Is(err, ErrUserNotFound) {
			return LoginResult{}, ErrInvalidCredentials
		}
		return LoginResult{}, err
	}

	// Verify password
	err = bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(req.Password))
	if err != nil {
		return LoginResult{}, ErrInvalidCredentials
	}

	// Generate JWT token
	token, err := s.generateToken(user.ID, user.Role)
	if err != nil {
		return LoginResult{}, fmt.Errorf("auth: generate token: %w", err)
	}

	return LoginResult{
		Token: token,
		User:  user,
	}, nil
}

// GetUserByID retrieves user information by ID.
func (s *Service) GetUserByID(ctx context.Context, userID string) (*User, error) {
	user, err := s.repo.GetUserByID(ctx, userID)
	if err != nil {
		return nil, err
	}
	return &user, nil
}

// VerifyToken validates a JWT token and returns the user ID.
func (s *Service) VerifyToken(tokenString string) (string, Role, error) {
	token, err := jwt.Parse(tokenString, func(token *jwt.Token) (interface{}, error) {
		// Validate signing method
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return s.jwtSecret, nil
	})

	if err != nil {
		return "", "", fmt.Errorf("auth: parse token: %w", err)
	}

	if claims, ok := token.Claims.(jwt.MapClaims); ok && token.Valid {
		userID, ok := claims["user_id"].(string)
		if !ok {
			return "", "", fmt.Errorf("auth: invalid user_id in token")
		}
		roleStr, ok := claims["role"].(string)
		if !ok {
			return "", "", fmt.Errorf("auth: invalid role in token")
		}
		role := Role(roleStr)
		if !isValidRole(role) {
			return "", "", fmt.Errorf("auth: invalid role %q in token", roleStr)
		}
		return userID, role, nil
	}

	return "", "", fmt.Errorf("auth: invalid token")
}

// generateToken creates a JWT token for the user.
func (s *Service) generateToken(userID string, role Role) (string, error) {
	claims := jwt.MapClaims{
		"user_id": userID,
		"role":    role,
		"exp":     time.Now().Add(24 * time.Hour).Unix(), // Token expires in 24 hours
		"iat":     time.Now().Unix(),
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenString, err := token.SignedString(s.jwtSecret)
	if err != nil {
		return "", err
	}

	return tokenString, nil
}

func isValidRole(role Role) bool {
	switch role {
	case RoleAgent, RoleBrokerAdmin, RoleClient:
		return true
	default:
		return false
	}
}
