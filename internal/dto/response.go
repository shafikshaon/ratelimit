package dto

import "github.com/shafikshaon/ratelimit/internal/model"

// ── List / Detail ─────────────────────────────────────────────────────────────

type APIListResponse struct {
	Data []model.APIGroup `json:"data"`
}

type APIDetailResponse struct {
	Data APIDetail `json:"data"`
}

type APIDetail struct {
	Name  string       `json:"name"`
	Tiers []model.Tier `json:"tiers"`
}

// ── Wallet config ─────────────────────────────────────────────────────────────

// WalletConfigResponse is returned by GET /apis/:name/config/:wallet.
// Re-uses the model type directly — no translation needed.
type WalletConfigResponse = model.ResolvedConfig

// ── Rate-limit check ──────────────────────────────────────────────────────────

type TierCheckResult struct {
	Tier    int    `json:"tier"`
	Scope   string `json:"scope"`
	Status  string `json:"status"` // "pass" | "blocked" | "skipped"
	Used    int64  `json:"used"`
	Limit   int    `json:"limit"`
	ScopeID string `json:"scope_id"`
}

type CheckResponse struct {
	Allowed     bool              `json:"allowed"`
	TierResults []TierCheckResult `json:"tier_results"`
}

// ── Usage ─────────────────────────────────────────────────────────────────────

type TierUsage struct {
	Tier    int    `json:"tier"`
	Scope   string `json:"scope"`
	Used    int64  `json:"used"`
	Limit   int    `json:"limit"`
	ScopeID string `json:"scope_id"`
	Window  string `json:"window"`
	ResetIn int64  `json:"reset_in"` // seconds until window resets; -1 = no active window
}

type UsageResponse struct {
	Data []TierUsage `json:"data"`
}

// ── Overrides ─────────────────────────────────────────────────────────────────

type OverridePageResponse struct {
	Data          []model.Override `json:"data"`
	NextPageToken string           `json:"next_page_token,omitempty"`
	HasMore       bool             `json:"has_more"`
}

// ── Redis export ──────────────────────────────────────────────────────────────

type RedisKeyEntry struct {
	Key        string `json:"key"`
	Value      string `json:"value"`
	TTLSeconds int64  `json:"ttl_seconds"` // -1 = no expiry
}

type RedisExportResponse struct {
	Total int             `json:"total"`
	Keys  []RedisKeyEntry `json:"keys"`
}
