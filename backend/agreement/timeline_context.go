package agreement

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/jackc/pgx/v5"
)

func setTimelineBroker(ctx context.Context, tx pgx.Tx, fromBrokerID, toBrokerID string, actorID *string) error {
	brokerID := fromBrokerID
	if actorID != nil && *actorID != "" {
		var actorBroker sql.NullString
		if err := tx.QueryRow(ctx, `SELECT broker_id::text FROM users WHERE id = $1`, *actorID).Scan(&actorBroker); err == nil && actorBroker.Valid {
			if actorBroker.String == fromBrokerID || actorBroker.String == toBrokerID {
				brokerID = actorBroker.String
			}
		}
	}

	if brokerID == "" {
		return fmt.Errorf("agreement: timeline broker context missing")
	}

	if _, err := tx.Exec(ctx, `SET LOCAL app.broker_id = $1`, brokerID); err != nil {
		return fmt.Errorf("agreement: set timeline broker: %w", err)
	}
	return nil
}
