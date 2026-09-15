package dto

// OpenDisputeRequest is submitted by either contract party.
type OpenDisputeRequest struct {
	Reason   string   `json:"reason" validate:"required,min=5,max=500"`
	Claim    string   `json:"claim" validate:"required,min=2,max=500"`
	Evidence []string `json:"evidence" validate:"dive,max=255"`
}

// SupplementDisputeRequest lets either party append material to an open case.
type SupplementDisputeRequest struct {
	Content  string   `json:"content" validate:"required,min=2,max=1000"`
	Evidence []string `json:"evidence" validate:"dive,max=255"`
	// Version is the client's observed case version for optimistic concurrency.
	// Omitted: the server uses the version it reads at request entry. Supplying
	// a stale version returns 409, so concurrently started mutations cannot both
	// succeed.
	Version *uint `json:"version" validate:"omitempty"`
}

// WithdrawDisputeRequest lets the opener withdraw the open case.
type WithdrawDisputeRequest struct {
	Reason  string `json:"reason" validate:"omitempty,max=255"`
	Version *uint  `json:"version" validate:"omitempty"`
}

// ResolveDisputeRequest is the admin ruling payload.
// For verdict=proportional, PartyARatio + PartyBRatio must equal 1.0
// (money is conserved: both sides together receive the full contract amount).
type ResolveDisputeRequest struct {
	Verdict     string  `json:"verdict" validate:"required,oneof=full_to_b full_refund_a proportional"`
	PartyARatio float64 `json:"partyARatio" validate:"min=0,max=1"`
	PartyBRatio float64 `json:"partyBRatio" validate:"min=0,max=1"`
	Note        string  `json:"note" validate:"omitempty,max=500"`
	Version     *uint   `json:"version" validate:"omitempty"`
}
