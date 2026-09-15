package service

import (
	"errors"
	"log/slog"

	"github.com/gigmatch/gigmatch/internal/constants"
	"github.com/gigmatch/gigmatch/internal/model"
	"github.com/gigmatch/gigmatch/internal/repository"
)

// DashboardData is the role-aware workbench payload.
type DashboardData struct {
	MyRequirements []model.Requirement `json:"myRequirements"`
	MyBids         []model.Bid         `json:"myBids"`
	MyContracts    []model.Contract    `json:"myContracts"`
	OpenDisputes   []model.DisputeCase `json:"openDisputes"`
	Counts         map[string]int64    `json:"counts"`
}

// DashboardService aggregates workbench data.
type DashboardService struct {
	requirements *repository.RequirementRepository
	bids         *repository.BidRepository
	contracts    *repository.ContractRepository
	disputes     *repository.DisputeRepository
	logger       *slog.Logger
}

// NewDashboardService builds a DashboardService.
func NewDashboardService(requirements *repository.RequirementRepository, bids *repository.BidRepository, contracts *repository.ContractRepository, disputes *repository.DisputeRepository, logger *slog.Logger) *DashboardService {
	return &DashboardService{requirements: requirements, bids: bids, contracts: contracts, disputes: disputes, logger: logger}
}

// Get returns the workbench payload for a user.
func (s *DashboardService) Get(userID uint) (*DashboardData, error) {
	myRequirements, err := s.requirements.ListByPublisher(userID)
	if err != nil {
		return nil, err
	}
	myBids, err := s.bids.ListByBidder(userID)
	if err != nil {
		return nil, err
	}
	myContracts, err := s.contracts.ListByParty(userID)
	if err != nil {
		return nil, err
	}
	openDisputes, err := s.openDisputesForUser(myContracts)
	if err != nil {
		return nil, err
	}
	reqCount, err := s.requirements.CountByPublisher(userID)
	if err != nil {
		return nil, err
	}
	bidCount, err := s.bids.CountByBidder(userID)
	if err != nil {
		return nil, err
	}
	contractCount, err := s.contracts.CountByParty(userID)
	if err != nil {
		return nil, err
	}
	return &DashboardData{
		MyRequirements: myRequirements,
		MyBids:         myBids,
		MyContracts:    myContracts,
		OpenDisputes:   openDisputes,
		Counts: map[string]int64{
			"requirements": reqCount,
			"bids":         bidCount,
			"contracts":    contractCount,
			"openDisputes": int64(len(openDisputes)),
		},
	}, nil
}

// openDisputesForUser collects the open cases of every contract the user is a
// party to, so both the requester and the freelancer see the same frozen case
// on their own workbench.
func (s *DashboardService) openDisputesForUser(contracts []model.Contract) ([]model.DisputeCase, error) {
	open := make([]model.DisputeCase, 0)
	for _, c := range contracts {
		if c.Status == constants.ContractCompleted || c.Status == constants.ContractTerminated {
			continue
		}
		d, err := s.disputes.FindOpenByContract(c.ID)
		if err != nil {
			if errors.Is(err, repository.ErrNotFound) {
				continue
			}
			return nil, err
		}
		open = append(open, *d)
	}
	return open, nil
}
