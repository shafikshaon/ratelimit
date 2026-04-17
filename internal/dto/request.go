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

// CreateOverrideRequest is the body for POST /apis/:name/overrides.
type CreateOverrideRequest struct {
	Wallet string `json:"wallet"`
	T1     string `json:"t1"`
	T2     string `json:"t2"`
	T3     string `json:"t3"`
	Reason string `json:"reason"`
}
