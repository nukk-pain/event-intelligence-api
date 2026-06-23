package api

// envelope.go defines the shared response envelope reused by every collection
// endpoint (events list, changes feed, sources). The wire shape is:
//
//	{"data": <payload>, "page": {"next_cursor": str|null, "has_more": bool, "limit": int}}
//
// Keeping ONE envelope type means 3.2–3.5 serialize identically and the cursor
// contract is defined in a single place.

// Envelope is the top-level JSON response for collection endpoints. Data holds
// the resource payload (typically a slice); Page carries pagination metadata.
// CategoryCounts is the OPTIONAL category facet, present only on the events list
// (omitempty keeps it absent from the changes/sources feeds that reuse this
// envelope and from older clients' expectations).
type Envelope struct {
	Data           any             `json:"data"`
	Page           PageInfo        `json:"page"`
	CategoryCounts []CategoryCount `json:"category_counts,omitempty"`
}

// CategoryCount is one entry of the events-list category facet: a taxonomy slug
// and the number of non-excluded events in it under the active (non-category)
// filters. The list is emitted in the canonical taxonomy order so the wire shape
// is deterministic (stable ETags) and the UI can render chips in a fixed order.
type CategoryCount struct {
	Category string `json:"category"`
	Count    int    `json:"count"`
}

// PageInfo is the pagination block. NextCursor is nil (JSON null) on the last
// page. Limit echoes the EFFECTIVE limit actually applied after clamping, so a
// caller that requested limit=101 sees the clamped value (e.g. 100).
type PageInfo struct {
	NextCursor *string `json:"next_cursor"`
	HasMore    bool    `json:"has_more"`
	Limit      int     `json:"limit"`
}
