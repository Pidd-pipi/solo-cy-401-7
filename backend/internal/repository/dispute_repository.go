package repository

import (
	"errors"
	"fmt"

	"gorm.io/gorm"

	"github.com/gigmatch/gigmatch/internal/model"
)

// DisputeRepository persists dispute cases and their materials.
type DisputeRepository struct {
	db *gorm.DB
}

// NewDisputeRepository builds a DisputeRepository.
func NewDisputeRepository(db *gorm.DB) *DisputeRepository {
	return &DisputeRepository{db: db}
}

// Create inserts a dispute case.
func (r *DisputeRepository) Create(d *model.DisputeCase) error {
	if err := r.db.Create(d).Error; err != nil {
		return fmt.Errorf("create dispute: %w", err)
	}
	return nil
}

// CreateMaterial inserts a piece of dispute material.
func (r *DisputeRepository) CreateMaterial(m *model.DisputeMaterial) error {
	if err := r.db.Create(m).Error; err != nil {
		return fmt.Errorf("create dispute material: %w", err)
	}
	return nil
}

// FindByID loads a case with materials and contract parties.
func (r *DisputeRepository) FindByID(id uint) (*model.DisputeCase, error) {
	var d model.DisputeCase
	err := r.db.
		Preload("Materials", func(db *gorm.DB) *gorm.DB { return db.Order("created_at ASC") }).
		Preload("Contract.PartyA").
		Preload("Contract.PartyB").
		Preload("Opener").
		Preload("Resolver").
		First(&d, id).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("find dispute by id: %w", err)
	}
	return &d, nil
}

// FindOpenByContract returns the open case of a contract, or ErrNotFound.
func (r *DisputeRepository) FindOpenByContract(contractID uint) (*model.DisputeCase, error) {
	var d model.DisputeCase
	err := r.db.
		Preload("Materials", func(db *gorm.DB) *gorm.DB { return db.Order("created_at ASC") }).
		Where("contract_id = ? AND status = ?", contractID, "open").
		First(&d).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("find open dispute: %w", err)
	}
	return &d, nil
}

// ListByContract returns all cases of a contract, newest first.
func (r *DisputeRepository) ListByContract(contractID uint) ([]model.DisputeCase, error) {
	var list []model.DisputeCase
	if err := r.db.
		Preload("Opener").
		Where("contract_id = ?", contractID).
		Order("created_at DESC").
		Find(&list).Error; err != nil {
		return nil, fmt.Errorf("list disputes by contract: %w", err)
	}
	return list, nil
}

// ListOpen returns all open cases (admin workbench).
func (r *DisputeRepository) ListOpen() ([]model.DisputeCase, error) {
	var list []model.DisputeCase
	if err := r.db.
		Preload("Contract").
		Preload("Opener").
		Where("status = ?", "open").
		Order("created_at ASC").
		Find(&list).Error; err != nil {
		return nil, fmt.Errorf("list open disputes: %w", err)
	}
	return list, nil
}

// CountOpenByContract returns the number of open cases for a contract (0 or 1).
func (r *DisputeRepository) CountOpenByContract(contractID uint) (int64, error) {
	var count int64
	if err := r.db.Model(&model.DisputeCase{}).
		Where("contract_id = ? AND status = ?", contractID, "open").
		Count(&count).Error; err != nil {
		return 0, fmt.Errorf("count open disputes: %w", err)
	}
	return count, nil
}

// Save persists case changes.
func (r *DisputeRepository) Save(d *model.DisputeCase) error {
	if err := r.db.Save(d).Error; err != nil {
		return fmt.Errorf("save dispute: %w", err)
	}
	return nil
}

// ClaimOpenVersion atomically claims the right to mutate an open case by
// advancing its version, but only while the case is still open and still at the
// version the caller observed:
//
//	UPDATE dispute_cases SET version = version + 1
//	WHERE id = ? AND status = 'open' AND version = ?
//
// It returns true exactly once among concurrent supplement/withdraw/resolve
// requests that started from the same observed version: the losing UPDATEs
// match zero rows. This makes "at most one concurrent mutation succeeds" hold
// deterministically on any database, independent of row-lock semantics.
func (r *DisputeRepository) ClaimOpenVersion(id uint, observedVersion uint) (bool, error) {
	res := r.db.Model(&model.DisputeCase{}).
		Where("id = ? AND status = ? AND version = ?", id, "open", observedVersion).
		UpdateColumn("version", gorm.Expr("version + 1"))
	if res.Error != nil {
		return false, fmt.Errorf("claim dispute version: %w", res.Error)
	}
	return res.RowsAffected == 1, nil
}
