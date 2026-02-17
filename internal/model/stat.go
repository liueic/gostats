package model

type StatResponse struct {
	Source    string  `json:"source"`
	Key       string  `json:"key"`
	Metric    string  `json:"metric"`
	Label     string  `json:"label"`
	Failed    bool    `json:"failed"`
	Count     any     `json:"count"`
	Unit      string  `json:"unit,omitempty"`
	Data      any     `json:"data,omitempty"`
	UpdatedAt string  `json:"updatedAt"`
	Error     *string `json:"error,omitempty"`
}
