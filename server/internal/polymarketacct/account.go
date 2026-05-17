// Package polymarketacct holds the shared Polymarket credential document shape
// (JSON on disk and previously Badger) without importing store or accountsfile.
package polymarketacct

import "time"

// Account is a saved Polymarket API identity (file or legacy Badger migration).
type Account struct {
	ID            string    `json:"id"`
	Name          string    `json:"name"`
	APIKey        string    `json:"apiKey"`
	Secret        string    `json:"secret"`
	Passphrase    string    `json:"passphrase"`
	PrivateKey    string    `json:"privateKey"`
	FunderAddress string    `json:"funderAddress"`
	IsActive      bool      `json:"isActive"`
	CreatedAt     time.Time `json:"createdAt"`
	UpdatedAt     time.Time `json:"updatedAt"`
}
