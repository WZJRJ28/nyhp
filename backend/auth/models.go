package auth

import "time"

type Role string

const (
	RoleAgent       Role = "agent"
	RoleBrokerAdmin Role = "broker_admin"
	RoleClient      Role = "client"
)

// User is the domain representation of an authenticated user.
// It mirrors the users table and should not include JSON annotations so it
// can be reused by different presentation layers.
type User struct {
	ID           string
	Email        string
	FullName     string
	PasswordHash string
	Phone        *string
	Languages    []string
	BrokerID     *string
	Rating       float64
	Role         Role
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// RegisterRequest contains user registration data supplied by callers.
type RegisterRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
	FullName string `json:"full_name"`
	Role     Role   `json:"role"`
}

// LoginRequest contains user login credentials.
type LoginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}
