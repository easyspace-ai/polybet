// Package accountsfile persists Polymarket API accounts as JSON on disk (not Badger).
package accountsfile

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/easyspace-ai/polybet/internal/polymarketacct"
)

const defaultRelative = ".polybet/polymarket-accounts.json"

type diskDoc struct {
	Accounts []polymarketacct.Account `json:"accounts"`
	ActiveID string                   `json:"activeId,omitempty"`
}

// Store is a file-backed account list (thread-safe).
type Store struct {
	mu   sync.Mutex
	path string
}

var (
	defaultStore *Store
	defaultOnce  sync.Once
)

// Default returns the process-wide accounts file store (~/.polybet/polymarket-accounts.json).
func Default() *Store {
	defaultOnce.Do(func() {
		defaultStore = New(defaultPath())
	})
	return defaultStore
}

// ResetDefaultForTest clears the lazy singleton (call between tests).
func ResetDefaultForTest() {
	defaultOnce = sync.Once{}
	defaultStore = nil
}

func defaultPath() string {
	if p := strings.TrimSpace(os.Getenv("POLYBET_ACCOUNTS_FILE")); p != "" {
		return p
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return filepath.Join(".", "polymarket-accounts.json")
	}
	return filepath.Join(home, defaultRelative)
}

// New returns a store bound to path (usually from POLYBET_ACCOUNTS_FILE or defaultPath).
func New(path string) *Store {
	return &Store{path: path}
}

// Path returns the backing file path.
func (s *Store) Path() string {
	if s == nil {
		return ""
	}
	return s.path
}

func (s *Store) readDisk() (diskDoc, error) {
	var doc diskDoc
	if s == nil || strings.TrimSpace(s.path) == "" {
		return doc, errors.New("accountsfile: nil store or path")
	}
	b, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return doc, nil
		}
		return doc, err
	}
	b = bytesTrimBOM(b)
	if err := json.Unmarshal(b, &doc); err != nil {
		return doc, err
	}
	return doc, nil
}

func bytesTrimBOM(b []byte) []byte {
	if len(b) >= 3 && b[0] == 0xEF && b[1] == 0xBB && b[2] == 0xBF {
		return b[3:]
	}
	return b
}

func (s *Store) writeDisk(doc diskDoc) error {
	if s == nil || strings.TrimSpace(s.path) == "" {
		return errors.New("accountsfile: nil store or path")
	}
	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return err
	}
	b = append(b, '\n')
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

func activeIDFromDoc(doc diskDoc) string {
	if id := strings.TrimSpace(doc.ActiveID); id != "" {
		return id
	}
	for _, a := range doc.Accounts {
		if a.IsActive {
			return strings.TrimSpace(a.ID)
		}
	}
	return ""
}

// ReadAccounts returns all accounts (order preserved).
func (s *Store) ReadAccounts(ctx context.Context) ([]polymarketacct.Account, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	doc, err := s.readDisk()
	if err != nil {
		return nil, err
	}
	return append([]polymarketacct.Account(nil), doc.Accounts...), nil
}

// ReadActiveAccountID returns the active account id, if any.
func (s *Store) ReadActiveAccountID(ctx context.Context) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	doc, err := s.readDisk()
	if err != nil {
		return "", err
	}
	return activeIDFromDoc(doc), nil
}

// ReadAccount loads one account by id.
func (s *Store) ReadAccount(ctx context.Context, id string) (*polymarketacct.Account, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	id = strings.TrimSpace(id)
	if id == "" {
		return nil, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	doc, err := s.readDisk()
	if err != nil {
		return nil, err
	}
	for i := range doc.Accounts {
		if doc.Accounts[i].ID == id {
			a := doc.Accounts[i]
			return &a, nil
		}
	}
	return nil, nil
}

func (s *Store) writeSnapshot(ctx context.Context, accounts []polymarketacct.Account, activeID string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	doc := diskDoc{Accounts: append([]polymarketacct.Account(nil), accounts...), ActiveID: strings.TrimSpace(activeID)}
	return s.writeDisk(doc)
}

// WriteSnapshot replaces the entire file (used by migration).
func (s *Store) WriteSnapshot(ctx context.Context, accounts []polymarketacct.Account, activeID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.writeSnapshot(ctx, accounts, activeID)
}

// InsertPolymarketAccount appends or updates logic identical to the former Badger path.
func (s *Store) InsertPolymarketAccount(ctx context.Context, a *polymarketacct.Account) error {
	if a == nil {
		return errors.New("accountsfile: nil account")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	doc, err := s.readDisk()
	if err != nil {
		return err
	}
	list := append([]polymarketacct.Account(nil), doc.Accounts...)
	if a.IsActive {
		for i := range list {
			list[i].IsActive = false
		}
	}
	now := time.Now().UTC()
	if a.CreatedAt.IsZero() {
		a.CreatedAt = now
	}
	a.UpdatedAt = now
	list = append(list, *a)
	activeID := ""
	for _, x := range list {
		if x.IsActive {
			activeID = x.ID
			break
		}
	}
	return s.writeSnapshot(ctx, list, activeID)
}

// DeactivateAllAccounts clears active flags and activeId.
func (s *Store) DeactivateAllAccounts(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	doc, err := s.readDisk()
	if err != nil {
		return err
	}
	for i := range doc.Accounts {
		doc.Accounts[i].IsActive = false
		doc.Accounts[i].UpdatedAt = time.Now().UTC()
	}
	doc.ActiveID = ""
	return s.writeSnapshot(ctx, doc.Accounts, "")
}

// ActivateAccount marks one account active.
func (s *Store) ActivateAccount(ctx context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	doc, err := s.readDisk()
	if err != nil {
		return err
	}
	found := false
	for i := range doc.Accounts {
		doc.Accounts[i].IsActive = doc.Accounts[i].ID == id
		if doc.Accounts[i].ID == id {
			found = true
		}
		doc.Accounts[i].UpdatedAt = time.Now().UTC()
	}
	if !found {
		return errAccountNotFound
	}
	return s.writeSnapshot(ctx, doc.Accounts, id)
}

// DeletePolymarketAccount removes one account by id.
func (s *Store) DeletePolymarketAccount(ctx context.Context, id string) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	doc, err := s.readDisk()
	if err != nil {
		return 0, err
	}
	var out []polymarketacct.Account
	var removed bool
	activeID := ""
	for _, a := range doc.Accounts {
		if a.ID == id {
			removed = true
			continue
		}
		out = append(out, a)
		if a.IsActive {
			activeID = a.ID
		}
	}
	if !removed {
		return 0, nil
	}
	if err := s.writeSnapshot(ctx, out, activeID); err != nil {
		return 0, err
	}
	return 1, nil
}

// CountPolymarketAccounts returns the number of accounts.
func (s *Store) CountPolymarketAccounts(ctx context.Context) (int, error) {
	list, err := s.ReadAccounts(ctx)
	if err != nil {
		return 0, err
	}
	return len(list), nil
}

var errAccountNotFound = errors.New("polymarket account not found")
