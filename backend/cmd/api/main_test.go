package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"brokerflow/auth"
	"brokerflow/broker"
	"brokerflow/dispute"
	"brokerflow/referral"
)

type stubBrokerRepo struct {
	profile  broker.Profile
	profiles []broker.Profile
	err      error
}

func (s *stubBrokerRepo) GetByID(_ context.Context, _ string) (broker.Profile, error) {
	return s.profile, s.err
}

func (s *stubBrokerRepo) List(_ context.Context, limit int) ([]broker.Profile, error) {
	if s.err != nil {
		return nil, s.err
	}
	if limit <= 0 || limit > len(s.profiles) {
		limit = len(s.profiles)
	}
	out := make([]broker.Profile, limit)
	copy(out, s.profiles[:limit])
	return out, nil
}

type stubMatchService struct {
	listMatches      []referral.Match
	listErr          error
	createMatch      referral.Match
	createErr        error
	candidateMatches []referral.Match
	candidateErr     error
	updateResult     referral.MatchUpdateResult
	updateErr        error
}

func (s *stubMatchService) List(_ context.Context, _ string, _ string) ([]referral.Match, error) {
	return s.listMatches, s.listErr
}

type stubDisputeService struct {
	listRecords   []dispute.Record
	listErr       error
	createRecord  dispute.Record
	createErr     error
	resolveRecord dispute.Record
	resolveErr    error
}

func (s *stubDisputeService) List(_ context.Context, _ string, _ string) ([]dispute.Record, error) {
	return s.listRecords, s.listErr
}

func (s *stubDisputeService) Create(_ context.Context, _ string, _ string) (dispute.Record, error) {
	return s.createRecord, s.createErr
}

func (s *stubDisputeService) Resolve(_ context.Context, _ string, _ string) (dispute.Record, error) {
	return s.resolveRecord, s.resolveErr
}

func (s *stubMatchService) Create(_ context.Context, _ referral.CreateMatchParams) (referral.Match, error) {
	return s.createMatch, s.createErr
}

func (s *stubMatchService) ListForCandidate(_ context.Context, _ string) ([]referral.Match, error) {
	return s.candidateMatches, s.candidateErr
}

func (s *stubMatchService) UpdateState(_ context.Context, _ referral.UpdateMatchParams) (referral.MatchUpdateResult, error) {
	return s.updateResult, s.updateErr
}

func TestHandleBroker_Success(t *testing.T) {
	now := time.Date(2024, 10, 31, 15, 4, 5, 0, time.UTC)
	server := &Server{
		brokerService: broker.NewService(&stubBrokerRepo{
			profile: broker.Profile{
				ID:        "b1",
				Name:      "Metro Realty",
				Fein:      "12-3456789",
				Verified:  true,
				CreatedAt: now,
			},
		}),
	}

	req := httptest.NewRequest(http.MethodGet, "/api/brokers/b1", nil)
	rec := httptest.NewRecorder()

	server.handleBroker(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rec.Code)
	}

	var resp brokerResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if resp.ID != "b1" || resp.Name != "Metro Realty" || !resp.Verified {
		t.Fatalf("unexpected response payload: %+v", resp)
	}
	if resp.CreatedAt != now.Format(time.RFC3339) {
		t.Fatalf("expected createdAt %s, got %s", now.Format(time.RFC3339), resp.CreatedAt)
	}
}

func TestHandleBroker_NotFound(t *testing.T) {
	server := &Server{
		brokerService: broker.NewService(&stubBrokerRepo{
			err: broker.ErrNotFound,
		}),
	}

	req := httptest.NewRequest(http.MethodGet, "/api/brokers/missing", nil)
	rec := httptest.NewRecorder()

	server.handleBroker(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}

func TestHandleBroker_InvalidPath(t *testing.T) {
	server := &Server{
		brokerService: broker.NewService(&stubBrokerRepo{}),
	}

	req := httptest.NewRequest(http.MethodGet, "/api/brokers/", nil)
	rec := httptest.NewRecorder()

	server.handleBroker(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestHandleBroker_WrongMethod(t *testing.T) {
	server := &Server{
		brokerService: broker.NewService(&stubBrokerRepo{}),
	}

	req := httptest.NewRequest(http.MethodPost, "/api/brokers/b1", nil)
	rec := httptest.NewRecorder()

	server.handleBroker(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", rec.Code)
	}
}

func TestHandleBroker_UnexpectedError(t *testing.T) {
	server := &Server{
		brokerService: broker.NewService(&stubBrokerRepo{
			err: errors.New("boom"),
		}),
	}

	req := httptest.NewRequest(http.MethodGet, "/api/brokers/b1", nil)
	rec := httptest.NewRecorder()

	server.handleBroker(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", rec.Code)
	}
}

func TestHandleBrokers_List(t *testing.T) {
	now := time.Now().UTC()
	server := &Server{
		brokerService: broker.NewService(&stubBrokerRepo{
			profiles: []broker.Profile{
				{ID: "b1", Name: "Alpha Realty", Fein: "11-1111111", Verified: true, CreatedAt: now},
				{ID: "b2", Name: "Beta Realty", Fein: "22-2222222", Verified: false, CreatedAt: now},
			},
		}),
	}

	req := httptest.NewRequest(http.MethodGet, "/api/brokers?limit=1", nil)
	rec := httptest.NewRecorder()

	server.handleBrokers(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var payload struct {
		Items []brokerResponse `json:"items"`
		Total int              `json:"total"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode list response: %v", err)
	}
	if len(payload.Items) != 1 || payload.Total != 1 || payload.Items[0].ID != "b1" {
		t.Fatalf("unexpected payload: %+v", payload)
	}
}

func TestHandleListMatches_Success(t *testing.T) {
	now := time.Now().UTC()
	server := &Server{
		matchService: &stubMatchService{
			listMatches: []referral.Match{
				{ID: "m1", CandidateAgentID: "agent-1", State: referral.MatchStateAccepted, Score: 0.9, CreatedAt: now},
			},
		},
	}

	req := httptest.NewRequest(http.MethodGet, "/api/referrals/req-1/matches", nil)
	req = req.WithContext(context.WithValue(req.Context(), ctxKeyUserID, "owner-1"))
	rec := httptest.NewRecorder()

	server.handleReferralDetail(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var payload struct {
		Items []matchResponse `json:"items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(payload.Items) != 1 || payload.Items[0].ID != "m1" {
		t.Fatalf("unexpected payload: %+v", payload)
	}
}

func TestHandleCreateMatch_ValidationError(t *testing.T) {
	server := &Server{
		matchService: &stubMatchService{
			createErr: referral.ErrCandidateMandatory,
		},
	}

	body := strings.NewReader(`{"score":0.8}`)
	req := httptest.NewRequest(http.MethodPost, "/api/referrals/req-1/matches", body)
	req = req.WithContext(context.WithValue(req.Context(), ctxKeyUserID, "owner-1"))
	rec := httptest.NewRecorder()

	server.handleReferralDetail(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestHandleCandidateMatches_Success(t *testing.T) {
	server := &Server{
		matchService: &stubMatchService{
			candidateMatches: []referral.Match{{ID: "m1", RequestID: "r1", CandidateAgentID: "agent-1", State: referral.MatchStateInvited}},
		},
	}
	req := httptest.NewRequest(http.MethodGet, "/api/matches", nil)
	req = req.WithContext(context.WithValue(req.Context(), ctxKeyUserID, "agent-1"))
	req = req.WithContext(context.WithValue(req.Context(), ctxKeyRole, auth.RoleAgent))
	rec := httptest.NewRecorder()

	server.handleCandidateMatches(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var payload struct {
		Items []matchResponse `json:"items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(payload.Items) != 1 || payload.Items[0].ID != "m1" {
		t.Fatalf("unexpected matches payload: %+v", payload)
	}
}

func TestHandleUpdateMatch_InvalidState(t *testing.T) {
	server := &Server{
		matchService: &stubMatchService{},
	}

	req := httptest.NewRequest(http.MethodPatch, "/api/referrals/r1/matches/m1", strings.NewReader(`{"state":"pending"}`))
	req = req.WithContext(context.WithValue(req.Context(), ctxKeyUserID, "agent-1"))
	req = req.WithContext(context.WithValue(req.Context(), ctxKeyRole, auth.RoleAgent))
	rec := httptest.NewRecorder()

	server.handleUpdateMatch(rec, req, "r1", "m1")

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestHandleCreateReferral_ForbidClientRole(t *testing.T) {
	server := &Server{}
	body := strings.NewReader(`{"region":["us"],"priceMin":100,"priceMax":200,"propertyType":"condo","dealType":"buy","languages":["English"],"slaHours":24}`)
	req := httptest.NewRequest(http.MethodPost, "/api/referrals", body)
	ctx := context.WithValue(req.Context(), ctxKeyUserID, "user-1")
	ctx = context.WithValue(ctx, ctxKeyRole, auth.RoleClient)
	rec := httptest.NewRecorder()

	server.handleCreateReferral(rec, req.WithContext(ctx))

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", rec.Code)
	}
}

func TestHandleListDisputes_Success(t *testing.T) {
	now := time.Now().UTC()
	server := &Server{
		disputeService: &stubDisputeService{
			listRecords: []dispute.Record{{ID: "d1", AgreementID: "ag1", Status: dispute.StatusUnderReview, CreatedAt: now, UpdatedAt: now}},
		},
	}

	req := httptest.NewRequest(http.MethodGet, "/api/disputes", nil)
	req = req.WithContext(context.WithValue(req.Context(), ctxKeyUserID, "owner-1"))
	rec := httptest.NewRecorder()

	server.handleDisputes(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var payload struct {
		Items []disputeResponse `json:"items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(payload.Items) != 1 || payload.Items[0].ID != "d1" {
		t.Fatalf("unexpected payload: %+v", payload)
	}
}

func TestHandleCreateDispute_NotFound(t *testing.T) {
	server := &Server{
		disputeService: &stubDisputeService{
			createErr: dispute.ErrForbidden,
		},
	}

	body := strings.NewReader(`{"agreementId":"ag1"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/disputes", body)
	req = req.WithContext(context.WithValue(req.Context(), ctxKeyUserID, "owner-1"))
	rec := httptest.NewRecorder()

	server.handleDisputes(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}

func TestHandleResolveDispute_BadStatus(t *testing.T) {
	server := &Server{
		disputeService: &stubDisputeService{
			resolveErr: dispute.ErrBadStatus,
		},
	}

	body := strings.NewReader(`{"status":"resolved"}`)
	req := httptest.NewRequest(http.MethodPatch, "/api/disputes/d1", body)
	req = req.WithContext(context.WithValue(req.Context(), ctxKeyUserID, "owner-1"))
	rec := httptest.NewRecorder()

	server.handleDisputeDetail(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}
