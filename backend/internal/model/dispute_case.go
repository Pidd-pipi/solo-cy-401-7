package model

import (
	"encoding/json"
	"time"

	"gorm.io/gorm"
)

// DisputeCase is a dispute opened by either contract party.
// At most one open case may exist per contract (unique index on
// contract_id + active_dispute_id via contract freeze flag).
type DisputeCase struct {
	ID            uint    `gorm:"primaryKey" json:"id"`
	CaseNo        string  `gorm:"size:64;uniqueIndex;not null" json:"caseNo"`
	ContractID    uint    `gorm:"index;not null" json:"contractId"`
	OpenerID      uint    `gorm:"index;not null" json:"openerId"`
	OpenerSide    string  `gorm:"size:16;not null" json:"openerSide"` // party_a / party_b
	Reason        string  `gorm:"size:512;not null" json:"reason"`
	Claim         string  `gorm:"size:512;not null" json:"claim"`
	StageSnapshot string  `gorm:"size:24" json:"stageSnapshot"` // 立案时合同当前阶段/状态
	Status        string  `gorm:"size:16;index;not null;default:open" json:"status"`
	Verdict       string  `gorm:"size:24" json:"verdict"`
	PartyARatio   float64 `gorm:"type:decimal(7,4);not null;default:0" json:"partyARatio"`
	PartyBRatio   float64 `gorm:"type:decimal(7,4);not null;default:0" json:"partyBRatio"`
	PartyAAmount  float64 `gorm:"type:decimal(14,2);not null;default:0" json:"partyAAmount"`
	PartyBAmount  float64 `gorm:"type:decimal(14,2);not null;default:0" json:"partyBAmount"`
	TotalSettled  float64 `gorm:"type:decimal(14,2);not null;default:0" json:"totalSettled"`
	RulingNote    string  `gorm:"size:512" json:"rulingNote"`
	// ResolverID is NULL until an admin actually rules: a case with no resolver
	// yet must never point at a non-existent user (foreign key).
	ResolverID *uint      `gorm:"index" json:"resolverId"`
	ResolvedAt *time.Time `json:"resolvedAt"`
	// WithdrawnByID is NULL until a party withdraws the case (foreign key).
	WithdrawnByID   *uint      `gorm:"index" json:"withdrawnById"`
	WithdrawnReason string     `gorm:"size:255" json:"withdrawnReason"`
	WithdrawnAt     *time.Time `json:"withdrawnAt"`
	// Version is an optimistic-concurrency counter. Supplement/withdraw/resolve
	// claim an open case with a version-guarded UPDATE, so concurrent mutations
	// cannot both succeed even if row-lock waiting would otherwise serialize them.
	Version   uint      `gorm:"not null;default:0" json:"version"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"-"`

	// Associations.
	Materials []DisputeMaterial `gorm:"foreignKey:CaseID" json:"materials"`
	Contract  *Contract         `gorm:"foreignKey:ContractID" json:"contract,omitempty"`
	Opener    *User             `gorm:"foreignKey:OpenerID" json:"opener,omitempty"`
	Resolver  *User             `gorm:"foreignKey:ResolverID" json:"resolver,omitempty"`
}

// DisputeMaterial is a piece of evidence or a supplemental statement.
type DisputeMaterial struct {
	ID            uint      `gorm:"primaryKey" json:"id"`
	CaseID        uint      `gorm:"index;not null" json:"caseId"`
	SubmitterID   uint      `gorm:"index;not null" json:"submitterId"`
	SubmitterSide string    `gorm:"size:16;not null" json:"submitterSide"`           // party_a / party_b
	Kind          string    `gorm:"size:16;not null;default:supplement" json:"kind"` // open / supplement
	Content       string    `gorm:"size:2000;not null" json:"content"`
	EvidenceJS    string    `gorm:"column:evidence;type:text" json:"-"`
	CreatedAt     time.Time `json:"createdAt"`

	// Computed field.
	Evidence []string `gorm:"-" json:"evidence"`

	// Associations (FK: case_id -> dispute_cases via the Materials back-reference,
	// submitter_id -> users).
	Submitter *User `gorm:"foreignKey:SubmitterID" json:"submitter,omitempty"`
}

// BeforeSave serializes the evidence array.
func (m *DisputeMaterial) BeforeSave(_ *gorm.DB) error {
	if m.Evidence != nil {
		raw, err := json.Marshal(m.Evidence)
		if err != nil {
			return err
		}
		m.EvidenceJS = string(raw)
	}
	return nil
}

// AfterFind restores the evidence array.
func (m *DisputeMaterial) AfterFind(_ *gorm.DB) error {
	m.Evidence = []string{}
	if m.EvidenceJS != "" {
		_ = json.Unmarshal([]byte(m.EvidenceJS), &m.Evidence)
	}
	return nil
}
