package service

import (
	"errors"
	"fmt"
	"log/slog"
	"math"
	"time"

	"gorm.io/gorm"

	"github.com/gigmatch/gigmatch/internal/constants"
	"github.com/gigmatch/gigmatch/internal/dto"
	"github.com/gigmatch/gigmatch/internal/model"
	"github.com/gigmatch/gigmatch/internal/repository"
)

// mapUnfreezeConflict converts a lost conditional unfreeze (the freeze slot no
// longer points at this case) into an explicit 409 so the loser of a concurrent
// dispute mutation gets a clear conflict instead of a generic 500.
func mapUnfreezeConflict(err error) error {
	if errors.Is(err, repository.ErrConflict) {
		return conflictRetrying()
	}
	return fmt.Errorf("unfreeze contract: %w", err)
}

// runDisputeTx executes a dispute-mutation transaction and converts any
// database-level race (deadlock victim, lock timeout, duplicate key) into an
// explicit 409, so the loser of concurrent open/supplement/withdraw/resolve
// always receives a clear conflict and may retry after refreshing.
func runDisputeTx(db *gorm.DB, logger *slog.Logger, fn func(tx *gorm.DB) error) error {
	err := db.Transaction(fn)
	if err == nil {
		return nil
	}
	var appErr *constants.AppError
	if errors.As(err, &appErr) {
		return err // already a business-level response (400/403/404/409)
	}
	if repository.IsLockConflict(err) {
		logger.Warn("dispute concurrent mutation conflict", "error", err)
		return conflictRetrying()
	}
	return err
}

func conflictRetrying() error {
	return constants.NewAppError(constants.CodeConflict, "案件状态已被并发操作改变，请刷新后重试")
}

// settlementEpsilon tolerates floating drift when checking ratio conservation.
const settlementEpsilon = 1e-6

// auditEntry defers the operation-log write until after the surrounding
// database transaction has committed, so auditing never takes part in its locks
// (important under single-writer databases such as MySQL/SQLite).
type auditEntry struct {
	userID   uint
	userName string
	action   string
	entity   string
	entityID uint
	detail   string
}

func (a auditEntry) record(logs *OperationLogService) {
	if a.action == "" {
		return
	}
	logs.Record(a.userID, a.userName, a.action, a.entity, a.entityID, a.detail)
}

// DisputeService handles dispute cases: open / supplement / withdraw / resolve.
//
// Freeze model: opening a case sets contracts.active_dispute_id (a nullable
// unique column), which blocks contract completion and stage advancement.
//
// Concurrency model: every mutation runs in a transaction. Concurrent
// supplement/withdraw/resolve acting on the same case are arbitrated
// optimistically by a version-guarded UPDATE
// (WHERE status='open' AND version=?), so exactly one can advance the version;
// the losers match zero rows and get a 409. Two concurrent opens are arbitrated
// by the conditional Freeze UPDATE plus the unique index. Database-level races
// (deadlock 1213 / lock-timeout 1205 / duplicate 1062) are mapped to 409 too.
type DisputeService struct {
	db        *gorm.DB
	disputes  *repository.DisputeRepository
	contracts *repository.ContractRepository
	logs      *OperationLogService
	logger    *slog.Logger
}

// NewDisputeService builds a DisputeService.
func NewDisputeService(db *gorm.DB, disputes *repository.DisputeRepository, contracts *repository.ContractRepository, logs *OperationLogService, logger *slog.Logger) *DisputeService {
	return &DisputeService{db: db, disputes: disputes, contracts: contracts, logs: logs, logger: logger}
}

// Get loads a case; the caller must be a party or an admin (checked by handler).
func (s *DisputeService) Get(id uint) (*model.DisputeCase, error) {
	return s.disputes.FindByID(id)
}

// ListByContract returns the case history of a contract.
func (s *DisputeService) ListByContract(contractID uint) ([]model.DisputeCase, error) {
	return s.disputes.ListByContract(contractID)
}

// ListOpen returns every open case for the admin workbench.
func (s *DisputeService) ListOpen() ([]model.DisputeCase, error) {
	return s.disputes.ListOpen()
}

// Open files a new dispute case. Only the contract parties may file, only while
// the contract is in progress or pending review, and only when no open case exists.
func (s *DisputeService) Open(contractID uint, req dto.OpenDisputeRequest, userID uint, userName, role string) (*model.DisputeCase, error) {
	var result *model.DisputeCase
	var audit auditEntry
	err := runDisputeTx(s.db, s.logger, func(tx *gorm.DB) error {
		txContracts := repository.NewContractRepository(tx)
		txDisputes := repository.NewDisputeRepository(tx)

		// Snapshot read of the contract. Open creates a brand-new case, so it does
		// not claim a case version; mutual exclusion of two concurrent opens is
		// decided solely by the conditional Freeze UPDATE (and the unique index).
		c, err := txContracts.FindByID(contractID)
		if err != nil {
			return err
		}
		// Authorize before revealing whether a case already exists, so an outsider
		// gets 403 rather than a "duplicate" 409.
		side, err := partySide(c, userID)
		if err != nil {
			return err
		}
		if c.Status != constants.ContractInProgress && c.Status != constants.ContractPendingReview {
			return constants.NewAppError(constants.CodeConflict, "仅执行中或待验收的合同可以提交争议")
		}
		if c.ActiveDisputeID != nil {
			return constants.NewAppError(constants.CodeConflict, "该合同已有未结争议案件，请勿重复提交")
		}
		if open, _ := txDisputes.CountOpenByContract(contractID); open > 0 {
			return constants.NewAppError(constants.CodeConflict, "该合同已有未结争议案件，请勿重复提交")
		}

		evidence := req.Evidence
		if evidence == nil {
			evidence = []string{}
		}
		d := &model.DisputeCase{
			CaseNo:        fmt.Sprintf("DSP-%d-%d", contractID, time.Now().UnixNano()),
			ContractID:    contractID,
			OpenerID:      userID,
			OpenerSide:    side,
			Reason:        req.Reason,
			Claim:         req.Claim,
			StageSnapshot: c.Status,
			Status:        constants.DisputeOpen,
		}
		if err := txDisputes.Create(d); err != nil {
			return fmt.Errorf("create dispute: %w", err)
		}
		opening := &model.DisputeMaterial{
			CaseID:        d.ID,
			SubmitterID:   userID,
			SubmitterSide: side,
			Kind:          constants.DisputeMaterialOpen,
			Content:       "问题说明：" + req.Reason + "\n诉求：" + req.Claim,
			Evidence:      evidence,
		}
		if err := txDisputes.CreateMaterial(opening); err != nil {
			return fmt.Errorf("create opening material: %w", err)
		}

		// Atomic claim of the freeze slot: rejects concurrent double-open.
		frozen, err := txContracts.Freeze(contractID, d.ID)
		if err != nil {
			return fmt.Errorf("freeze contract: %w", err)
		}
		if !frozen {
			return constants.NewAppError(constants.CodeConflict, "该合同已有未结争议案件，请勿重复提交")
		}

		result = d
		audit = auditEntry{
			userID: userID, userName: userName, action: "dispute.open", entity: "dispute", entityID: d.ID,
			detail: fmt.Sprintf("提交争议案件 %s（合同 %s，阶段 %s）", d.CaseNo, c.ContractNo, c.Status),
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	audit.record(s.logs)
	return s.disputes.FindByID(result.ID)
}

// Supplement appends material from either party to an open case. It runs in a
// transaction and wins mutual exclusion with a concurrent ruling/withdraw via
// the case version claim: exactly one of supplement/withdraw/resolve acting on
// the same observed version can advance it. Once the case is no longer open,
// material cannot be written.
func (s *DisputeService) Supplement(caseID uint, req dto.SupplementDisputeRequest, userID uint, userName string) (*model.DisputeCase, error) {
	var result *model.DisputeCase
	var audit auditEntry
	err := runDisputeTx(s.db, s.logger, func(tx *gorm.DB) error {
		txContracts := repository.NewContractRepository(tx)
		txDisputes := repository.NewDisputeRepository(tx)

		d, err := txDisputes.FindByID(caseID)
		if err != nil {
			return err
		}
		c, err := txContracts.FindByID(d.ContractID)
		if err != nil {
			return err
		}
		side, err := partySide(c, userID)
		if err != nil {
			return err
		}
		if d.Status != constants.DisputeOpen {
			return constants.NewAppError(constants.CodeConflict, "案件已结案，不能再补充材料")
		}
		// Defensive: an open case must still own the contract freeze slot.
		if c.ActiveDisputeID == nil || *c.ActiveDisputeID != d.ID {
			return constants.NewAppError(constants.CodeConflict, "案件与合同冻结状态不一致，补充材料已拒绝")
		}
		// Deterministic mutual exclusion: only one concurrent mutation can advance
		// the open case version; the others match zero rows and get 409.
		if err := claimOpenCase(txDisputes, d, req.Version); err != nil {
			return err
		}

		evidence := req.Evidence
		if evidence == nil {
			evidence = []string{}
		}
		m := &model.DisputeMaterial{
			CaseID:        d.ID,
			SubmitterID:   userID,
			SubmitterSide: side,
			Kind:          constants.DisputeMaterialSupplement,
			Content:       req.Content,
			Evidence:      evidence,
		}
		if err := txDisputes.CreateMaterial(m); err != nil {
			return fmt.Errorf("add material: %w", err)
		}

		result = d
		audit = auditEntry{
			userID: userID, userName: userName, action: "dispute.supplement", entity: "dispute", entityID: d.ID,
			detail: "补充争议材料",
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	audit.record(s.logs)
	return s.disputes.FindByID(result.ID)
}

// Withdraw lifts the freeze and restores the original contract flow. Either
// party may withdraw; the contract keeps its status and stages untouched.
func (s *DisputeService) Withdraw(caseID uint, req dto.WithdrawDisputeRequest, userID uint, userName string) (*model.DisputeCase, error) {
	var result *model.DisputeCase
	var audit auditEntry
	err := runDisputeTx(s.db, s.logger, func(tx *gorm.DB) error {
		txContracts := repository.NewContractRepository(tx)
		txDisputes := repository.NewDisputeRepository(tx)

		// Snapshot read: the version-guarded claim below is the arbiter between
		// this withdraw and a concurrent supplement/ruling.
		d, err := txDisputes.FindByID(caseID)
		if err != nil {
			return err
		}
		c, err := txContracts.FindByID(d.ContractID)
		if err != nil {
			return err
		}
		if _, err := partySide(c, userID); err != nil {
			return err
		}
		if d.Status != constants.DisputeOpen {
			return constants.NewAppError(constants.CodeConflict, "仅未结案件可以撤回")
		}
		if c.ActiveDisputeID == nil || *c.ActiveDisputeID != d.ID {
			return constants.NewAppError(constants.CodeConflict, "案件与合同冻结状态不一致，撤回已拒绝")
		}
		if err := claimOpenCase(txDisputes, d, req.Version); err != nil {
			return err
		}

		// Conditional unfreeze: only clear the slot while it still points at this case.
		if err := txContracts.Unfreeze(d.ContractID, d.ID); err != nil {
			return mapUnfreezeConflict(err)
		}

		now := time.Now()
		withdrawnBy := userID
		d.Status = constants.DisputeWithdrawn
		d.WithdrawnByID = &withdrawnBy
		d.WithdrawnReason = req.Reason
		d.WithdrawnAt = &now
		if err := txDisputes.Save(d); err != nil {
			return fmt.Errorf("withdraw dispute: %w", err)
		}

		result = d
		audit = auditEntry{
			userID: userID, userName: userName, action: "dispute.withdraw", entity: "dispute", entityID: d.ID,
			detail: fmt.Sprintf("撤回争议案件 %s，合同恢复原流程", d.CaseNo),
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	audit.record(s.logs)
	return s.disputes.FindByID(result.ID)
}

// Resolve issues the admin ruling and settles the contract. Money is conserved:
// the cents allocated to both parties always sum exactly to the contract total.
func (s *DisputeService) Resolve(caseID uint, req dto.ResolveDisputeRequest, adminID uint, adminName, role string) (*model.DisputeCase, error) {
	if role != constants.RoleAdmin {
		return nil, constants.ErrForbidden
	}
	if !constants.ValidVerdict(req.Verdict) {
		return nil, constants.NewAppError(constants.CodeBadRequest, "无效的裁决方式")
	}

	var result *model.DisputeCase
	var audit auditEntry
	err := runDisputeTx(s.db, s.logger, func(tx *gorm.DB) error {
		txContracts := repository.NewContractRepository(tx)
		txDisputes := repository.NewDisputeRepository(tx)

		// Snapshot read: the version-guarded claim after validation arbitrates
		// between this ruling and any concurrent supplement/withdraw.
		d, err := txDisputes.FindByID(caseID)
		if err != nil {
			return err
		}
		c, err := txContracts.FindByID(d.ContractID)
		if err != nil {
			return err
		}
		if d.Status != constants.DisputeOpen {
			return constants.NewAppError(constants.CodeConflict, "案件已结案，不能重复裁决")
		}
		if c.ActiveDisputeID == nil || *c.ActiveDisputeID != d.ID {
			// Invariant guard: contract must be frozen by this very case.
			return constants.NewAppError(constants.CodeConflict, "案件与合同冻结状态不一致，无法裁决")
		}

		ratioA, ratioB, err := settlementRatios(req)
		if err != nil {
			return err
		}
		amountA, amountB, err := allocate(c.TotalAmount, ratioA, ratioB)
		if err != nil {
			return err
		}

		// Claim only after the ruling validates. A rejected ruling (bad verdict or
		// non-conserving ratios) returns above WITHOUT advancing the version or
		// touching the freeze/materials, so they remain intact for a retry.
		if err := claimOpenCase(txDisputes, d, req.Version); err != nil {
			return err
		}

		newStatus := constants.ContractCompleted
		if req.Verdict == constants.VerdictFullRefundA {
			// Full refund means the freelancer did not earn the payment: terminate.
			newStatus = constants.ContractTerminated
		}
		if newStatus == constants.ContractCompleted {
			// Same terminal rule as full_to_b: a completed contract cannot leave
			// any stage in progress/pending, otherwise contract and stages disagree.
			finalizeStages(c.Stages)
		}

		// Release the freeze before saving the settled contract.
		if err := txContracts.Unfreeze(c.ID, d.ID); err != nil {
			return mapUnfreezeConflict(err)
		}
		settlement := &model.Settlement{
			CaseID:       d.ID,
			Verdict:      req.Verdict,
			PartyARatio:  ratioA,
			PartyBRatio:  ratioB,
			PartyAAmount: amountA,
			PartyBAmount: amountB,
			TotalAmount:  c.TotalAmount,
			Note:         req.Note,
		}
		c.ActiveDisputeID = nil
		c.Status = newStatus
		c.Settlement = settlement
		if err := txContracts.Update(c); err != nil {
			return fmt.Errorf("settle contract: %w", err)
		}

		now := time.Now()
		resolverID := adminID
		d.Status = constants.DisputeResolved
		d.Verdict = req.Verdict
		d.PartyARatio = ratioA
		d.PartyBRatio = ratioB
		d.PartyAAmount = amountA
		d.PartyBAmount = amountB
		d.TotalSettled = amountA + amountB
		d.RulingNote = req.Note
		d.ResolverID = &resolverID
		d.ResolvedAt = &now
		if err := txDisputes.Save(d); err != nil {
			return fmt.Errorf("resolve dispute: %w", err)
		}

		result = d
		audit = auditEntry{
			userID: adminID, userName: adminName, action: "dispute.resolve", entity: "dispute", entityID: d.ID,
			detail: fmt.Sprintf("裁决案件 %s：%s；甲方 %.2f / 乙方 %.2f，合同 -> %s",
				d.CaseNo, verdictLabel(req.Verdict), amountA, amountB, newStatus),
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	audit.record(s.logs)
	return s.disputes.FindByID(result.ID)
}

// EnsureNotFrozen guards contract mutations: an active dispute pauses completion
// and any later stage advancement.
func ensureNotFrozen(c *model.Contract) error {
	if c.ActiveDisputeID != nil {
		return constants.NewAppError(constants.CodeConflict, "争议处理中，合同完成与阶段推进已暂停")
	}
	return nil
}

// finalizeStages moves every milestone to its terminal "done" state. It is the
// single terminal rule shared by normal completion and any ruling that closes a
// contract as completed, so a completed contract never keeps in-progress or
// pending stages.
func finalizeStages(stages []model.ContractStage) {
	for i := range stages {
		stages[i].Status = "done"
	}
}

// claimOpenCase performs the version-guarded claim that gives deterministic
// mutual exclusion to supplement/withdraw/resolve: the single UPDATE that
// advances version while status=open wins; everyone else matches zero rows and
// receives 409. If the client supplied a version it must match the one read.
func claimOpenCase(r *repository.DisputeRepository, d *model.DisputeCase, clientVersion *uint) error {
	observed := d.Version
	if clientVersion != nil && *clientVersion != observed {
		return conflictRetrying()
	}
	claimed, err := r.ClaimOpenVersion(d.ID, observed)
	if err != nil {
		return fmt.Errorf("claim dispute version: %w", err)
	}
	if !claimed {
		return conflictRetrying()
	}
	d.Version = observed + 1
	return nil
}

// partySide returns the caller's side of the contract or ErrForbidden.
func partySide(c *model.Contract, userID uint) (string, error) {
	switch userID {
	case c.PartyAID:
		return constants.DisputeSideA, nil
	case c.PartyBID:
		return constants.DisputeSideB, nil
	default:
		return "", constants.ErrForbidden
	}
}

// settlementRatios validates the verdict and returns the two ratios.
func settlementRatios(req dto.ResolveDisputeRequest) (float64, float64, error) {
	switch req.Verdict {
	case constants.VerdictFullToB:
		return 0, 1, nil
	case constants.VerdictFullRefundA:
		return 1, 0, nil
	case constants.VerdictProportional:
		if math.IsNaN(req.PartyARatio) || math.IsNaN(req.PartyBRatio) ||
			req.PartyARatio < 0 || req.PartyARatio > 1 || req.PartyBRatio < 0 || req.PartyBRatio > 1 {
			return 0, 0, constants.NewAppError(constants.CodeBadRequest, "分成比例必须在 0 到 1 之间")
		}
		if math.Abs(req.PartyARatio+req.PartyBRatio-1) > settlementEpsilon {
			return 0, 0, constants.NewAppError(constants.CodeBadRequest, "余额分配不守恒：甲乙双方比例之和必须等于 1")
		}
		return req.PartyARatio, req.PartyBRatio, nil
	default:
		return 0, 0, constants.NewAppError(constants.CodeBadRequest, "无效的裁决方式")
	}
}

// allocate splits the total into cents-exact amounts; party B receives the
// remainder so the two amounts always sum back to the total.
func allocate(total, ratioA, ratioB float64) (float64, float64, error) {
	if total < 0 {
		return 0, 0, constants.NewAppError(constants.CodeBadRequest, "合同金额异常，无法结算")
	}
	totalCents := int64(math.Round(total * 100))
	aCents := int64(math.Round(float64(totalCents) * ratioA))
	if aCents < 0 {
		aCents = 0
	}
	if aCents > totalCents {
		aCents = totalCents
	}
	bCents := totalCents - aCents // conservation by construction
	if bCents < 0 || aCents+bCents != totalCents {
		return 0, 0, constants.NewAppError(constants.CodeBadRequest, "余额分配不守恒，裁决已拒绝")
	}
	return float64(aCents) / 100, float64(bCents) / 100, nil
}

func verdictLabel(v string) string {
	switch v {
	case constants.VerdictFullToB:
		return "全额归乙方"
	case constants.VerdictFullRefundA:
		return "全额退回甲方"
	case constants.VerdictProportional:
		return "按比例结算"
	default:
		return v
	}
}

// GetForView loads a case and enforces read access: contract party or admin.
func (s *DisputeService) GetForView(id uint, userID uint, role string) (*model.DisputeCase, error) {
	d, err := s.disputes.FindByID(id)
	if err != nil {
		return nil, err
	}
	if d.Contract == nil {
		c, err := s.contracts.FindByID(d.ContractID)
		if err != nil {
			return nil, err
		}
		d.Contract = c
	}
	if !canViewCase(d.Contract, userID, role) {
		return nil, constants.ErrForbidden
	}
	return d, nil
}

// ListByContractForView returns case history visible to the caller.
func (s *DisputeService) ListByContractForView(contractID uint, userID uint, role string) ([]model.DisputeCase, error) {
	c, err := s.contracts.FindByID(contractID)
	if err != nil {
		return nil, err
	}
	if !canViewCase(c, userID, role) {
		return nil, constants.ErrForbidden
	}
	return s.disputes.ListByContract(contractID)
}

func canViewCase(c *model.Contract, userID uint, role string) bool {
	if role == constants.RoleAdmin {
		return true
	}
	return userID == c.PartyAID || userID == c.PartyBID
}
