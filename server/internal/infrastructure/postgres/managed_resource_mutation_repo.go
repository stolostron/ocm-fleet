package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/stolostron/ocm-fleet/server/internal/domain"
)

// ManagedResourceMutationRepo persists immutable request receipts.
type ManagedResourceMutationRepo struct {
	DB interface {
		ExecContext(context.Context, string, ...any) (sql.Result, error)
		QueryRowContext(context.Context, string, ...any) *sql.Row
	}
}

func (r *ManagedResourceMutationRepo) Get(ctx context.Context, id string) (domain.ManagedResourceMutationReceipt, error) {
	var data string
	if err := r.DB.QueryRowContext(ctx, `SELECT receipt FROM managed_resource_mutations WHERE request_id = $1`, id).Scan(&data); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domain.ManagedResourceMutationReceipt{}, domain.ErrNotFound
		}
		return domain.ManagedResourceMutationReceipt{}, err
	}
	var receipt domain.ManagedResourceMutationReceipt
	if err := json.Unmarshal([]byte(data), &receipt); err != nil {
		return receipt, fmt.Errorf("decode mutation receipt: %w", err)
	}
	return receipt, nil
}

func (r *ManagedResourceMutationRepo) Create(ctx context.Context, receipt domain.ManagedResourceMutationReceipt) error {
	data, err := json.Marshal(receipt)
	if err != nil {
		return err
	}
	_, err = r.DB.ExecContext(ctx, `INSERT INTO managed_resource_mutations (request_id, receipt) VALUES ($1, $2)`, receipt.RequestID, string(data))
	return err
}
