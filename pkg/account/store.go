package account

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

// Store is a thread-safe, file-backed collection of accounts.
type Store struct {
	path string

	mu       sync.RWMutex
	accounts map[string]*Account
	def      string // default account name
}

type persisted struct {
	Default  string     `json:"default"`
	Accounts []*Account `json:"accounts"`
}

// DefaultPath returns the standard store location (~/.agentx/accounts.json),
// overridable via the AGENTX_HOME environment variable.
func DefaultPath() string {
	if h := os.Getenv("AGENTX_HOME"); h != "" {
		return filepath.Join(h, "accounts.json")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "accounts.json"
	}
	return filepath.Join(home, ".agentx", "accounts.json")
}

// Open loads the store at path, creating an empty one if it does not exist.
func Open(path string) (*Store, error) {
	if path == "" {
		path = DefaultPath()
	}
	s := &Store{path: path, accounts: map[string]*Account{}}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return s, nil
	}
	if err != nil {
		return nil, fmt.Errorf("account: read store: %w", err)
	}
	var p persisted
	if err := json.Unmarshal(data, &p); err != nil {
		return nil, fmt.Errorf("account: parse store: %w", err)
	}
	for _, a := range p.Accounts {
		s.accounts[a.Name] = a
	}
	s.def = p.Default
	return s, nil
}

func (s *Store) saveLocked() error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return fmt.Errorf("account: create store dir: %w", err)
	}
	p := persisted{Default: s.def}
	for _, a := range s.accounts {
		p.Accounts = append(p.Accounts, a)
	}
	sort.Slice(p.Accounts, func(i, j int) bool { return p.Accounts[i].Name < p.Accounts[j].Name })
	data, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return err
	}
	// 0600: the file contains session secrets.
	if err := os.WriteFile(s.path, data, 0o600); err != nil {
		return fmt.Errorf("account: write store: %w", err)
	}
	return nil
}

// Add inserts or replaces an account and persists the store. The first account
// added becomes the default.
func (s *Store) Add(a *Account) error {
	if err := a.Validate(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if a.AddedAt.IsZero() {
		a.AddedAt = time.Now()
	}
	s.accounts[a.Name] = a
	if s.def == "" {
		s.def = a.Name
	}
	return s.saveLocked()
}

// Remove deletes an account by name.
func (s *Store) Remove(name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.accounts[name]; !ok {
		return fmt.Errorf("account %q not found", name)
	}
	delete(s.accounts, name)
	if s.def == name {
		s.def = ""
		for n := range s.accounts {
			s.def = n
			break
		}
	}
	return s.saveLocked()
}

// Get returns a copy of the named account.
func (s *Store) Get(name string) (*Account, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	a, ok := s.accounts[name]
	if !ok {
		return nil, fmt.Errorf("account %q not found", name)
	}
	cp := *a
	return &cp, nil
}

// Resolve returns the named account, or the default when name is empty.
func (s *Store) Resolve(name string) (*Account, error) {
	if name != "" {
		return s.Get(name)
	}
	s.mu.RLock()
	def := s.def
	s.mu.RUnlock()
	if def == "" {
		return nil, fmt.Errorf("no accounts configured; add one first")
	}
	return s.Get(def)
}

// List returns all accounts sorted by name.
func (s *Store) List() []*Account {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*Account, 0, len(s.accounts))
	for _, a := range s.accounts {
		cp := *a
		out = append(out, &cp)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// Default returns the default account name (may be empty).
func (s *Store) Default() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.def
}

// SetDefault changes the default account.
func (s *Store) SetDefault(name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.accounts[name]; !ok {
		return fmt.Errorf("account %q not found", name)
	}
	s.def = name
	return s.saveLocked()
}

// Touch records that an account was just used.
func (s *Store) Touch(name, screenName string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if a, ok := s.accounts[name]; ok {
		a.LastUsed = time.Now()
		if screenName != "" {
			a.ScreenName = screenName
		}
		_ = s.saveLocked()
	}
}
