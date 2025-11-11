package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"brokerflow/agreement"
	"brokerflow/auth"
	"brokerflow/broker"
	"brokerflow/db"
	"brokerflow/dispute"
	"brokerflow/referral"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Server struct {
	pool             *pgxpool.Pool
	agreementService *agreement.Service
	agreementCRUD    *agreement.CRUDService
	agreementStatus  *agreement.StatusService
	authService      *auth.Service
	referralService  *referral.Service
	brokerService    *broker.Service
	matchService     matchService
	disputeService   disputeService
}

type matchService interface {
	List(ctx context.Context, requestID, ownerID string) ([]referral.Match, error)
	Create(ctx context.Context, params referral.CreateMatchParams) (referral.Match, error)
	ListForCandidate(ctx context.Context, candidateID string) ([]referral.Match, error)
	UpdateState(ctx context.Context, params referral.UpdateMatchParams) (referral.MatchUpdateResult, error)
}

type disputeService interface {
	List(ctx context.Context, ownerID, agreementID string) ([]dispute.Record, error)
	Create(ctx context.Context, ownerID, agreementID string) (dispute.Record, error)
	Resolve(ctx context.Context, ownerID, disputeID string) (dispute.Record, error)
}

type ctxKey string

const (
	ctxKeyUserID   ctxKey = "user_id"
	ctxKeyRole     ctxKey = "user_role"
	requestTimeout        = 5 * time.Second
)

func main() {
	ctx := context.Background()

	// 数据库连接
	connString := os.Getenv("DATABASE_URL")
	if connString == "" {
		connString = "postgresql://postgres:postgres@localhost:5432/brokerflow_test?sslmode=disable"
	}

	pool, err := db.NewPool(ctx, connString)
	if err != nil {
		log.Fatalf("bootstrap database pool: %v", err)
	}
	defer pool.Close()

	wd, err := os.Getwd()
	if err != nil {
		log.Fatalf("determine working directory: %v", err)
	}
	migrationsDir := filepath.Join(wd, "migrations")
	if err := ensureSchema(ctx, pool, migrationsDir); err != nil {
		log.Fatalf("apply migrations: %v", err)
	}

	// 初始化服务
	agreementRepo := agreement.NewRepository()
	agreementService := agreement.NewService(pool, agreementRepo)
	agreementCRUD := agreement.NewCRUDService(pool)
	agreementStatus := agreement.NewStatusService(pool)
	referralRepo := referral.NewRepository(pool)
	referralService := referral.NewService(pool, referralRepo, nil, nil)
	authRepo := auth.NewRepository(pool)
	brokerRepo := broker.NewRepository(pool)
	brokerService := broker.NewService(brokerRepo)
	matchRepo := referral.NewMatchRepository(pool)
	matchService := referral.NewMatchService(matchRepo).
		WithAgreementRepository(agreementRepo)
	disputeRepo := dispute.NewRepository(pool)
	disputeService := dispute.NewService(disputeRepo)
	jwtSecret := os.Getenv("JWT_SECRET")
	if jwtSecret == "" {
		jwtSecret = "dev-secret-key-change-in-production"
	}
	authService := auth.NewService(authRepo, jwtSecret)

	server := &Server{
		pool:             pool,
		agreementService: agreementService,
		agreementCRUD:    agreementCRUD,
		agreementStatus:  agreementStatus,
		authService:      authService,
		referralService:  referralService,
		brokerService:    brokerService,
		matchService:     matchService,
		disputeService:   disputeService,
	}

	// 路由
	mux := http.NewServeMux()

	// 认证接口（匹配前端路径）
	mux.HandleFunc("/auth/register", server.handleRegister)
	mux.HandleFunc("/auth/login", server.handleLogin)
	mux.HandleFunc("/api/me", server.authMiddleware(server.handleMe))
	mux.HandleFunc("/api/referrals", server.authMiddleware(server.handleReferrals))
	mux.HandleFunc("/api/referrals/", server.authMiddleware(server.handleReferralDetail))
	mux.HandleFunc("/api/matches", server.authMiddleware(server.handleCandidateMatches))
	mux.HandleFunc("/api/agreements", server.authMiddleware(server.handleAgreements))
	mux.HandleFunc("/api/events", server.authMiddleware(server.handleTimelineEvents))
	mux.HandleFunc("/api/brokers", server.authMiddleware(server.handleBrokers))
	mux.HandleFunc("/api/brokers/", server.authMiddleware(server.handleBroker))
	mux.HandleFunc("/api/disputes", server.authMiddleware(server.handleDisputes))
	mux.HandleFunc("/api/disputes/", server.authMiddleware(server.handleDisputeDetail))

	// CORS 中间件
	handler := loggingMiddleware(corsMiddleware(mux))

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	log.Printf("🚀 Server starting on http://localhost:%s", port)
	log.Printf("📝 Auth endpoints:")
	log.Printf("   POST /auth/register")
	log.Printf("   POST /auth/login")
	log.Printf("   GET  /api/me")

	if err := http.ListenAndServe(":"+port, handler); err != nil {
		log.Fatalf("server failed: %v", err)
	}
}

// handleRegister 处理用户注册
func (s *Server) handleRegister(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req auth.RegisterRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), requestTimeout)
	defer cancel()

	user, err := s.authService.Register(ctx, req)
	if err != nil {
		log.Printf("Register error: %v", err)
		if err == auth.ErrDuplicateEmail {
			respondError(w, http.StatusConflict, "Email already exists")
			return
		}
		if err == auth.ErrWeakPassword {
			respondError(w, http.StatusBadRequest, err.Error())
			return
		}
		respondError(w, http.StatusInternalServerError, "Registration failed")
		return
	}

	respondJSON(w, http.StatusCreated, map[string]any{
		"user": newAgentResponse(*user),
	})
}

// handleLogin 处理用户登录
func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req auth.LoginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), requestTimeout)
	defer cancel()

	resp, err := s.authService.Login(ctx, req)
	if err != nil {
		if err == auth.ErrInvalidCredentials {
			respondError(w, http.StatusUnauthorized, "Invalid credentials")
			return
		}
		respondError(w, http.StatusInternalServerError, "Login failed")
		return
	}

	respondJSON(w, http.StatusOK, map[string]any{
		"token": resp.Token,
		"user":  newAgentResponse(resp.User),
	})
}

// handleMe 获取当前用户信息
func (s *Server) handleMe(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	userID, ok := r.Context().Value(ctxKeyUserID).(string)
	if !ok || userID == "" {
		respondError(w, http.StatusUnauthorized, "Invalid authentication context")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), requestTimeout)
	defer cancel()

	user, err := s.authService.GetUserByID(ctx, userID)
	if err != nil {
		if errors.Is(err, auth.ErrUserNotFound) {
			respondError(w, http.StatusNotFound, "User not found")
			return
		}
		respondError(w, http.StatusNotFound, "User not found")
		return
	}

	respondJSON(w, http.StatusOK, newAgentResponse(*user))
}

// authMiddleware JWT 认证中间件
func (s *Server) authMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		authHeader := r.Header.Get("Authorization")
		if authHeader == "" {
			respondError(w, http.StatusUnauthorized, "Missing authorization header")
			return
		}

		parts := strings.Split(authHeader, " ")
		if len(parts) != 2 || parts[0] != "Bearer" {
			respondError(w, http.StatusUnauthorized, "Invalid authorization header")
			return
		}

		token := parts[1]
		userID, role, err := s.authService.VerifyToken(token)
		if err != nil {
			respondError(w, http.StatusUnauthorized, "Invalid token")
			return
		}

		ctx := context.WithValue(r.Context(), ctxKeyUserID, userID)
		ctx = context.WithValue(ctx, ctxKeyRole, role)
		next(w, r.WithContext(ctx))
	}
}

// corsMiddleware CORS 中间件
func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusOK)
			return
		}

		next.ServeHTTP(w, r)
	})
}

// respondJSON 返回 JSON 响应
func respondJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}

// respondError 返回错误响应
func respondError(w http.ResponseWriter, status int, message string) {
	log.Printf("HTTP error: status=%d message=%s", status, message)
	respondJSON(w, status, map[string]string{"message": message})
}

func loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		lrw := &loggingResponseWriter{ResponseWriter: w, statusCode: http.StatusOK}
		next.ServeHTTP(lrw, r)
		duration := time.Since(start)
		log.Printf("HTTP %s %s -> %d (%s)", r.Method, r.URL.Path, lrw.statusCode, duration)
	})
}

type loggingResponseWriter struct {
	http.ResponseWriter
	statusCode int
}

func (lrw *loggingResponseWriter) WriteHeader(code int) {
	lrw.statusCode = code
	lrw.ResponseWriter.WriteHeader(code)
}

type agentResponse struct {
	ID        string    `json:"id"`
	FullName  string    `json:"fullName"`
	Email     string    `json:"email"`
	Phone     string    `json:"phone"`
	Languages []string  `json:"languages"`
	BrokerID  string    `json:"brokerId"`
	Rating    float64   `json:"rating"`
	Role      auth.Role `json:"role"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

func newAgentResponse(u auth.User) agentResponse {
	phone := ""
	if u.Phone != nil {
		phone = *u.Phone
	}
	brokerID := ""
	if u.BrokerID != nil {
		brokerID = *u.BrokerID
	}

	return agentResponse{
		ID:        u.ID,
		FullName:  u.FullName,
		Email:     u.Email,
		Phone:     phone,
		Languages: append([]string(nil), u.Languages...),
		BrokerID:  brokerID,
		Rating:    u.Rating,
		Role:      u.Role,
		CreatedAt: u.CreatedAt,
		UpdatedAt: u.UpdatedAt,
	}
}

func (s *Server) handleReferrals(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		s.handleCreateReferral(w, r)
	case http.MethodGet:
		s.handleListReferrals(w, r)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleReferralDetail(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/referrals/")
	if path == "" {
		http.NotFound(w, r)
		return
	}
	parts := strings.Split(path, "/")
	if len(parts) < 2 || parts[0] == "" {
		http.NotFound(w, r)
		return
	}

	requestID := parts[0]
	switch parts[1] {
	case "matches":
		if len(parts) == 2 {
			switch r.Method {
			case http.MethodGet:
				s.handleListMatches(w, r, requestID)
			case http.MethodPost:
				s.handleCreateMatch(w, r, requestID)
			default:
				http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			}
			return
		}
		if len(parts) == 3 {
			switch r.Method {
			case http.MethodPatch:
				s.handleUpdateMatch(w, r, requestID, parts[2])
			default:
				http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			}
			return
		}
	case "cancel":
		s.handleCancelReferral(w, r, requestID)
		return
	}

	http.NotFound(w, r)
}

func (s *Server) handleCandidateMatches(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	userID, ok := r.Context().Value(ctxKeyUserID).(string)
	if !ok || userID == "" {
		respondError(w, http.StatusUnauthorized, "Invalid authentication context")
		return
	}
	role, _ := r.Context().Value(ctxKeyRole).(auth.Role)
	if role != auth.RoleAgent && role != auth.RoleBrokerAdmin {
		respondError(w, http.StatusForbidden, "Insufficient permissions")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), requestTimeout)
	defer cancel()

	matches, err := s.matchService.ListForCandidate(ctx, userID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "Failed to load matches")
		return
	}

	resp := make([]matchResponse, 0, len(matches))
	for _, m := range matches {
		resp = append(resp, newMatchResponse(m))
	}

	respondJSON(w, http.StatusOK, map[string]any{"items": resp})
}

func (s *Server) handleDisputes(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.handleListDisputes(w, r)
	case http.MethodPost:
		s.handleCreateDispute(w, r)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleDisputeDetail(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/api/disputes/")
	if id == "" || strings.Contains(id, "/") {
		http.NotFound(w, r)
		return
	}

	switch r.Method {
	case http.MethodPatch:
		s.handleResolveDispute(w, r, id)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

type createReferralRequest struct {
	Region       []string `json:"region"`
	PriceMin     int64    `json:"priceMin"`
	PriceMax     int64    `json:"priceMax"`
	PropertyType string   `json:"propertyType"`
	DealType     string   `json:"dealType"`
	Languages    []string `json:"languages"`
	SLAHours     int      `json:"slaHours"`
}

func (s *Server) handleCreateReferral(w http.ResponseWriter, r *http.Request) {
	var req createReferralRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	userID, ok := r.Context().Value(ctxKeyUserID).(string)
	if !ok || userID == "" {
		respondError(w, http.StatusUnauthorized, "Invalid authentication context")
		return
	}
	role, _ := r.Context().Value(ctxKeyRole).(auth.Role)
	if role != auth.RoleAgent && role != auth.RoleBrokerAdmin {
		respondError(w, http.StatusForbidden, "Insufficient permissions to create referral")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), requestTimeout)
	defer cancel()

	created, err := s.referralService.Create(ctx, referral.CreateParams{
		CreatorUserID: userID,
		Region:        req.Region,
		PriceMin:      req.PriceMin,
		PriceMax:      req.PriceMax,
		PropertyType:  req.PropertyType,
		DealType:      req.DealType,
		Languages:     req.Languages,
		SLAHours:      req.SLAHours,
	})
	if err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}

	respondJSON(w, http.StatusCreated, newReferralResponse(created))
}

func (s *Server) handleListReferrals(w http.ResponseWriter, r *http.Request) {
	userID, ok := r.Context().Value(ctxKeyUserID).(string)
	if !ok || userID == "" {
		respondError(w, http.StatusUnauthorized, "Invalid authentication context")
		return
	}

	query := r.URL.Query()
	page, _ := strconv.Atoi(query.Get("page"))
	pageSize, _ := strconv.Atoi(query.Get("pageSize"))

	filters := referral.Filters{
		CreatorUserID: userID,
		Status:        referral.Status(query.Get("status")),
		Region:        query.Get("region"),
		DealType:      query.Get("dealType"),
		Page:          page,
		PageSize:      pageSize,
		SortKey:       query.Get("sortKey"),
		SortOrder:     query.Get("sortOrder"),
	}

	if filters.Page <= 0 {
		filters.Page = 1
	}
	if filters.PageSize <= 0 || filters.PageSize > 100 {
		filters.PageSize = 20
	}

	ctx, cancel := context.WithTimeout(r.Context(), requestTimeout)
	defer cancel()

	result, err := s.referralService.List(ctx, filters)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}

	items := make([]referralResponse, 0, len(result.Items))
	for _, item := range result.Items {
		items = append(items, newReferralResponse(item))
	}

	respondJSON(w, http.StatusOK, paginatedReferrals{
		Items:    items,
		Total:    result.Total,
		Page:     filters.Page,
		PageSize: filters.PageSize,
	})
}

type referralResponse struct {
	ID             string   `json:"id"`
	CreatorAgentID string   `json:"creatorAgentId"`
	Region         []string `json:"region"`
	PriceMin       int64    `json:"priceMin"`
	PriceMax       int64    `json:"priceMax"`
	PropertyType   string   `json:"propertyType"`
	DealType       string   `json:"dealType"`
	Languages      []string `json:"languages"`
	SLAHours       int      `json:"slaHours"`
	Status         string   `json:"status"`
	CancelReason   *string  `json:"cancelReason,omitempty"`
	CreatedAt      string   `json:"createdAt"`
	UpdatedAt      string   `json:"updatedAt"`
}

type paginatedReferrals struct {
	Items    []referralResponse `json:"items"`
	Total    int                `json:"total"`
	Page     int                `json:"page"`
	PageSize int                `json:"pageSize"`
}

type matchResponse struct {
	ID               string             `json:"id"`
	CandidateAgentID string             `json:"candidateAgentId"`
	State            string             `json:"state"`
	Score            float64            `json:"score"`
	CreatedAt        string             `json:"createdAt"`
	Agreement        *agreementResponse `json:"agreement,omitempty"`
}

type disputeResponse struct {
	ID          string  `json:"id"`
	AgreementID string  `json:"agreementId"`
	Status      string  `json:"status"`
	CreatedAt   string  `json:"createdAt"`
	UpdatedAt   string  `json:"updatedAt"`
	ResolvedAt  *string `json:"resolvedAt,omitempty"`
}

func newReferralResponse(r referral.Request) referralResponse {
	region := append([]string{}, r.Region...)
	languages := append([]string{}, r.Languages...)
	if region == nil {
		region = []string{}
	}
	if languages == nil {
		languages = []string{}
	}

	return referralResponse{
		ID:             r.ID,
		CreatorAgentID: r.CreatorUserID,
		Region:         region,
		PriceMin:       r.PriceMin,
		PriceMax:       r.PriceMax,
		PropertyType:   r.PropertyType,
		DealType:       r.DealType,
		Languages:      languages,
		SLAHours:       r.SLAHours,
		Status:         string(r.Status),
		CancelReason:   r.CancelReason,
		CreatedAt:      r.CreatedAt.UTC().Format(time.RFC3339),
		UpdatedAt:      r.UpdatedAt.UTC().Format(time.RFC3339),
	}
}

func newMatchResponse(m referral.Match) matchResponse {
	return matchResponse{
		ID:               m.ID,
		CandidateAgentID: m.CandidateAgentID,
		State:            string(m.State),
		Score:            m.Score,
		CreatedAt:        m.CreatedAt.UTC().Format(time.RFC3339),
		Agreement:        nil,
	}
}

func newDisputeResponse(d dispute.Record) disputeResponse {
	resp := disputeResponse{
		ID:          d.ID,
		AgreementID: d.AgreementID,
		Status:      string(d.Status),
		CreatedAt:   d.CreatedAt.UTC().Format(time.RFC3339),
		UpdatedAt:   d.UpdatedAt.UTC().Format(time.RFC3339),
	}
	if d.ResolvedAt != nil {
		val := d.ResolvedAt.UTC().Format(time.RFC3339)
		resp.ResolvedAt = &val
	}
	return resp
}

func (s *Server) handleListMatches(w http.ResponseWriter, r *http.Request, requestID string) {
	userID, ok := r.Context().Value(ctxKeyUserID).(string)
	if !ok || userID == "" {
		respondError(w, http.StatusUnauthorized, "Invalid authentication context")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), requestTimeout)
	defer cancel()

	matches, err := s.matchService.List(ctx, requestID, userID)
	if err != nil {
		if errors.Is(err, referral.ErrReferralNotOwned) {
			respondError(w, http.StatusNotFound, "Referral not found")
			return
		}
		respondError(w, http.StatusInternalServerError, "Failed to load matches")
		return
	}

	resp := make([]matchResponse, 0, len(matches))
	for _, m := range matches {
		resp = append(resp, newMatchResponse(m))
	}

	respondJSON(w, http.StatusOK, map[string]any{
		"items": resp,
	})
}

func (s *Server) handleBrokers(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	limit := 50
	if raw := r.URL.Query().Get("limit"); raw != "" {
		if val, err := strconv.Atoi(raw); err == nil && val > 0 {
			limit = val
		}
	}

	ctx, cancel := context.WithTimeout(r.Context(), requestTimeout)
	defer cancel()

	profiles, err := s.brokerService.List(ctx, limit)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "Failed to load brokers")
		return
	}

	items := make([]brokerResponse, 0, len(profiles))
	for _, profile := range profiles {
		items = append(items, newBrokerResponse(profile))
	}

	respondJSON(w, http.StatusOK, map[string]any{
		"items": items,
		"total": len(items),
	})
}

func (s *Server) handleCreateMatch(w http.ResponseWriter, r *http.Request, requestID string) {
	userID, ok := r.Context().Value(ctxKeyUserID).(string)
	if !ok || userID == "" {
		respondError(w, http.StatusUnauthorized, "Invalid authentication context")
		return
	}

	var req struct {
		CandidateAgentID string  `json:"candidateAgentId"`
		Score            float64 `json:"score,omitempty"`
		State            string  `json:"state,omitempty"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), requestTimeout)
	defer cancel()

	match, err := s.matchService.Create(ctx, referral.CreateMatchParams{
		RequestID:        requestID,
		OwnerUserID:      userID,
		CandidateAgentID: req.CandidateAgentID,
		Score:            req.Score,
		State:            referral.MatchState(req.State),
	})
	if err != nil {
		switch {
		case errors.Is(err, referral.ErrCandidateMandatory), errors.Is(err, referral.ErrMatchInvalidScore), errors.Is(err, referral.ErrMatchInvalidState):
			respondError(w, http.StatusBadRequest, err.Error())
		case errors.Is(err, referral.ErrReferralNotOwned):
			respondError(w, http.StatusNotFound, "Referral not found")
		case errors.Is(err, referral.ErrMatchDuplicate):
			respondError(w, http.StatusConflict, err.Error())
		default:
			respondError(w, http.StatusInternalServerError, "Failed to create match")
		}
		return
	}

	respondJSON(w, http.StatusCreated, newMatchResponse(match))
}

func (s *Server) handleUpdateMatch(w http.ResponseWriter, r *http.Request, requestID, matchID string) {
	userID, ok := r.Context().Value(ctxKeyUserID).(string)
	if !ok || userID == "" {
		respondError(w, http.StatusUnauthorized, "Invalid authentication context")
		return
	}
	role, _ := r.Context().Value(ctxKeyRole).(auth.Role)
	if role != auth.RoleAgent && role != auth.RoleBrokerAdmin {
		respondError(w, http.StatusForbidden, "Insufficient permissions")
		return
	}

	var req struct {
		State string `json:"state"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "Invalid request body")
		return
	}
	state := referral.MatchState(strings.ToLower(strings.TrimSpace(req.State)))
	if state != referral.MatchStateAccepted && state != referral.MatchStateDeclined {
		respondError(w, http.StatusBadRequest, "state must be 'accepted' or 'declined'")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), requestTimeout)
	defer cancel()

	result, err := s.matchService.UpdateState(ctx, referral.UpdateMatchParams{
		MatchID:     matchID,
		CandidateID: userID,
		NewState:    state,
		Pool:        s.pool,
	})
	if err != nil {
		switch {
		case errors.Is(err, referral.ErrMatchNotFound):
			respondError(w, http.StatusNotFound, "Match not found")
		case errors.Is(err, referral.ErrMatchForbidden):
			respondError(w, http.StatusForbidden, "Insufficient permissions")
		case errors.Is(err, referral.ErrMatchInvalidTransition):
			respondError(w, http.StatusBadRequest, err.Error())
		default:
			respondError(w, http.StatusInternalServerError, "Failed to update match")
		}
		return
	}
	if result.Match.RequestID != requestID {
		respondError(w, http.StatusNotFound, "Match not found")
		return
	}

	resp := newMatchResponse(result.Match)
	if result.Agreement != nil {
		ar := newAgreementResponse(*result.Agreement)
		resp.Agreement = &ar
	}

	respondJSON(w, http.StatusOK, resp)
}

func (s *Server) handleCancelReferral(w http.ResponseWriter, r *http.Request, requestID string) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	userID, ok := r.Context().Value(ctxKeyUserID).(string)
	if !ok || userID == "" {
		respondError(w, http.StatusUnauthorized, "Invalid authentication context")
		return
	}
	role, _ := r.Context().Value(ctxKeyRole).(auth.Role)
	var payload struct {
		Reason *string `json:"reason"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil && err != io.EOF {
		respondError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), requestTimeout)
	defer cancel()

	updated, err := s.referralService.Cancel(ctx, referral.CancelParams{
		RequestID: requestID,
		ActorID:   userID,
		ActorRole: string(role),
		Reason:    payload.Reason,
	})
	if err != nil {
		switch {
		case errors.Is(err, referral.ErrNotFound):
			respondError(w, http.StatusNotFound, "Referral not found")
		case errors.Is(err, referral.ErrCancelForbidden):
			respondError(w, http.StatusForbidden, err.Error())
		case errors.Is(err, referral.ErrCancelInvalidState):
			respondError(w, http.StatusBadRequest, err.Error())
		default:
			respondError(w, http.StatusInternalServerError, "Failed to cancel referral")
		}
		return
	}

	respondJSON(w, http.StatusOK, newReferralResponse(updated))
}

func (s *Server) handleListDisputes(w http.ResponseWriter, r *http.Request) {
	userID, ok := r.Context().Value(ctxKeyUserID).(string)
	if !ok || userID == "" {
		respondError(w, http.StatusUnauthorized, "Invalid authentication context")
		return
	}

	agreementID := r.URL.Query().Get("agreementId")

	ctx, cancel := context.WithTimeout(r.Context(), requestTimeout)
	defer cancel()

	records, err := s.disputeService.List(ctx, userID, agreementID)
	if err != nil {
		if errors.Is(err, dispute.ErrForbidden) {
			respondError(w, http.StatusNotFound, "Disputes not found")
			return
		}
		respondError(w, http.StatusInternalServerError, "Failed to load disputes")
		return
	}

	resp := make([]disputeResponse, 0, len(records))
	for _, rec := range records {
		resp = append(resp, newDisputeResponse(rec))
	}

	respondJSON(w, http.StatusOK, map[string]any{"items": resp})
}

func (s *Server) handleCreateDispute(w http.ResponseWriter, r *http.Request) {
	userID, ok := r.Context().Value(ctxKeyUserID).(string)
	if !ok || userID == "" {
		respondError(w, http.StatusUnauthorized, "Invalid authentication context")
		return
	}

	var req struct {
		AgreementID string `json:"agreementId"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	if strings.TrimSpace(req.AgreementID) == "" {
		respondError(w, http.StatusBadRequest, "agreementId is required")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), requestTimeout)
	defer cancel()

	record, err := s.disputeService.Create(ctx, userID, req.AgreementID)
	if err != nil {
		if errors.Is(err, dispute.ErrForbidden) {
			respondError(w, http.StatusNotFound, "Agreement not found")
			return
		}
		respondError(w, http.StatusInternalServerError, "Failed to create dispute")
		return
	}

	respondJSON(w, http.StatusCreated, newDisputeResponse(record))
}

func (s *Server) handleResolveDispute(w http.ResponseWriter, r *http.Request, disputeID string) {
	userID, ok := r.Context().Value(ctxKeyUserID).(string)
	if !ok || userID == "" {
		respondError(w, http.StatusUnauthorized, "Invalid authentication context")
		return
	}

	var req struct {
		Status string `json:"status"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	if req.Status != "resolved" {
		respondError(w, http.StatusBadRequest, "status must be 'resolved'")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), requestTimeout)
	defer cancel()

	record, err := s.disputeService.Resolve(ctx, userID, disputeID)
	if err != nil {
		switch {
		case errors.Is(err, dispute.ErrForbidden):
			respondError(w, http.StatusNotFound, "Dispute not found")
		case errors.Is(err, dispute.ErrBadStatus):
			respondError(w, http.StatusBadRequest, err.Error())
		default:
			respondError(w, http.StatusInternalServerError, "Failed to resolve dispute")
		}
		return
	}

	respondJSON(w, http.StatusOK, newDisputeResponse(record))
}

func ensureSchema(ctx context.Context, pool *pgxpool.Pool, dir string) error {
	const checkSQL = `
		SELECT EXISTS (
			SELECT 1 FROM information_schema.tables
			WHERE table_schema = current_schema()
			  AND table_name = 'referral_requests'
		)
	`
	var exists bool
	if err := pool.QueryRow(ctx, checkSQL).Scan(&exists); err != nil {
		return fmt.Errorf("check schema: %w", err)
	}
	if exists {
		if err := ensureColumn(ctx, pool, "users", "role",
			`ALTER TABLE users ADD COLUMN IF NOT EXISTS role TEXT NOT NULL DEFAULT 'agent'`); err != nil {
			return err
		}
		if err := ensureColumn(ctx, pool, "referral_requests", "cancel_reason",
			`ALTER TABLE referral_requests ADD COLUMN IF NOT EXISTS cancel_reason TEXT`); err != nil {
			return err
		}
		if err := ensureColumn(ctx, pool, "agreements", "created_at",
			`ALTER TABLE agreements ADD COLUMN IF NOT EXISTS created_at TIMESTAMPTZ NOT NULL DEFAULT get_tx_timestamp()`); err != nil {
			return err
		}
		if err := ensureColumn(ctx, pool, "agreements", "updated_at",
			`ALTER TABLE agreements ADD COLUMN IF NOT EXISTS updated_at TIMESTAMPTZ NOT NULL DEFAULT get_tx_timestamp()`); err != nil {
			return err
		}
		if err := ensureColumn(ctx, pool, "agreements", "fee_rate",
			`ALTER TABLE agreements ADD COLUMN IF NOT EXISTS fee_rate NUMERIC(5,2) NOT NULL DEFAULT 0`); err != nil {
			return err
		}
		if err := ensureColumn(ctx, pool, "agreements", "protect_days",
			`ALTER TABLE agreements ADD COLUMN IF NOT EXISTS protect_days INTEGER NOT NULL DEFAULT 0`); err != nil {
			return err
		}
		if err := ensureColumn(ctx, pool, "agreements", "status_updated_at",
			`ALTER TABLE agreements ADD COLUMN IF NOT EXISTS status_updated_at TIMESTAMPTZ NOT NULL DEFAULT get_tx_timestamp()`); err != nil {
			return err
		}
		if err := ensureColumn(ctx, pool, "agreements", "status_updated_by",
			`ALTER TABLE agreements ADD COLUMN IF NOT EXISTS status_updated_by UUID`); err != nil {
			return err
		}
		if err := ensureColumn(ctx, pool, "agreements", "effective_at",
			`ALTER TABLE agreements ADD COLUMN IF NOT EXISTS effective_at TIMESTAMPTZ`); err != nil {
			return err
		}
		if err := ensureColumn(ctx, pool, "agreements", "pii_first_access_time",
			`ALTER TABLE agreements ADD COLUMN IF NOT EXISTS pii_first_access_time TIMESTAMPTZ`); err != nil {
			return err
		}
		if err := ensureColumn(ctx, pool, "agreements", "event_seq",
			`ALTER TABLE agreements ADD COLUMN IF NOT EXISTS event_seq BIGINT NOT NULL DEFAULT 0`); err != nil {
			return err
		}
		if err := ensureColumn(ctx, pool, "timeline_events", "payload",
			`ALTER TABLE timeline_events ADD COLUMN IF NOT EXISTS payload JSONB NOT NULL DEFAULT '{}'::jsonb`); err != nil {
			return err
		}
		if err := ensureColumn(ctx, pool, "timeline_events", "payload_version",
			`ALTER TABLE timeline_events ADD COLUMN IF NOT EXISTS payload_version SMALLINT NOT NULL DEFAULT 1`); err != nil {
			return err
		}
		if err := ensureColumn(ctx, pool, "timeline_events", "actor_broker_id",
			`ALTER TABLE timeline_events ADD COLUMN IF NOT EXISTS actor_broker_id UUID`); err != nil {
			return err
		}
		return applyMigrations(ctx, pool, dir)
	}

	if err := ensurePgcrypto(ctx, pool); err != nil {
		return err
	}

	return applyMigrations(ctx, pool, dir)
}

func ensurePgcrypto(ctx context.Context, pool *pgxpool.Pool) error {
	const q = `SELECT EXISTS (SELECT 1 FROM pg_proc WHERE proname = 'gen_random_uuid')`
	var exists bool
	if err := pool.QueryRow(ctx, q).Scan(&exists); err != nil {
		return fmt.Errorf("check pgcrypto: %w", err)
	}
	if exists {
		return nil
	}
	if _, err := pool.Exec(ctx, "CREATE EXTENSION IF NOT EXISTS pgcrypto"); err != nil {
		return fmt.Errorf("install pgcrypto: %w", err)
	}
	return nil
}

func applyMigrations(ctx context.Context, pool *pgxpool.Pool, dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("read migrations dir: %w", err)
	}

	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Name() < entries[j].Name()
	})

	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".sql" {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("read migration %s: %w", entry.Name(), err)
		}
		if _, err := pool.Exec(ctx, string(data)); err != nil {
			return fmt.Errorf("apply migration %s: %w", entry.Name(), err)
		}
	}

	return nil
}

func ensureColumn(ctx context.Context, pool *pgxpool.Pool, table, column, ddl string) error {
	const query = `
		SELECT EXISTS (
			SELECT 1 FROM information_schema.columns
			WHERE table_schema = current_schema()
			  AND table_name = $1
			  AND column_name = $2
		)
	`
	var exists bool
	if err := pool.QueryRow(ctx, query, table, column).Scan(&exists); err != nil {
		return fmt.Errorf("check column %s.%s: %w", table, column, err)
	}
	if exists {
		return nil
	}
	if _, err := pool.Exec(ctx, ddl); err != nil {
		return fmt.Errorf("add column %s.%s: %w", table, column, err)
	}
	return nil
}

func (s *Server) handleAgreements(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		s.handleCreateAgreement(w, r)
	case http.MethodGet:
		s.handleListAgreements(w, r)
	case http.MethodPatch:
		s.handleUpdateAgreementStatus(w, r)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

type timelineEvent struct {
	ID          string         `json:"id"`
	AgreementID string         `json:"agreementId"`
	Type        string         `json:"type"`
	At          time.Time      `json:"at"`
	Payload     map[string]any `json:"payload,omitempty"`
	ActorBroker *string        `json:"actorBrokerId,omitempty"`
}

type brokerResponse struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Fein      string `json:"fein"`
	Verified  bool   `json:"verified"`
	CreatedAt string `json:"createdAt"`
}

func newBrokerResponse(profile broker.Profile) brokerResponse {
	return brokerResponse{
		ID:        profile.ID,
		Name:      profile.Name,
		Fein:      profile.Fein,
		Verified:  profile.Verified,
		CreatedAt: profile.CreatedAt.UTC().Format(time.RFC3339),
	}
}

func (s *Server) handleTimelineEvents(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), requestTimeout)
	defer cancel()

	query := r.URL.Query()
	page, _ := strconv.Atoi(query.Get("page"))
	pageSize, _ := strconv.Atoi(query.Get("pageSize"))

	if page <= 0 {
		page = 1
	}
	if pageSize <= 0 || pageSize > 100 {
		pageSize = 20
	}

	rows, err := s.pool.Query(ctx, `
		SELECT id, agreement_id, type, ts, payload, actor_broker_id
		FROM timeline_events
		ORDER BY ts DESC
		LIMIT $1 OFFSET $2
	`, pageSize, (page-1)*pageSize)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "Failed to load timeline events")
		return
	}
	defer rows.Close()

	events := make([]timelineEvent, 0, pageSize)
	for rows.Next() {
		var (
			id           int64
			agID         string
			typeStr      string
			ts           time.Time
			payloadBytes []byte
			actorBroker  sql.NullString
		)

		if err := rows.Scan(&id, &agID, &typeStr, &ts, &payloadBytes, &actorBroker); err != nil {
			respondError(w, http.StatusInternalServerError, "Failed to parse timeline event")
			return
		}

		var payload map[string]any
		if len(payloadBytes) > 0 && string(payloadBytes) != "null" {
			if err := json.Unmarshal(payloadBytes, &payload); err != nil {
				payload = map[string]any{"raw": string(payloadBytes)}
			}
		}

		var actorPtr *string
		if actorBroker.Valid {
			val := actorBroker.String
			actorPtr = &val
		}

		events = append(events, timelineEvent{
			ID:          strconv.FormatInt(id, 10),
			AgreementID: agID,
			Type:        typeStr,
			At:          ts.UTC(),
			Payload:     payload,
			ActorBroker: actorPtr,
		})
	}

	if err := rows.Err(); err != nil {
		respondError(w, http.StatusInternalServerError, "Failed to iterate timeline events")
		return
	}

	var total int
	if err := s.pool.QueryRow(ctx, `SELECT COUNT(*) FROM timeline_events`).Scan(&total); err != nil {
		respondError(w, http.StatusInternalServerError, "Failed to count timeline events")
		return
	}

	respondJSON(w, http.StatusOK, map[string]any{
		"items":    events,
		"total":    total,
		"page":     page,
		"pageSize": pageSize,
	})
}

func (s *Server) handleBroker(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	id := strings.TrimPrefix(r.URL.Path, "/api/brokers/")
	if id == "" {
		respondError(w, http.StatusBadRequest, "Missing broker id")
		return
	}
	if slash := strings.IndexRune(id, '/'); slash >= 0 {
		respondError(w, http.StatusBadRequest, "Missing broker id")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), requestTimeout)
	defer cancel()

	profile, err := s.brokerService.GetByID(ctx, id)
	if err != nil {
		if errors.Is(err, broker.ErrNotFound) {
			respondError(w, http.StatusNotFound, "Broker not found")
			return
		}
		respondError(w, http.StatusInternalServerError, "Failed to load broker")
		return
	}

	respondJSON(w, http.StatusOK, newBrokerResponse(profile))
}

type createAgreementRequest struct {
	RequestID        string  `json:"requestId"`
	ReferrerBrokerID string  `json:"referrerBrokerId"`
	RefereeBrokerID  string  `json:"refereeBrokerId"`
	FeeRate          float64 `json:"feeRate"`
	ProtectDays      int     `json:"protectDays"`
}

func (s *Server) handleCreateAgreement(w http.ResponseWriter, r *http.Request) {
	var req createAgreementRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	userID, ok := r.Context().Value(ctxKeyUserID).(string)
	if !ok || userID == "" {
		respondError(w, http.StatusUnauthorized, "Invalid authentication context")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), requestTimeout)
	defer cancel()

	record, err := s.agreementCRUD.Create(ctx, userID, agreement.CreateParams{
		RequestID:        req.RequestID,
		ReferrerBrokerID: req.ReferrerBrokerID,
		RefereeBrokerID:  req.RefereeBrokerID,
		FeeRate:          req.FeeRate,
		ProtectDays:      req.ProtectDays,
	})
	if err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}

	respondJSON(w, http.StatusCreated, newAgreementResponse(record))
}

func (s *Server) handleListAgreements(w http.ResponseWriter, r *http.Request) {
	userID, ok := r.Context().Value(ctxKeyUserID).(string)
	if !ok || userID == "" {
		respondError(w, http.StatusUnauthorized, "Invalid authentication context")
		return
	}

	query := r.URL.Query()
	page, _ := strconv.Atoi(query.Get("page"))
	pageSize, _ := strconv.Atoi(query.Get("pageSize"))

	filters := agreement.ListFilters{
		CreatorUserID: userID,
		Page:          page,
		PageSize:      pageSize,
	}

	ctx, cancel := context.WithTimeout(r.Context(), requestTimeout)
	defer cancel()

	items, total, err := s.agreementCRUD.List(ctx, filters)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}

	responses := make([]agreementResponse, 0, len(items))
	for _, item := range items {
		responses = append(responses, newAgreementResponse(item))
	}

	if filters.Page <= 0 {
		filters.Page = 1
	}
	if filters.PageSize <= 0 || filters.PageSize > 100 {
		filters.PageSize = 20
	}

	respondJSON(w, http.StatusOK, paginatedAgreements{
		Items:    responses,
		Total:    total,
		Page:     filters.Page,
		PageSize: filters.PageSize,
	})
}

type agreementResponse struct {
	ID               string  `json:"id"`
	RequestID        string  `json:"requestId"`
	ReferrerBrokerID string  `json:"referrerBrokerId"`
	RefereeBrokerID  string  `json:"refereeBrokerId"`
	FeeRate          float64 `json:"feeRate"`
	ProtectDays      int     `json:"protectDays"`
	EffectiveAt      string  `json:"effectiveAt,omitempty"`
	CreatedAt        string  `json:"createdAt"`
	UpdatedAt        string  `json:"updatedAt"`
}

type paginatedAgreements struct {
	Items    []agreementResponse `json:"items"`
	Total    int                 `json:"total"`
	Page     int                 `json:"page"`
	PageSize int                 `json:"pageSize"`
}

func newAgreementResponse(rec agreement.Record) agreementResponse {
	var effective string
	if rec.EffectiveAt != nil {
		effective = rec.EffectiveAt.UTC().Format(time.RFC3339)
	}

	return agreementResponse{
		ID:               rec.ID,
		RequestID:        rec.RequestID,
		ReferrerBrokerID: rec.ReferrerBrokerID,
		RefereeBrokerID:  rec.RefereeBrokerID,
		FeeRate:          rec.FeeRate,
		ProtectDays:      rec.ProtectDays,
		EffectiveAt:      effective,
		CreatedAt:        rec.CreatedAt.UTC().Format(time.RFC3339),
		UpdatedAt:        rec.UpdatedAt.UTC().Format(time.RFC3339),
	}
}

func (s *Server) handleUpdateAgreementStatus(w http.ResponseWriter, r *http.Request) {
	var req struct {
		AgreementID string         `json:"agreementId"`
		NextStatus  string         `json:"nextStatus"`
		Payload     map[string]any `json:"payload"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	userID, ok := r.Context().Value(ctxKeyUserID).(string)
	if !ok || userID == "" {
		respondError(w, http.StatusUnauthorized, "Invalid authentication context")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), requestTimeout)
	defer cancel()

	if err := s.agreementStatus.Transition(ctx, agreement.TransitionParams{
		AgreementID: req.AgreementID,
		ActorID:     userID,
		NextStatus:  req.NextStatus,
		Payload:     req.Payload,
	}); err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}

	respondJSON(w, http.StatusOK, map[string]any{
		"agreementId": req.AgreementID,
		"nextStatus":  req.NextStatus,
	})
}
