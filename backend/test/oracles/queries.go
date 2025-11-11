package oracles

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

type Oracle struct {
	Name string
	SQL  string
}

func All() []Oracle {
	return []Oracle{
		{
			Name: "O1_unique_active_agreement",
			SQL: `SELECT referral_id, COUNT(*) FROM agreements
                  WHERE status IN ('pending_signature','effective')
                  GROUP BY referral_id HAVING COUNT(*) > 1`,
		},
		{
			Name: "O2_temporal_order",
			SQL: `SELECT e.* FROM timeline_events e
                  JOIN agreements a ON a.id = e.agreement_id
                  WHERE e.type IN ('OFFER_MADE','ESIGN_COMPLETED','DEAL_CLOSED')
                  AND (a.status NOT IN ('effective','success','disputed') OR e.ts < a.effective_at)`,
		},
		{
			Name: "O3_worm_seq_monotonic",
			SQL: `WITH seqs AS (
                      SELECT agreement_id, seq,
                             LAG(seq) OVER (PARTITION BY agreement_id ORDER BY seq) AS prev
                      FROM timeline_events)
                  SELECT * FROM seqs WHERE prev IS NOT NULL AND seq <= prev`,
		},
		{
			Name: "O4_pii_gate_bypass",
			SQL: `SELECT * FROM audit_logs
                  WHERE action = 'PII_READ'
                    AND ts <= (SELECT effective_at FROM agreements WHERE id = audit_logs.agreement_id)`,
		},
		{
			Name: "O5_outbox_edge_idem",
			SQL: `WITH stale AS (
                      SELECT id::text AS any FROM outbox
                      WHERE status NOT IN ('processed','dead')
                        AND now()-created_at > interval '5 minutes'
                  ),
                  dup_edge AS (
                      SELECT key AS any FROM edge_invocations WHERE status='completed'
                      GROUP BY key HAVING COUNT(*) > 1)
                  SELECT * FROM stale
                  UNION ALL
                  SELECT * FROM dup_edge`,
		},
		{
			Name: "O6_dispute_linkage",
			SQL: `SELECT i.* FROM invoices i
                  JOIN disputes d ON d.agreement_id = i.agreement_id
                  WHERE d.status='resolved' AND i.is_invalidated=false AND i.status NOT IN ('closed','written_off')`,
		},
		{
			Name: "O7_region_immutable",
			SQL:  `SELECT * FROM agreements_region_audit`,
		},
		{
			Name: "O8_timeline_actor_broker",
			SQL:  `SELECT id FROM timeline_events WHERE actor_broker_id IS NULL`,
		},
		{
			Name: "O9_agreement_delete_guard",
			SQL: `SELECT 'missing_no_delete_trigger' AS detail
                  WHERE NOT EXISTS (SELECT 1 FROM pg_trigger WHERE tgname='no_delete_agreements')`,
		},
	}
}

// Run executes all oracles and returns the first failure (name and sample row text) or empty name if all pass.
func Run(ctx context.Context, pool *pgxpool.Pool) (string, string, error) {
	for _, o := range All() {
		rows, err := pool.Query(ctx, o.SQL)
		if err != nil {
			return o.Name, "", fmt.Errorf("oracle %s: %w", o.Name, err)
		}
		has := rows.Next()
		if has {
			vals, err := rows.Values()
			rows.Close()
			if err != nil {
				return o.Name, "", err
			}
			return o.Name, fmt.Sprintf("%v", vals), nil
		}
		rows.Close()
	}
	return "", "", nil
}
