package model

import (
	"encoding/json"
	"time"

	"gorm.io/gorm"
)

// ContractStage is one payment milestone of a contract.
type ContractStage struct {
	Name   string  `json:"name"`
	Amount float64 `json:"amount"`
	Status string  `json:"status"` // pending / in_progress / done
	DueAt  string  `json:"dueAt"`
}

// Settlement is the immutable money-allocation snapshot produced by a ruling.
// PartyAAmount + PartyBAmount always equals the settled total (cents-exact).
type Settlement struct {
	CaseID       uint    `json:"caseId"`
	Verdict      string  `json:"verdict"`
	PartyARatio  float64 `json:"partyARatio"`
	PartyBRatio  float64 `json:"partyBRatio"`
	PartyAAmount float64 `json:"partyAAmount"` // 退回甲方
	PartyBAmount float64 `json:"partyBAmount"` // 支付乙方
	TotalAmount  float64 `json:"totalAmount"`
	Note         string  `json:"note"`
}

// Contract is the signed agreement between requester and freelancer.
type Contract struct {
	ID              uint      `gorm:"primaryKey" json:"id"`
	ContractNo      string    `gorm:"size:64;uniqueIndex;not null" json:"contractNo"`
	TotalAmount     float64   `gorm:"type:decimal(14,2);not null" json:"totalAmount"`
	PaymentType     string    `gorm:"size:16;not null;default:one_time" json:"paymentType"`
	StagesJS        string    `gorm:"column:stages;type:text" json:"-"`
	Status          string    `gorm:"size:24;not null;default:pending_signature" json:"status"`
	ActiveDisputeID *uint     `gorm:"column:active_dispute_id;uniqueIndex" json:"activeDisputeId"`
	SettlementJS    string    `gorm:"column:settlement;type:text" json:"-"`
	RequirementID   uint      `gorm:"index;not null" json:"requirementId"`
	PartyAID        uint      `gorm:"index;not null" json:"partyAId"`
	PartyBID        uint      `gorm:"index;not null" json:"partyBId"`
	CreatedAt       time.Time `json:"createdAt"`
	UpdatedAt       time.Time `json:"-"`

	// Computed fields.
	Stages      []ContractStage `gorm:"-" json:"stages"`
	Settlement  *Settlement     `gorm:"-" json:"settlement,omitempty"`
	PartyA      *User           `gorm:"foreignKey:PartyAID" json:"partyA"`
	PartyB      *User           `gorm:"foreignKey:PartyBID" json:"partyB"`
	Requirement *Requirement    `gorm:"foreignKey:RequirementID" json:"requirement"`
}

// BeforeSave serializes stages and settlement.
func (c *Contract) BeforeSave(_ *gorm.DB) error {
	if c.Stages != nil {
		raw, err := json.Marshal(c.Stages)
		if err != nil {
			return err
		}
		c.StagesJS = string(raw)
	}
	if c.Settlement != nil {
		raw, err := json.Marshal(c.Settlement)
		if err != nil {
			return err
		}
		c.SettlementJS = string(raw)
	}
	return nil
}

// AfterFind restores stages and settlement.
func (c *Contract) AfterFind(_ *gorm.DB) error {
	c.Stages = []ContractStage{}
	if c.StagesJS != "" {
		_ = json.Unmarshal([]byte(c.StagesJS), &c.Stages)
	}
	if c.SettlementJS != "" {
		var s Settlement
		if err := json.Unmarshal([]byte(c.SettlementJS), &s); err == nil {
			c.Settlement = &s
		}
	}
	return nil
}
