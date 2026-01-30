package tunnel

import (
	"time"
)

// AuthStore persists credentials.
type AuthStore interface {
	Save(cred *Credential) error
	Load(server string) (*Credential, error)
	Delete(server string) error
	List() ([]*Credential, error)
}

// Credential stores authentication tokens.
type Credential struct {
	Server       string
	RefreshToken string
	AccessToken  string
	ExpiresAt    time.Time
	Default      bool
}

func (c *Credential) isValid() bool {
	return c.RefreshToken != ""
}

func (c *Credential) isExpired() bool {
	return time.Now().After(c.ExpiresAt)
}
