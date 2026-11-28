package clientgo

// HistoryChangeResponse represents a single field change in task history.
//
//	@Description	Field-level task change with old and new values.
type HistoryChangeResponse struct {
	Field    string  `json:"field"`
	OldValue *string `json:"old_value"`
	NewValue *string `json:"new_value"`
}

// HistoryEntryResponse represents a single task history entry.
//
//	@Description	Task history entry with actor and field changes.
type HistoryEntryResponse struct {
	ID        int64                   `json:"id"`
	Action    string                  `json:"action"`
	Actor     UserBrief               `json:"actor"`
	Changes   []HistoryChangeResponse `json:"changes"`
	CreatedAt string                  `json:"created_at"`
}

// FeedResponse represents a paginated task activity feed.
//
//	@Description	Paginated activity feed with total count.
type FeedResponse struct {
	Items []HistoryEntryResponse `json:"items"`
	Total int                    `json:"total"`
}
