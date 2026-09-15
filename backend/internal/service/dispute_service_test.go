package service

import (
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/go-sql-driver/mysql"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/gigmatch/gigmatch/internal/constants"
	"github.com/gigmatch/gigmatch/internal/dto"
	"github.com/gigmatch/gigmatch/internal/model"
	"github.com/gigmatch/gigmatch/internal/repository"
)

type disputeFixture struct {
	db          *gorm.DB
	requester   *model.User
	freelancer  *model.User
	admin       *model.User
	contract    *model.Contract
	contractSvc *ContractService
	disputeSvc  *DisputeService
	reqSvc      *RequirementService
	bidSvc      *BidService
}

// memDBSeq guarantees a unique in-memory database per fixture build, so tests
// remain repeatable (including `go test -count=N`), which would otherwise reuse
// the same shared-cache DSN keyed only by test name.
var memDBSeq uint64

func newDisputeTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	n := atomic.AddUint64(&memDBSeq, 1)
	dsn := fmt.Sprintf("file:%s_%d?mode=memory&cache=shared", sanitizeName(t.Name()), n)
	return migrateDisputeDB(t, dsn, false)
}

// migrateDisputeDB opens a GORM/SQLite handle at dsn, runs the full schema
// migration, and optionally enforces real foreign-key constraints.
func migrateDisputeDB(t *testing.T, dsn string, enforceFK bool) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	sqlDB, _ := db.DB()
	sqlDB.SetMaxOpenConns(1)
	if err := db.AutoMigrate(
		&model.User{}, &model.Requirement{}, &model.Bid{},
		&model.Contract{}, &model.DisputeCase{}, &model.DisputeMaterial{},
		&model.OperationLog{},
	); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if enforceFK {
		if err := db.Exec("PRAGMA foreign_keys = ON").Error; err != nil {
			t.Fatalf("enable foreign keys: %v", err)
		}
	}
	return db
}

func setupDisputeFixture(t *testing.T, sign bool) *disputeFixture {
	t.Helper()
	db := newDisputeTestDB(t)
	return assembleDisputeFixture(t, db, sign)
}

// assembleDisputeFixture builds the services and seed entities on an existing
// (possibly FK-enforced or WAL) database handle.
func assembleDisputeFixture(t *testing.T, db *gorm.DB, sign bool) *disputeFixture {
	t.Helper()
	logger := discardLogger()

	userRepo := repository.NewUserRepository(db)
	reqRepo := repository.NewRequirementRepository(db)
	bidRepo := repository.NewBidRepository(db)
	contractRepo := repository.NewContractRepository(db)
	disputeRepo := repository.NewDisputeRepository(db)
	logRepo := repository.NewOperationLogRepository(db)

	logSvc := NewOperationLogService(logRepo, logger)
	contractSvc := NewContractService(contractRepo, logSvc, logger)
	disputeSvc := NewDisputeService(db, disputeRepo, contractRepo, logSvc, logger)
	reqSvc := NewRequirementService(reqRepo, bidRepo, logSvc, logger)
	bidSvc := NewBidService(bidRepo, reqRepo, logSvc, logger)

	requester := &model.User{Username: "dsp_req_" + t.Name(), PasswordHash: "x", Name: "需求方", Role: constants.RoleRequester}
	freelancer := &model.User{Username: "dsp_free_" + t.Name(), PasswordHash: "x", Name: "自由职业者", Role: constants.RoleFreelancer}
	admin := &model.User{Username: "dsp_admin_" + t.Name(), PasswordHash: "x", Name: "管理员", Role: constants.RoleAdmin}
	for _, u := range []*model.User{requester, freelancer, admin} {
		if err := userRepo.Create(u); err != nil {
			t.Fatal(err)
		}
	}

	req, err := reqSvc.Create(dto.CreateRequirementRequest{
		Title: "争议测试需求", Description: "用于争议案件测试的需求描述", MinBudget: 1000, MaxBudget: 5000, Skills: []string{"Go"},
	}, requester.ID, requester.Name, requester.Role)
	if err != nil {
		t.Fatalf("create requirement: %v", err)
	}
	bid, err := bidSvc.Create(dto.CreateBidRequest{
		RequirementID: req.ID, Amount: 100, DurationDays: 10, Proposal: "我来做",
	}, freelancer.ID, freelancer.Name, freelancer.Role)
	if err != nil {
		t.Fatalf("create bid: %v", err)
	}
	contract, err := reqSvc.AcceptBid(req.ID, bid.ID, requester.ID, requester.Name, "installments", contractSvc)
	if err != nil {
		t.Fatalf("accept bid: %v", err)
	}
	if sign {
		contract, err = contractSvc.Sign(contract.ID, requester.ID, requester.Name)
		if err != nil {
			t.Fatalf("sign: %v", err)
		}
	}
	return &disputeFixture{
		db: db, requester: requester, freelancer: freelancer, admin: admin,
		contract: contract, contractSvc: contractSvc, disputeSvc: disputeSvc,
		reqSvc: reqSvc, bidSvc: bidSvc,
	}
}

func openReq() dto.OpenDisputeRequest {
	return dto.OpenDisputeRequest{
		Reason:   "乙方交付的功能与合同约定不符，多个接口无法使用",
		Claim:    "要求退还已支付的首期款项",
		Evidence: []string{"https://example.com/evidence/1.png", "聊天记录截图"},
	}
}

func TestDisputeOpenFreezeAndWithdrawRestoresFlow(t *testing.T) {
	f := setupDisputeFixture(t, true)

	// Party A opens the case.
	d, err := f.disputeSvc.Open(f.contract.ID, openReq(), f.requester.ID, f.requester.Name, f.requester.Role)
	if err != nil {
		t.Fatalf("open dispute: %v", err)
	}
	if d.Status != constants.DisputeOpen || d.OpenerSide != constants.DisputeSideA {
		t.Fatalf("unexpected case: status=%s side=%s", d.Status, d.OpenerSide)
	}
	if len(d.Materials) != 1 || d.Materials[0].Kind != constants.DisputeMaterialOpen || len(d.Materials[0].Evidence) != 2 {
		t.Fatalf("opening material not recorded: %+v", d.Materials)
	}

	// Contract is frozen by this case.
	c, err := f.contractSvc.Get(f.contract.ID)
	if err != nil {
		t.Fatal(err)
	}
	if c.ActiveDisputeID == nil || *c.ActiveDisputeID != d.ID {
		t.Fatalf("contract not frozen by case %d", d.ID)
	}

	// Completion is paused while the case is open.
	if _, err := f.contractSvc.Complete(c.ID, f.requester.ID, f.requester.Name); err == nil {
		t.Fatal("complete during open dispute should be rejected")
	}

	// Duplicate submission by either party is rejected.
	if _, err := f.disputeSvc.Open(c.ID, openReq(), f.freelancer.ID, f.freelancer.Name, f.freelancer.Role); err == nil {
		t.Fatal("duplicate open dispute should be rejected")
	}

	// An outsider may not open a case.
	other := &model.User{Username: "outsider_" + t.Name(), Name: "外人", Role: constants.RoleFreelancer}
	if err := repository.NewUserRepository(f.db).Create(other); err != nil {
		t.Fatal(err)
	}
	if _, err := f.disputeSvc.Open(c.ID, openReq(), other.ID, other.Name, other.Role); !errors.Is(err, constants.ErrForbidden) {
		t.Fatalf("outsider open err = %v, want forbidden", err)
	}

	// Party B supplements material.
	supp, err := f.disputeSvc.Supplement(d.ID, dto.SupplementDisputeRequest{
		Content:  "乙方说明：功能已按约定交付，附件为验收说明",
		Evidence: []string{"交付说明.pdf"},
	}, f.freelancer.ID, f.freelancer.Name)
	if err != nil {
		t.Fatalf("supplement: %v", err)
	}
	if len(supp.Materials) != 2 {
		t.Fatalf("materials = %d, want 2", len(supp.Materials))
	}

	// Withdraw lifts the freeze without changing contract status/stages.
	wd, err := f.disputeSvc.Withdraw(d.ID, dto.WithdrawDisputeRequest{Reason: "双方已协商一致"}, f.freelancer.ID, f.freelancer.Name)
	if err != nil {
		t.Fatalf("withdraw: %v", err)
	}
	if wd.Status != constants.DisputeWithdrawn {
		t.Fatalf("case status = %s, want withdrawn", wd.Status)
	}
	c, err = f.contractSvc.Get(c.ID)
	if err != nil {
		t.Fatal(err)
	}
	if c.ActiveDisputeID != nil {
		t.Fatal("freeze not lifted after withdraw")
	}
	if c.Status != constants.ContractInProgress {
		t.Fatalf("contract status changed by withdraw: %s", c.Status)
	}

	// Original flow resumes: completion works again.
	c, err = f.contractSvc.Complete(c.ID, f.requester.ID, f.requester.Name)
	if err != nil {
		t.Fatalf("complete after withdraw: %v", err)
	}
	if c.Status != constants.ContractCompleted {
		t.Fatalf("status = %s, want completed", c.Status)
	}

	// A new case can be opened after withdrawal only while contract is active;
	// now that it's completed, opening must be rejected.
	if _, err := f.disputeSvc.Open(c.ID, openReq(), f.requester.ID, f.requester.Name, f.requester.Role); err == nil {
		t.Fatal("open dispute on completed contract should be rejected")
	}
}

func TestDisputeOpenEligibility(t *testing.T) {
	// Not yet signed: pending_signature cannot file a dispute.
	f := setupDisputeFixture(t, false)
	if _, err := f.disputeSvc.Open(f.contract.ID, openReq(), f.requester.ID, f.requester.Name, f.requester.Role); err == nil {
		t.Fatal("open dispute on pending_signature contract should be rejected")
	}
}

func TestDisputeSupplementGuards(t *testing.T) {
	f := setupDisputeFixture(t, true)
	d, err := f.disputeSvc.Open(f.contract.ID, openReq(), f.requester.ID, f.requester.Name, f.requester.Role)
	if err != nil {
		t.Fatal(err)
	}
	// Non-party cannot supplement.
	other := &model.User{Username: "outsider2_" + t.Name(), Name: "外人", Role: constants.RoleFreelancer}
	if err := repository.NewUserRepository(f.db).Create(other); err != nil {
		t.Fatal(err)
	}
	if _, err := f.disputeSvc.Supplement(d.ID, dto.SupplementDisputeRequest{Content: "无关材料"}, other.ID, other.Name); !errors.Is(err, constants.ErrForbidden) {
		t.Fatalf("outsider supplement err = %v, want forbidden", err)
	}
	// After withdrawal the case no longer accepts material.
	if _, err := f.disputeSvc.Withdraw(d.ID, dto.WithdrawDisputeRequest{}, f.requester.ID, f.requester.Name); err != nil {
		t.Fatal(err)
	}
	if _, err := f.disputeSvc.Supplement(d.ID, dto.SupplementDisputeRequest{Content: "结案后的材料"}, f.freelancer.ID, f.freelancer.Name); err == nil {
		t.Fatal("supplement after withdraw should be rejected")
	}
	// Withdrawing twice is rejected.
	if _, err := f.disputeSvc.Withdraw(d.ID, dto.WithdrawDisputeRequest{}, f.requester.ID, f.requester.Name); err == nil {
		t.Fatal("double withdraw should be rejected")
	}
}

func TestDisputeResolve(t *testing.T) {
	tests := []struct {
		name           string
		req            dto.ResolveDisputeRequest
		wantContract   string
		wantA, wantB   float64
		wantStagesDone bool
	}{
		{
			name:         "full to freelancer",
			req:          dto.ResolveDisputeRequest{Verdict: constants.VerdictFullToB, Note: "验收通过"},
			wantContract: constants.ContractCompleted,
			wantA:        0, wantB: 100, wantStagesDone: true,
		},
		{
			name:         "full refund to requester",
			req:          dto.ResolveDisputeRequest{Verdict: constants.VerdictFullRefundA, Note: "交付完全不符合"},
			wantContract: constants.ContractTerminated,
			wantA:        100, wantB: 0, wantStagesDone: false,
		},
		{
			name:         "proportional 40/60",
			req:          dto.ResolveDisputeRequest{Verdict: constants.VerdictProportional, PartyARatio: 0.4, PartyBRatio: 0.6},
			wantContract: constants.ContractCompleted,
			wantA:        40, wantB: 60, wantStagesDone: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := setupDisputeFixture(t, true)
			d, err := f.disputeSvc.Open(f.contract.ID, openReq(), f.requester.ID, f.requester.Name, f.requester.Role)
			if err != nil {
				t.Fatal(err)
			}
			resolved, err := f.disputeSvc.Resolve(d.ID, tt.req, f.admin.ID, f.admin.Name, f.admin.Role)
			if err != nil {
				t.Fatalf("resolve: %v", err)
			}
			if resolved.Status != constants.DisputeResolved || resolved.ResolverID == nil || *resolved.ResolverID != f.admin.ID {
				t.Fatalf("case not resolved correctly: %+v", resolved)
			}
			if diff := resolved.PartyAAmount + resolved.PartyBAmount - resolved.TotalSettled; diff != 0 {
				t.Fatalf("settlement not conserved: A=%v B=%v total=%v", resolved.PartyAAmount, resolved.PartyBAmount, resolved.TotalSettled)
			}
			if resolved.PartyAAmount != tt.wantA || resolved.PartyBAmount != tt.wantB {
				t.Fatalf("amounts A=%v B=%v, want %v/%v", resolved.PartyAAmount, resolved.PartyBAmount, tt.wantA, tt.wantB)
			}

			c, err := f.contractSvc.Get(f.contract.ID)
			if err != nil {
				t.Fatal(err)
			}
			if c.Status != tt.wantContract {
				t.Fatalf("contract status = %s, want %s", c.Status, tt.wantContract)
			}
			if c.ActiveDisputeID != nil {
				t.Fatal("freeze not released after ruling")
			}
			if c.Settlement == nil || c.Settlement.CaseID != d.ID {
				t.Fatalf("settlement snapshot missing: %+v", c.Settlement)
			}
			if c.Settlement.PartyAAmount+c.Settlement.PartyBAmount != c.TotalAmount {
				t.Fatal("contract settlement snapshot not conserved")
			}
			if tt.wantStagesDone {
				// A completed contract (full_to_b / proportional) must have every
				// stage closed; no in_progress/pending milestone may linger.
				for _, st := range c.Stages {
					if st.Status != "done" {
						t.Fatalf("stage %s = %s after ruling, want done", st.Name, st.Status)
					}
				}
			} else {
				// Termination (full refund) does not mark stages done: the contract
				// is closed, but it ended without accepted delivery.
				if c.Status != constants.ContractTerminated {
					t.Fatalf("unexpected non-terminal status %s with untouched stages", c.Status)
				}
			}

			// A resolved case cannot be ruled again.
			if _, err := f.disputeSvc.Resolve(d.ID, tt.req, f.admin.ID, f.admin.Name, f.admin.Role); err == nil {
				t.Fatal("double resolve should be rejected")
			}
		})
	}
}

func TestDisputeResolveConservationRejection(t *testing.T) {
	f := setupDisputeFixture(t, true)
	d, err := f.disputeSvc.Open(f.contract.ID, openReq(), f.requester.ID, f.requester.Name, f.requester.Role)
	if err != nil {
		t.Fatal(err)
	}
	// Ratios summing to 0.8 must be rejected; case stays open and contract frozen.
	if _, err := f.disputeSvc.Resolve(d.ID, dto.ResolveDisputeRequest{
		Verdict: constants.VerdictProportional, PartyARatio: 0.4, PartyBRatio: 0.4,
	}, f.admin.ID, f.admin.Name, f.admin.Role); err == nil {
		t.Fatal("non-conserving allocation should be rejected")
	}
	d2, err := f.disputeSvc.Get(d.ID)
	if err != nil {
		t.Fatal(err)
	}
	if d2.Status != constants.DisputeOpen {
		t.Fatalf("case status = %s, want still open", d2.Status)
	}
	c, err := f.contractSvc.Get(f.contract.ID)
	if err != nil {
		t.Fatal(err)
	}
	if c.ActiveDisputeID == nil {
		t.Fatal("contract freeze must remain after rejected ruling")
	}

	// A party is not allowed to rule.
	if _, err := f.disputeSvc.Resolve(d.ID, dto.ResolveDisputeRequest{Verdict: constants.VerdictFullToB}, f.requester.ID, f.requester.Name, f.requester.Role); !errors.Is(err, constants.ErrForbidden) {
		t.Fatalf("party resolve err = %v, want forbidden", err)
	}
}

func TestDisputeProportionalRoundingConserves(t *testing.T) {
	f := setupDisputeFixture(t, true)
	// Make the total awkward to split: 100.01.
	c, err := f.contractSvc.Get(f.contract.ID)
	if err != nil {
		t.Fatal(err)
	}
	c.TotalAmount = 100.01
	if err := repository.NewContractRepository(f.db).Update(c); err != nil {
		t.Fatal(err)
	}
	d, err := f.disputeSvc.Open(c.ID, openReq(), f.requester.ID, f.requester.Name, f.requester.Role)
	if err != nil {
		t.Fatal(err)
	}
	resolved, err := f.disputeSvc.Resolve(d.ID, dto.ResolveDisputeRequest{
		Verdict: constants.VerdictProportional, PartyARatio: 1.0 / 3.0, PartyBRatio: 2.0 / 3.0,
	}, f.admin.ID, f.admin.Name, f.admin.Role)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if resolved.PartyAAmount+resolved.PartyBAmount != 100.01 {
		t.Fatalf("conservation violated: A=%v B=%v", resolved.PartyAAmount, resolved.PartyBAmount)
	}
}

func TestDisputeReadAccess(t *testing.T) {
	f := setupDisputeFixture(t, true)
	d, err := f.disputeSvc.Open(f.contract.ID, openReq(), f.requester.ID, f.requester.Name, f.requester.Role)
	if err != nil {
		t.Fatal(err)
	}
	other := &model.User{Username: "outsider3_" + t.Name(), Name: "外人", Role: constants.RoleFreelancer}
	if err := repository.NewUserRepository(f.db).Create(other); err != nil {
		t.Fatal(err)
	}
	if _, err := f.disputeSvc.GetForView(d.ID, other.ID, other.Role); !errors.Is(err, constants.ErrForbidden) {
		t.Fatalf("outsider read err = %v, want forbidden", err)
	}
	if _, err := f.disputeSvc.GetForView(d.ID, f.admin.ID, f.admin.Role); err != nil {
		t.Fatalf("admin read: %v", err)
	}
	if _, err := f.disputeSvc.GetForView(d.ID, f.freelancer.ID, f.freelancer.Role); err != nil {
		t.Fatalf("party read: %v", err)
	}
}

func TestDisputeFromPendingReviewByPartyB(t *testing.T) {
	f := setupDisputeFixture(t, true)
	// Move contract into pending review.
	c, err := f.contractSvc.Get(f.contract.ID)
	if err != nil {
		t.Fatal(err)
	}
	c.Status = constants.ContractPendingReview
	if err := repository.NewContractRepository(f.db).Update(c); err != nil {
		t.Fatal(err)
	}

	// Party B (freelancer) opens the case.
	d, err := f.disputeSvc.Open(c.ID, openReq(), f.freelancer.ID, f.freelancer.Name, f.freelancer.Role)
	if err != nil {
		t.Fatalf("open from pending_review: %v", err)
	}
	if d.OpenerSide != constants.DisputeSideB || d.StageSnapshot != constants.ContractPendingReview {
		t.Fatalf("unexpected opener/snapshot: %s / %s", d.OpenerSide, d.StageSnapshot)
	}

	// Admin full-to-B ruling completes the contract.
	resolved, err := f.disputeSvc.Resolve(d.ID, dto.ResolveDisputeRequest{Verdict: constants.VerdictFullToB}, f.admin.ID, f.admin.Name, f.admin.Role)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	c, _ = f.contractSvc.Get(c.ID)
	if c.Status != constants.ContractCompleted || resolved.Status != constants.DisputeResolved {
		t.Fatalf("status mismatch contract=%s case=%s", c.Status, resolved.Status)
	}
}

func TestDisputeOperationLogsConsistent(t *testing.T) {
	f := setupDisputeFixture(t, true)
	logRepo := repository.NewOperationLogRepository(f.db)

	d, err := f.disputeSvc.Open(f.contract.ID, openReq(), f.requester.ID, f.requester.Name, f.requester.Role)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.disputeSvc.Supplement(d.ID, dto.SupplementDisputeRequest{Content: "补充说明材料"}, f.freelancer.ID, f.freelancer.Name); err != nil {
		t.Fatal(err)
	}
	if _, err := f.disputeSvc.Resolve(d.ID, dto.ResolveDisputeRequest{Verdict: constants.VerdictFullToB}, f.admin.ID, f.admin.Name, f.admin.Role); err != nil {
		t.Fatal(err)
	}

	logs, err := logRepo.List(50)
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, l := range logs {
		if l.Entity == "dispute" && l.EntityID == d.ID {
			seen[l.Action] = true
		}
	}
	for _, action := range []string{"dispute.open", "dispute.supplement", "dispute.resolve"} {
		if !seen[action] {
			t.Fatalf("operation log %s missing", action)
		}
	}

	// Failed attempts must not leave success audit entries.
	logs2, _ := logRepo.List(200)
	openCount := 0
	for _, l := range logs2 {
		if l.Action == "dispute.open" && l.EntityID == d.ID {
			openCount++
		}
	}
	if openCount != 1 {
		t.Fatalf("dispute.open recorded %d times, want 1", openCount)
	}
}

func TestDisputeWithdrawThenReopenAndResolve(t *testing.T) {
	f := setupDisputeFixture(t, true)
	d1, err := f.disputeSvc.Open(f.contract.ID, openReq(), f.requester.ID, f.requester.Name, f.requester.Role)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.disputeSvc.Withdraw(d1.ID, dto.WithdrawDisputeRequest{}, f.requester.ID, f.requester.Name); err != nil {
		t.Fatal(err)
	}
	// A second case is allowed once the first was withdrawn.
	d2, err := f.disputeSvc.Open(f.contract.ID, openReq(), f.freelancer.ID, f.freelancer.Name, f.freelancer.Role)
	if err != nil {
		t.Fatalf("reopen after withdraw: %v", err)
	}
	if d2.ID == d1.ID {
		t.Fatal("expected a new case id")
	}
	if _, err := f.disputeSvc.Resolve(d2.ID, dto.ResolveDisputeRequest{
		Verdict: constants.VerdictProportional, PartyARatio: 0.3, PartyBRatio: 0.7,
	}, f.admin.ID, f.admin.Name, f.admin.Role); err != nil {
		t.Fatalf("resolve second case: %v", err)
	}
	c, _ := f.contractSvc.Get(f.contract.ID)
	if c.ActiveDisputeID != nil {
		t.Fatal("contract still frozen after second case resolved")
	}
	if c.Settlement == nil || c.Settlement.CaseID != d2.ID || c.Settlement.PartyAAmount != 30 || c.Settlement.PartyBAmount != 70 {
		t.Fatalf("settlement snapshot wrong: %+v", c.Settlement)
	}
}

// TestDisputeProportionalClosesStages guards the reported bug: after a
// proportional ruling the contract is completed, so every stage must be closed
// by the same terminal rule as full_to_b — no in_progress/pending may remain.
func TestDisputeProportionalClosesStages(t *testing.T) {
	f := setupDisputeFixture(t, true)
	c, err := f.contractSvc.Get(f.contract.ID)
	if err != nil {
		t.Fatal(err)
	}
	// Sanity: freshly signed contract has open milestones.
	openBefore := 0
	for _, st := range c.Stages {
		if st.Status != "done" {
			openBefore++
		}
	}
	if openBefore == 0 {
		t.Fatal("fixture should start with non-done stages")
	}

	d, err := f.disputeSvc.Open(c.ID, openReq(), f.requester.ID, f.requester.Name, f.requester.Role)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.disputeSvc.Resolve(d.ID, dto.ResolveDisputeRequest{
		Verdict: constants.VerdictProportional, PartyARatio: 0.45, PartyBRatio: 0.55,
	}, f.admin.ID, f.admin.Name, f.admin.Role); err != nil {
		t.Fatalf("resolve proportional: %v", err)
	}

	c, _ = f.contractSvc.Get(c.ID)
	if c.Status != constants.ContractCompleted {
		t.Fatalf("contract = %s, want completed", c.Status)
	}
	for _, st := range c.Stages {
		if st.Status != "done" {
			t.Fatalf("stage %q = %q, want done (contract/stages inconsistent)", st.Name, st.Status)
		}
	}
}

// conflictCode extracts the business code from an AppError, or -1.
func conflictCode(t *testing.T, err error) int {
	t.Helper()
	var appErr *constants.AppError
	if errors.As(err, &appErr) {
		return appErr.Code
	}
	return -1
}

// TestDisputeSupplementRejectedAfterClose ensures material can never be
// attached to a resolved or withdrawn case: the loser of the race against a
// ruling/withdraw gets an explicit 409 and the material list stays unchanged,
// so materials, case status and contract outcome cannot contradict each other.
func TestDisputeSupplementRejectedAfterClose(t *testing.T) {
	t.Run("after resolve", func(t *testing.T) {
		f := setupDisputeFixture(t, true)
		d, err := f.disputeSvc.Open(f.contract.ID, openReq(), f.requester.ID, f.requester.Name, f.requester.Role)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.disputeSvc.Resolve(d.ID, dto.ResolveDisputeRequest{Verdict: constants.VerdictFullToB}, f.admin.ID, f.admin.Name, f.admin.Role); err != nil {
			t.Fatal(err)
		}

		_, err = f.disputeSvc.Supplement(d.ID, dto.SupplementDisputeRequest{Content: "结案后试图补充的材料"}, f.freelancer.ID, f.freelancer.Name)
		if err == nil {
			t.Fatal("supplement after resolve must be rejected")
		}
		if code := conflictCode(t, err); code != constants.CodeConflict {
			t.Fatalf("supplement after resolve code = %d, want 409", code)
		}

		closed, err := f.disputeSvc.Get(d.ID)
		if err != nil {
			t.Fatal(err)
		}
		if len(closed.Materials) != 1 {
			t.Fatalf("materials = %d after rejected supplement, want only the opening material", len(closed.Materials))
		}
		if strings.Contains(closed.Materials[0].Content, "结案后试图补充") {
			t.Fatal("rejected material content leaked into the case")
		}
	})

	t.Run("after withdraw", func(t *testing.T) {
		f := setupDisputeFixture(t, true)
		d, err := f.disputeSvc.Open(f.contract.ID, openReq(), f.requester.ID, f.requester.Name, f.requester.Role)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.disputeSvc.Withdraw(d.ID, dto.WithdrawDisputeRequest{}, f.requester.ID, f.requester.Name); err != nil {
			t.Fatal(err)
		}
		_, err = f.disputeSvc.Supplement(d.ID, dto.SupplementDisputeRequest{Content: "撤回后试图补充的材料"}, f.freelancer.ID, f.freelancer.Name)
		if err == nil {
			t.Fatal("supplement after withdraw must be rejected")
		}
		if code := conflictCode(t, err); code != constants.CodeConflict {
			t.Fatalf("supplement after withdraw code = %d, want 409", code)
		}
		closed, _ := f.disputeSvc.Get(d.ID)
		if len(closed.Materials) != 1 {
			t.Fatalf("materials = %d, want 1", len(closed.Materials))
		}
	})
}

// TestDisputeResolvedOutcomeConsistency checks the three views agree after a
// ruling: case is resolved with conservation, contract is unfrozen with the
// right status/stages, and no stale open case remains for the contract.
func TestDisputeResolvedOutcomeConsistency(t *testing.T) {
	f := setupDisputeFixture(t, true)
	d, err := f.disputeSvc.Open(f.contract.ID, openReq(), f.freelancer.ID, f.freelancer.Name, f.freelancer.Role)
	if err != nil {
		t.Fatal(err)
	}
	// One legitimate supplement before the ruling must survive it.
	if _, err := f.disputeSvc.Supplement(d.ID, dto.SupplementDisputeRequest{Content: "裁决前的有效补充材料"}, f.requester.ID, f.requester.Name); err != nil {
		t.Fatal(err)
	}
	if _, err := f.disputeSvc.Resolve(d.ID, dto.ResolveDisputeRequest{
		Verdict: constants.VerdictProportional, PartyARatio: 0.2, PartyBRatio: 0.8, Note: "按履约比例",
	}, f.admin.ID, f.admin.Name, f.admin.Role); err != nil {
		t.Fatal(err)
	}

	// Case view.
	got, err := f.disputeSvc.Get(d.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != constants.DisputeResolved || got.PartyAAmount != 20 || got.PartyBAmount != 80 || got.PartyAAmount+got.PartyBAmount != 100 {
		t.Fatalf("case view inconsistent: %+v", got)
	}
	if len(got.Materials) != 2 {
		t.Fatalf("materials = %d, want opening + one supplement", len(got.Materials))
	}

	// Contract view.
	c, err := f.contractSvc.Get(f.contract.ID)
	if err != nil {
		t.Fatal(err)
	}
	if c.ActiveDisputeID != nil || c.Status != constants.ContractCompleted {
		t.Fatalf("contract view inconsistent: freeze=%v status=%s", c.ActiveDisputeID, c.Status)
	}
	for _, st := range c.Stages {
		if st.Status != "done" {
			t.Fatalf("stage %q = %q, want done", st.Name, st.Status)
		}
	}
	if c.Settlement == nil || c.Settlement.PartyAAmount != 20 || c.Settlement.PartyBAmount != 80 {
		t.Fatalf("settlement view inconsistent: %+v", c.Settlement)
	}

	// No open case may remain on the contract (uniqueness invariant).
	open, err := repository.NewDisputeRepository(f.db).FindOpenByContract(c.ID)
	if err == nil || open != nil {
		t.Fatalf("stale open case remains: %+v", open)
	}
}

// TestDisputeLockConflictMappedTo409 verifies that a database-level race loss
// (deadlock victim, duplicate-key on the freeze slot, or lock-wait timeout) is
// surfaced to the loser as an explicit 409 business conflict rather than 500.
func TestDisputeLockConflictMappedTo409(t *testing.T) {
	db := newDisputeTestDB(t)
	logger := discardLogger()

	for _, code := range []uint16{1062, 1213, 1205} {
		err := runDisputeTx(db, logger, func(tx *gorm.DB) error {
			return &mysql.MySQLError{Number: code, Message: "synthetic race"}
		})
		var appErr *constants.AppError
		if !errors.As(err, &appErr) {
			t.Fatalf("mysql %d: err = %T %v, want AppError", code, err, err)
		}
		if appErr.Code != constants.CodeConflict {
			t.Fatalf("mysql %d: code = %d, want %d", code, appErr.Code, constants.CodeConflict)
		}
	}

	// Conditional-update conflict from the repository maps too.
	if !repository.IsLockConflict(repository.ErrConflict) {
		t.Fatal("IsLockConflict(ErrConflict) = false, want true")
	}
	// Ordinary errors are untouched.
	if repository.IsLockConflict(errors.New("some other error")) {
		t.Fatal("IsLockConflict(generic) = true, want false")
	}
	// Business AppError returned inside the tx passes through unchanged.
	orig := constants.NewAppError(constants.CodeForbidden, "no access")
	err := runDisputeTx(db, logger, func(tx *gorm.DB) error { return orig })
	var appErr *constants.AppError
	if !errors.As(err, &appErr) || appErr.Code != constants.CodeForbidden {
		t.Fatalf("business error not preserved: %v", err)
	}
}
