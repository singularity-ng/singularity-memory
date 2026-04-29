package storageprofile

import (
	"fmt"
	"strings"
)

// Profile identifies the storage backend and its indexing capabilities.
type Profile string

const (
	PG0      Profile = "pg0"
	PGVECTOR Profile = "pgvector"
	VCHORD   Profile = "vchord"
)

// AllProfiles is the set of supported profiles for iteration and validation.
var AllProfiles = []Profile{PG0, PGVECTOR, VCHORD}

// ParseProfile resolves a string to a known Profile, case-insensitive.
func ParseProfile(s string) (Profile, error) {
	switch strings.ToLower(s) {
	case "pg0":
		return PG0, nil
	case "pgvector":
		return PGVECTOR, nil
	case "vchord":
		return VCHORD, nil
	default:
		return "", fmt.Errorf("unknown storage profile %q", s)
	}
}

// SupportsVectors reports whether the profile stores vector embeddings.
func (p Profile) SupportsVectors() bool {
	switch p {
	case PGVECTOR, VCHORD:
		return true
	default:
		return false
	}
}

// SupportsBM25 reports whether the profile supports BM25 full-text search.
func (p Profile) SupportsBM25() bool {
	switch p {
	case VCHORD:
		return true
	default:
		return false
	}
}

// DefaultDimensions returns the default embedding dimension for the profile.
// Profiles that do not store vectors return 0.
func (p Profile) DefaultDimensions() int {
	switch p {
	case PGVECTOR, VCHORD:
		return 768
	default:
		return 0
	}
}

// String returns the canonical string representation.
func (p Profile) String() string {
	return string(p)
}
