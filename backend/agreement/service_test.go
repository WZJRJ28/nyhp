package agreement

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

func TestHandleEsignCompletionWebhook_Idempotent(t *testing.T) {
	pool := &fakePool{}
	repo := &fakeRepo{insertErr: ErrDuplicateIdempotencyKey}
	svc := NewService(pool, repo)

	req := EsignCompletionRequest{
		AgreementID:    "agreement-123",
		IdempotencyKey: "event-abc",
	}

	if err := svc.HandleEsignCompletionWebhook(context.Background(), req); err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}

	if pool.tx == nil {
		t.Fatalf("expected Begin to provide transaction")
	}

	if !pool.tx.rolled {
		t.Errorf("expected rollback to be called")
	}

	if pool.tx.committed {
		t.Errorf("expected commit to be skipped on idempotent replay")
	}

	if repo.executed {
		t.Errorf("expected execution logic to be skipped when key duplicates")
	}
}

func TestHandleEsignCompletionWebhook_Success(t *testing.T) {
	pool := &fakePool{}
	repo := &fakeRepo{}
	svc := NewService(pool, repo)

	req := EsignCompletionRequest{
		AgreementID:    "agreement-xyz",
		IdempotencyKey: "event-123",
	}

	if err := svc.HandleEsignCompletionWebhook(context.Background(), req); err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}

	if pool.tx == nil {
		t.Fatalf("expected transaction to be created")
	}

	if !pool.tx.committed {
		t.Errorf("expected commit to be called")
	}

	if !repo.executed {
		t.Errorf("expected repository execution to run")
	}
}

type fakeRepo struct {
	insertErr error
	execErr   error
	executed  bool
}

func (f *fakeRepo) InsertIdempotencyKey(ctx context.Context, tx pgx.Tx, key string) error {
	return f.insertErr
}

func (f *fakeRepo) ExecuteEsignCompletionTx(ctx context.Context, tx pgx.Tx, params ExecuteEsignCompletionParams) error {
	f.executed = true
	return f.execErr
}

type fakePool struct {
	tx *fakeTx
}

func (f *fakePool) Begin(ctx context.Context) (pgx.Tx, error) {
	f.tx = &fakeTx{}
	return f.tx, nil
}

type fakeTx struct {
	rolled    bool
	committed bool
}

func (f *fakeTx) Begin(context.Context) (pgx.Tx, error) {
	return nil, errors.New("fakeTx does not support nested transactions")
}

func (f *fakeTx) Commit(context.Context) error {
	f.committed = true
	return nil
}

func (f *fakeTx) Rollback(context.Context) error {
	f.rolled = true
	return nil
}

func (f *fakeTx) CopyFrom(context.Context, pgx.Identifier, []string, pgx.CopyFromSource) (int64, error) {
	panic("not implemented")
}

func (f *fakeTx) SendBatch(context.Context, *pgx.Batch) pgx.BatchResults {
	panic("not implemented")
}

func (f *fakeTx) LargeObjects() pgx.LargeObjects {
	panic("not implemented")
}

func (f *fakeTx) Prepare(context.Context, string, string) (*pgconn.StatementDescription, error) {
	panic("not implemented")
}

func (f *fakeTx) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	panic("not implemented")
}

func (f *fakeTx) Query(context.Context, string, ...any) (pgx.Rows, error) {
	panic("not implemented")
}

func (f *fakeTx) QueryRow(context.Context, string, ...any) pgx.Row {
	panic("not implemented")
}

func (f *fakeTx) Conn() *pgx.Conn {
	return nil
}
