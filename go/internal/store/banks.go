package store

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type BankListItem struct {
	BankID      string         `json:"bank_id"`
	Name        *string        `json:"name"`
	Disposition map[string]int `json:"disposition"`
	Mission     string         `json:"mission"`
	CreatedAt   *string        `json:"created_at"`
	UpdatedAt   *string        `json:"updated_at"`
}

// BankProfile matches OpenAPI's BankProfileResponse.
type BankProfile struct {
	BankID      string         `json:"bank_id"`
	Name        string         `json:"name"`
	Disposition map[string]int `json:"disposition"`
	Mission     string         `json:"mission"`
	Background  *string        `json:"background"` // deprecated, mirrors mission
}

var defaultDisposition = map[string]int{
	"skepticism": 3,
	"literalism": 3,
	"empathy":    3,
}

var bankIndexFactTypes = map[string]string{
	"world":        "worl",
	"experience":   "expr",
	"observation":  "obsv",
}

func bankIndexName(ft, internalID string) string {
	hexOnly := strings.ReplaceAll(internalID, "-", "")
	return fmt.Sprintf("idx_mu_emb_%s_%s", bankIndexFactTypes[ft], hexOnly[:16])
}

func (s *Store) GetBank(ctx context.Context, bankID string) (*BankProfile, error) {
	query := `
		SELECT name, disposition, mission
		FROM ` + s.table("banks") + `
		WHERE bank_id = $1
	`
	var name *string
	var dispositionBytes []byte
	var mission *string
	err := s.pool.QueryRow(ctx, query, bankID).Scan(&name, &dispositionBytes, &mission)
	if err == nil {
		profile := &BankProfile{
			BankID: bankID,
		}
		if name != nil {
			profile.Name = *name
		}
		if len(dispositionBytes) > 0 {
			if err := json.Unmarshal(dispositionBytes, &profile.Disposition); err != nil {
				return nil, err
			}
		}
		if mission != nil {
			profile.Mission = *mission
		}
		bg := profile.Mission
		profile.Background = &bg
		return profile, nil
	}
	if err != pgx.ErrNoRows {
		return nil, err
	}

	// Not found — auto-create with defaults.
	internalID := uuid.New().String()
	insertQuery := `
		INSERT INTO ` + s.table("banks") + ` (bank_id, name, disposition, mission, internal_id)
		VALUES ($1, $2, $3::jsonb, $4, $5)
		ON CONFLICT (bank_id) DO NOTHING
		RETURNING bank_id
	`
	var returnedID *string
	err = s.pool.QueryRow(ctx, insertQuery, bankID, bankID, mustJSON(defaultDisposition), "", internalID).Scan(&returnedID)
	if err != nil && err != pgx.ErrNoRows {
		return nil, err
	}
	created := returnedID != nil
	if created {
		if err := s.createBankVectorIndexes(ctx, bankID, internalID); err != nil {
			return nil, err
		}
	}
	profile := &BankProfile{
		BankID:      bankID,
		Name:        bankID,
		Disposition: copyDisposition(defaultDisposition),
		Mission:     "",
	}
	bg := ""
	profile.Background = &bg
	return profile, nil
}

func (s *Store) UpdateBank(ctx context.Context, bankID string, name *string, mission *string, disposition map[string]int) (*BankProfile, error) {
	// Ensure bank exists (auto-creates if needed).
	if _, err := s.GetBank(ctx, bankID); err != nil {
		return nil, err
	}

	setParts := []string{"updated_at = NOW()"}
	args := []any{bankID}
	argIdx := 2

	if name != nil {
		setParts = append(setParts, fmt.Sprintf("name = $%d", argIdx))
		args = append(args, *name)
		argIdx++
	}
	if mission != nil {
		setParts = append(setParts, fmt.Sprintf("mission = $%d", argIdx))
		args = append(args, *mission)
		argIdx++
	}
	if disposition != nil {
		setParts = append(setParts, fmt.Sprintf("disposition = $%d::jsonb", argIdx))
		args = append(args, mustJSON(disposition))
		argIdx++
	}

	query := `
		UPDATE ` + s.table("banks") + `
		SET ` + strings.Join(setParts, ", ") + `
		WHERE bank_id = $1
	`
	if _, err := s.pool.Exec(ctx, query, args...); err != nil {
		return nil, err
	}

	return s.GetBank(ctx, bankID)
}

func (s *Store) DeleteBank(ctx context.Context, bankID string) (deletedCount int, err error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback(ctx)
		}
	}()

	var docsCount, unitsCount, entitiesCount int64

	err = tx.QueryRow(ctx,
		`SELECT COUNT(*) FROM `+s.table("documents")+` WHERE bank_id = $1`, bankID).Scan(&docsCount)
	if err != nil {
		return 0, err
	}
	_, err = tx.Exec(ctx, `DELETE FROM `+s.table("documents")+` WHERE bank_id = $1`, bankID)
	if err != nil {
		return 0, err
	}

	err = tx.QueryRow(ctx,
		`SELECT COUNT(*) FROM `+s.table("memory_units")+` WHERE bank_id = $1`, bankID).Scan(&unitsCount)
	if err != nil {
		return 0, err
	}
	_, err = tx.Exec(ctx, `DELETE FROM `+s.table("memory_units")+` WHERE bank_id = $1`, bankID)
	if err != nil {
		return 0, err
	}

	err = tx.QueryRow(ctx,
		`SELECT COUNT(*) FROM `+s.table("entities")+` WHERE bank_id = $1`, bankID).Scan(&entitiesCount)
	if err != nil {
		return 0, err
	}
	_, err = tx.Exec(ctx, `DELETE FROM `+s.table("entities")+` WHERE bank_id = $1`, bankID)
	if err != nil {
		return 0, err
	}

	var internalID *string
	err = tx.QueryRow(ctx,
		`DELETE FROM `+s.table("banks")+` WHERE bank_id = $1 RETURNING internal_id`, bankID).Scan(&internalID)
	if err != nil && err != pgx.ErrNoRows {
		return 0, err
	}

	if err := tx.Commit(ctx); err != nil {
		return 0, err
	}

	// Post-commit: drop vector indexes. Failures are logged but not returned.
	if internalID != nil {
		for ft := range bankIndexFactTypes {
			idx := bankIndexName(ft, *internalID)
			_, dropErr := s.pool.Exec(ctx, `DROP INDEX IF EXISTS `+s.schema+`.`+idx)
			if dropErr != nil {
				// Observability: log but do not fail the overall delete.
				// In a real service we'd use s.deps.Logger here; for now we swallow.
				_ = dropErr
			}
		}
	}

	return int(docsCount + unitsCount + entitiesCount), nil
}

func (s *Store) createBankVectorIndexes(ctx context.Context, bankID, internalID string) error {
	escaped := strings.ReplaceAll(bankID, "'", "''")
	for ft := range bankIndexFactTypes {
		idx := bankIndexName(ft, internalID)
		query := fmt.Sprintf(
			"CREATE INDEX IF NOT EXISTS %s ON %s USING hnsw (embedding vector_cosine_ops) WHERE fact_type = '%s' AND bank_id = '%s'",
			idx, s.table("memory_units"), ft, escaped,
		)
		if _, err := s.pool.Exec(ctx, query); err != nil {
			return err
		}
	}
	return nil
}

func mustJSON(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return string(b)
}

func copyDisposition(src map[string]int) map[string]int {
	dst := make(map[string]int, len(src))
	for k, v := range src {
		dst[k] = v
	}
	return dst
}

func (s *Store) ListBanks(ctx context.Context) ([]BankListItem, error) {
	query := `
		SELECT bank_id, name, disposition, mission, created_at, updated_at
		FROM ` + s.table("banks") + `
		ORDER BY updated_at DESC
	`
	rows, err := s.pool.Query(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var banks []BankListItem
	for rows.Next() {
		var item BankListItem
		var dispositionBytes []byte
		var mission *string
		var createdAt, updatedAt *time.Time
		if err := rows.Scan(
			&item.BankID,
			&item.Name,
			&dispositionBytes,
			&mission,
			&createdAt,
			&updatedAt,
		); err != nil {
			return nil, err
		}
		if len(dispositionBytes) > 0 {
			if err := json.Unmarshal(dispositionBytes, &item.Disposition); err != nil {
				return nil, err
			}
		}
		if mission != nil {
			item.Mission = *mission
		}
		item.CreatedAt = formatTime(createdAt)
		item.UpdatedAt = formatTime(updatedAt)
		banks = append(banks, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if banks == nil {
		banks = []BankListItem{}
	}
	return banks, nil
}

func formatTime(value *time.Time) *string {
	if value == nil {
		return nil
	}
	formatted := value.Format(time.RFC3339Nano)
	return &formatted
}
