package model

type Tier struct {
	ID          int    `json:"id"`
	Tier        int    `json:"tier"`
	Scope       string `json:"scope"`
	RedisKey    string `json:"redis_key"`
	Window      *int   `json:"window,omitempty"`
	Unit        string `json:"unit"`
	MaxRequests int    `json:"max_requests"`
	ResetHour   int    `json:"reset_hour"`
}

type Override struct {
	Wallet string `json:"wallet"`
	T1     string `json:"t1"`
	T2     string `json:"t2"`
	T3     string `json:"t3"`
	Reason string `json:"reason"`
}

type OverridePage struct {
	Data          []Override `json:"data"`
	NextPageToken string     `json:"next_page_token,omitempty"`
	HasMore       bool       `json:"has_more"`
}

// ResolvedTier is one tier with both the global limit and the wallet-effective limit.
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

// ResolvedConfig is the full, wallet-resolved configuration for one API.
// Fetched via a single Redis MGET; ScyllaDB consulted only on cache miss.
type ResolvedConfig struct {
	API    string         `json:"api"`
	Wallet string         `json:"wallet"`
	Tiers  []ResolvedTier `json:"tiers"`
}

// API is used for both the list endpoint (Tiers omitted) and the detail endpoint.
type API struct {
	ID        int    `json:"id"`
	Name      string `json:"name"`
	GroupName string `json:"group"`
	Tiers     []Tier `json:"tiers,omitempty"`
}

type APIGroup struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
	APIs  []API  `json:"apis"`
}

// TierCheckResult is the per-tier outcome of a rate-limit check.
type TierCheckResult struct {
	Tier    int    `json:"tier"`
	Scope   string `json:"scope"`
	Status  string `json:"status"` // "pass", "blocked", "skipped"
	Used    int64  `json:"used"`
	Limit   int    `json:"limit"`
	ScopeID string `json:"scope_id"`
}

// CheckResponse is returned by POST /apis/:name/check.
type CheckResponse struct {
	Allowed     bool              `json:"allowed"`
	TierResults []TierCheckResult `json:"tier_results"`
}

// TierUsage is the read-only usage snapshot for one tier.
type TierUsage struct {
	Tier    int    `json:"tier"`
	Scope   string `json:"scope"`
	Used    int64  `json:"used"`
	Limit   int    `json:"limit"`
	ScopeID string `json:"scope_id"`
	Window  string `json:"window"`
	ResetIn int64  `json:"reset_in"` // seconds until window resets; -1 = no active window
}
