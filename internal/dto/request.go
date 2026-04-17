package dto

// UpdateTierRequest is the body for PATCH /apis/:name/tiers/:tier.
type UpdateTierRequest struct {
	Window      *int   `json:"window"`
	Unit        string `json:"window_unit"`
	MaxRequests int    `json:"max_requests"`
	ResetHour   int    `json:"reset_hour"`
}

// CheckRequest is the body for POST /apis/:name/check.
type CheckRequest struct {
	Email  string `json:"email"`
	Wallet string `json:"wallet"`
}

// FingerprintTokenRequest is the body for POST /fingerprint/token.
type FingerprintTokenRequest struct {
	Nonce       string `json:"nonce"`
	HMACNonce   string `json:"hmac_nonce"`
	FPHash      string `json:"fp_hash"`
	PowSolution string `json:"pow_solution"`  // hex string; must satisfy PoW difficulty
	CollectedAt int64  `json:"collected_at"`  // unix seconds when signals were collected
	BotSignals  struct {
		Webdriver    bool `json:"webdriver"`
		OuterWidth   int  `json:"outer_width"`
		OuterHeight  int  `json:"outer_height"`
		PluginCount  int  `json:"plugin_count"`
		ChromeObject bool `json:"chrome_object"`
	} `json:"bot_signals"`
}

// CreateOverrideRequest is the body for POST /apis/:name/overrides.
type CreateOverrideRequest struct {
	Wallet string `json:"wallet"`
	T1     string `json:"t1"`
	T2     string `json:"t2"`
	T3     string `json:"t3"`
	Reason string `json:"reason"`
}
