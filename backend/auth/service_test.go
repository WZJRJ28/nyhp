package auth

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestService_RegisterAndLogin(t *testing.T) {
	repo := newFakeRepository()
	svc := NewService(repo, "test-secret")

	req := RegisterRequest{
		Email:    "alice@example.com",
		Password: "supersafe",
		FullName: "Alice Agent",
	}

	ctx := context.Background()
	user, err := svc.Register(ctx, req)
	if err != nil {
		t.Fatalf("register: unexpected error: %v", err)
	}

	if user.Email != req.Email {
		t.Fatalf("expected email %q got %q", req.Email, user.Email)
	}
	if user.Role != RoleAgent {
		t.Fatalf("register: expected default role %s got %s", RoleAgent, user.Role)
	}

	resp, err := svc.Login(ctx, LoginRequest{Email: req.Email, Password: req.Password})
	if err != nil {
		t.Fatalf("login: unexpected error: %v", err)
	}
	if resp.Token == "" {
		t.Fatal("login: expected token, got empty string")
	}
	if resp.User.ID != user.ID {
		t.Fatalf("login: expected user id %q got %q", user.ID, resp.User.ID)
	}
	if resp.User.Role != RoleAgent {
		t.Fatalf("login: expected role %s got %s", RoleAgent, resp.User.Role)
	}

	tokenUserID, tokenRole, err := svc.VerifyToken(resp.Token)
	if err != nil {
		t.Fatalf("verify token: %v", err)
	}
	if tokenUserID != user.ID {
		t.Fatalf("verify token: expected %q got %q", user.ID, tokenUserID)
	}
	if tokenRole != RoleAgent {
		t.Fatalf("verify token: expected role %s got %s", RoleAgent, tokenRole)
	}
}

func TestService_RegisterValidation(t *testing.T) {
	repo := newFakeRepository()
	svc := NewService(repo, "test-secret")

	_, err := svc.Register(context.Background(), RegisterRequest{
		Email:    "alice@example.com",
		Password: "short",
		FullName: "Alice Agent",
	})
	if !errors.Is(err, ErrWeakPassword) {
		t.Fatalf("expected ErrWeakPassword, got %v", err)
	}

	if _, err := svc.Register(context.Background(), RegisterRequest{
		Email:    "",
		Password: "strongpassword",
		FullName: "",
	}); err == nil {
		t.Fatal("expected validation error for missing fields")
	}
}

func TestService_DuplicateEmail(t *testing.T) {
	repo := newFakeRepository()
	svc := NewService(repo, "test-secret")

	req := RegisterRequest{
		Email:    "alice@example.com",
		Password: "strongpassword",
		FullName: "Alice Agent",
	}
	if _, err := svc.Register(context.Background(), req); err != nil {
		t.Fatalf("first register failed: %v", err)
	}

	if _, err := svc.Register(context.Background(), req); !errors.Is(err, ErrDuplicateEmail) {
		t.Fatalf("expected ErrDuplicateEmail, got %v", err)
	}
}

func TestService_LoginInvalidCredentials(t *testing.T) {
	repo := newFakeRepository()
	svc := NewService(repo, "test-secret")

	_, err := svc.Login(context.Background(), LoginRequest{
		Email:    "unknown@example.com",
		Password: "irrelevant",
	})
	if !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("expected ErrInvalidCredentials, got %v", err)
	}
}

type fakeRepository struct {
	usersByEmail map[string]User
	usersByID    map[string]User
	nextID       int
}

func newFakeRepository() *fakeRepository {
	return &fakeRepository{
		usersByEmail: make(map[string]User),
		usersByID:    make(map[string]User),
		nextID:       1,
	}
}

func (f *fakeRepository) CreateUser(ctx context.Context, params CreateUserParams) (User, error) {
	if _, exists := f.usersByEmail[strings.ToLower(params.Email)]; exists {
		return User{}, ErrDuplicateEmail
	}

	id := fmt.Sprintf("user-%d", f.nextID)
	f.nextID++
	role := params.Role
	if role == "" {
		role = RoleAgent
	}

	user := User{
		ID:           id,
		Email:        params.Email,
		FullName:     params.FullName,
		PasswordHash: params.PasswordHash,
		Languages:    []string{},
		Role:         role,
		CreatedAt:    time.Now().UTC(),
		UpdatedAt:    time.Now().UTC(),
	}

	f.usersByEmail[strings.ToLower(user.Email)] = user
	f.usersByID[user.ID] = user

	return user, nil
}

func (f *fakeRepository) GetUserByEmail(ctx context.Context, email string) (User, error) {
	user, ok := f.usersByEmail[strings.ToLower(email)]
	if !ok {
		return User{}, ErrUserNotFound
	}
	return user, nil
}

func (f *fakeRepository) GetUserByID(ctx context.Context, userID string) (User, error) {
	user, ok := f.usersByID[userID]
	if !ok {
		return User{}, ErrUserNotFound
	}
	return user, nil
}
