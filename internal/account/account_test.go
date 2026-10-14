package account

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestNormalizeDocumentNumber(t *testing.T) {
	t.Parallel()

	cases := []struct {
		in   string
		want string
	}{
		{"  12345678900  ", "12345678900"},
		{"123.456.789-00", "123.456.789-00"},
		{"AB-12", "AB-12"},
		{"550e8400-e29b-41d4-a716-446655440000", "550e8400-e29b-41d4-a716-446655440000"},
		{"  12-3  ", "12-3"},
	}
	for _, tc := range cases {
		got, err := NormalizeDocumentNumber(tc.in)
		if err != nil {
			t.Fatalf("NormalizeDocumentNumber(%q) error: %v", tc.in, err)
		}
		if got != tc.want {
			t.Fatalf("NormalizeDocumentNumber(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}

	bad := []string{"", "   ", "ba\nd", "ba\td", strings.Repeat("a", 37)}
	for _, in := range bad {
		if _, err := NormalizeDocumentNumber(in); !errors.Is(err, ErrInvalidDocument) {
			t.Fatalf("NormalizeDocumentNumber(%q) err = %v", in, err)
		}
	}
}

func TestServiceCreateAndGet(t *testing.T) {
	t.Parallel()

	repo := newMemRepo()
	svc := NewService(repo)
	ctx := context.Background()

	created, err := svc.Create(ctx, "12345678900")
	if err != nil {
		t.Fatal(err)
	}
	if created.ID != 1 || created.DocumentNumber != "12345678900" {
		t.Fatalf("created %+v", created)
	}

	got, err := svc.Get(ctx, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got != created {
		t.Fatalf("get %+v", got)
	}

	if _, err := svc.Create(ctx, "12345678900"); !errors.Is(err, ErrConflict) {
		t.Fatalf("duplicate err = %v", err)
	}
	if _, err := svc.Get(ctx, 99); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing err = %v", err)
	}
	if _, err := svc.Get(ctx, 0); !errors.Is(err, ErrNotFound) {
		t.Fatalf("zero id err = %v", err)
	}
	if _, err := svc.Get(ctx, -1); !errors.Is(err, ErrNotFound) {
		t.Fatalf("negative id err = %v", err)
	}
	formatted, err := svc.Create(ctx, "  12-3  ")
	if err != nil {
		t.Fatal(err)
	}
	if formatted.DocumentNumber != "12-3" {
		t.Fatalf("stored %q", formatted.DocumentNumber)
	}
	if _, err := svc.Create(ctx, "\n"); !errors.Is(err, ErrInvalidDocument) {
		t.Fatalf("invalid err = %v", err)
	}
}

type memRepo struct {
	byDoc map[string]Account
	byID  map[int64]Account
	next  int64
}

func newMemRepo() *memRepo {
	return &memRepo{
		byDoc: map[string]Account{},
		byID:  map[int64]Account{},
		next:  1,
	}
}

func (m *memRepo) Create(_ context.Context, documentNumber string) (Account, error) {
	if _, ok := m.byDoc[documentNumber]; ok {
		return Account{}, ErrConflict
	}
	acc := Account{ID: m.next, DocumentNumber: documentNumber}
	m.next++
	m.byDoc[documentNumber] = acc
	m.byID[acc.ID] = acc
	return acc, nil
}

func (m *memRepo) Get(_ context.Context, id int64) (Account, error) {
	acc, ok := m.byID[id]
	if !ok {
		return Account{}, ErrNotFound
	}
	return acc, nil
}
