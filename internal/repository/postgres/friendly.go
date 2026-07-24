package postgres

import (
	"context"
	"errors"
	"fmt"
)

var ErrFriendlyNotFound = errors.New("friendly squawk not found")

type FriendlySquawk struct {
	Code  string `json:"code"`
	Label string `json:"label"`
}

func (r *Repository) ListFriendlySquawks(ctx context.Context) ([]FriendlySquawk, error) {
	ctx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()

	rows, err := r.pool.Query(ctx, `SELECT code, label FROM friendly_squawks ORDER BY code`)
	if err != nil {
		return nil, fmt.Errorf("query friendly squawks: %w", err)
	}
	defer rows.Close()

	squawks := make([]FriendlySquawk, 0)
	for rows.Next() {
		var s FriendlySquawk
		if err := rows.Scan(&s.Code, &s.Label); err != nil {
			return nil, fmt.Errorf("scan friendly squawk: %w", err)
		}
		squawks = append(squawks, s)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate friendly squawks: %w", err)
	}
	return squawks, nil
}

func (r *Repository) ListFriendlyCodes(ctx context.Context) ([]string, error) {
	ctx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()

	rows, err := r.pool.Query(ctx, `SELECT code FROM friendly_squawks`)
	if err != nil {
		return nil, fmt.Errorf("query friendly codes: %w", err)
	}
	defer rows.Close()

	codes := make([]string, 0)
	for rows.Next() {
		var code string
		if err := rows.Scan(&code); err != nil {
			return nil, fmt.Errorf("scan friendly code: %w", err)
		}
		codes = append(codes, code)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate friendly codes: %w", err)
	}
	return codes, nil
}

func (r *Repository) AddFriendlySquawk(ctx context.Context, code, label string) error {
	ctx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()

	_, err := r.pool.Exec(ctx,
		`INSERT INTO friendly_squawks (code, label) VALUES ($1, $2)
		 ON CONFLICT (code) DO UPDATE SET label = EXCLUDED.label`,
		code, label,
	)
	if err != nil {
		return fmt.Errorf("insert friendly squawk: %w", err)
	}
	return nil
}

func (r *Repository) DeleteFriendlySquawk(ctx context.Context, code string) error {
	ctx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()

	tag, err := r.pool.Exec(ctx, `DELETE FROM friendly_squawks WHERE code = $1`, code)
	if err != nil {
		return fmt.Errorf("delete friendly squawk: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrFriendlyNotFound
	}
	return nil
}
