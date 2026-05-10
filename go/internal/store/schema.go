package store

import "context"

// EnsureSchema applies lightweight schema additions that the Go server needs
// but that are not covered by the Python migration baseline. The core tables
// (banks, memory_units, chunks, …) are expected to already exist.
func (s *Store) EnsureSchema(ctx context.Context) error {
	_, err := s.pool.Exec(ctx,
		`ALTER TABLE `+s.table("memory_units")+
			` ADD COLUMN IF NOT EXISTS confidence_score double precision`,
	)
	return err
}
