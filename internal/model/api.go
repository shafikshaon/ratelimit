package model

type Tier struct {
	ID          int    `json:"id"`
	Tier        int    `json:"tier"`
	Scope       string `json:"scope"`
	RedisKey    string `json:"redis_key"`
	Window      *int   `json:"window,omitempty"`
	Unit        string `json:"unit"`
	MaxRequests int    `json:"max_requests"`
	ActionMode  string `json:"action_mode"`
	Enabled     bool   `json:"enabled"`
	ResetHour   *int   `json:"reset_hour,omitempty"`
}

type API struct {
	ID        int    `json:"id"`
	Name      string `json:"name"`
	GroupName string `json:"group"`
	Tiers     []Tier `json:"tiers"`
}

type APIGroup struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
	APIs  []API  `json:"apis"`
}
