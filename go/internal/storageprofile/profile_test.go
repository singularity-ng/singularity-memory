package storageprofile

import (
	"testing"
)

func TestParseProfile(t *testing.T) {
	tests := []struct {
		input    string
		want     Profile
		wantErr  bool
	}{
		{"pg0", PG0, false},
		{"PG0", PG0, false},
		{"Pg0", PG0, false},
		{"pgvector", PGVECTOR, false},
		{"PGVECTOR", PGVECTOR, false},
		{"PgVector", PGVECTOR, false},
		{"vchord", VCHORD, false},
		{"VCHORD", VCHORD, false},
		{"VChord", VCHORD, false},
		{"", "", true},
		{"unknown", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got, err := ParseProfile(tt.input)
			if (err != nil) != tt.wantErr {
				t.Fatalf("ParseProfile(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
			}
			if got != tt.want {
				t.Fatalf("ParseProfile(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestProfileSupportsVectors(t *testing.T) {
	tests := []struct {
		profile Profile
		want    bool
	}{
		{PG0, false},
		{PGVECTOR, true},
		{VCHORD, true},
	}

	for _, tt := range tests {
		t.Run(tt.profile.String(), func(t *testing.T) {
			if got := tt.profile.SupportsVectors(); got != tt.want {
				t.Fatalf("SupportsVectors() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestProfileSupportsBM25(t *testing.T) {
	tests := []struct {
		profile Profile
		want    bool
	}{
		{PG0, false},
		{PGVECTOR, false},
		{VCHORD, true},
	}

	for _, tt := range tests {
		t.Run(tt.profile.String(), func(t *testing.T) {
			if got := tt.profile.SupportsBM25(); got != tt.want {
				t.Fatalf("SupportsBM25() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestProfileDefaultDimensions(t *testing.T) {
	tests := []struct {
		profile Profile
		want    int
	}{
		{PG0, 0},
		{PGVECTOR, 768},
		{VCHORD, 768},
	}

	for _, tt := range tests {
		t.Run(tt.profile.String(), func(t *testing.T) {
			if got := tt.profile.DefaultDimensions(); got != tt.want {
				t.Fatalf("DefaultDimensions() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestAllProfiles(t *testing.T) {
	if len(AllProfiles) != 3 {
		t.Fatalf("len(AllProfiles) = %d, want 3", len(AllProfiles))
	}
}
