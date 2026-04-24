package model

// Tier is the rate-limit configuration for one tier stored in PostgreSQL.
type Tier struct {
	ID          int    `json:"-"`
	Tier        int    `json:"tier"`
	Scope       string `json:"scope"`
	RedisKey    string `json:"redis_key"`
	Window      *int   `json:"window,omitempty"`
	Unit        string `json:"unit"`
	MaxRequests int    `json:"max_requests"`
	ResetHour   int    `json:"reset_hour"`
}

// Override is a per-wallet rate-limit override stored in ScyllaDB.
type Override struct {
	Wallet string `json:"wallet"`
	T1     string `json:"t1"`
	T2     string `json:"t2"`
	T3     string `json:"t3"`
	Reason string `json:"reason"`
}

// ResolvedTier is one tier with the global limit and the wallet-effective limit merged.
type ResolvedTier struct {
	Tier         int    `json:"tier"`
	Scope        string `json:"scope"`
	RedisKey     string `json:"redis_key"`
	Window       *int   `json:"window,omitempty"`
	Unit         string `json:"unit"`
	GlobalMax    int    `json:"global_max"`
	EffectiveMax int    `json:"effective_max"`
	Overridden   bool   `json:"overridden"`
	ResetHour    int    `json:"reset_hour"`
}

// ResolvedConfig is the full wallet-resolved configuration for one API.
type ResolvedConfig struct {
	API    string         `json:"api"`
	Wallet string         `json:"wallet"`
	Tiers  []ResolvedTier `json:"tiers"`
}

// API is used for the list endpoint sidebar (Tiers omitted) and the detail endpoint.
type API struct {
	ID        int    `json:"-"`
	Name      string `json:"name"`
	GroupName string `json:"group"`
	Tiers     []Tier `json:"tiers,omitempty"`
}

// APIGroup groups APIs by their group_name.
type APIGroup struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
	APIs  []API  `json:"apis"`
}
