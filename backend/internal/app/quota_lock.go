package app

import (
	"context"
	"sort"

	"cpa-helper/backend/internal/platform/database"
)

// All balance/card mutations acquire user locks first, in ascending order.
// SQLite already serializes transactions through its single connection.
func lockQuotaUsers(ctx context.Context, tx *database.Tx, ids []int) error {
	if tx.ForUpdate() == "" {
		return nil
	}
	ordered := append([]int(nil), ids...)
	sort.Ints(ordered)
	for _, id := range ordered {
		var found int
		if err := tx.QueryRowContext(ctx, "SELECT id FROM users WHERE id = ?"+tx.ForUpdate(), id).Scan(&found); err != nil {
			return err
		}
	}
	return nil
}
