package service

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/gigmatch/gigmatch/internal/constants"
	"github.com/gigmatch/gigmatch/internal/dto"
	"github.com/gigmatch/gigmatch/internal/model"
	"github.com/gigmatch/gigmatch/internal/repository"
)

// fkDBSeq makes every in-memory foreign-key database unique even when one test
// assembles several fixtures (shared-cache in-memory DBs key off the DSN).
var fkDBSeq uint64

// newFKTestDB opens an in-memory SQLite database with real foreign-key
// enforcement, so association constraints are checked by the database rather
// than merely by the application.
func newFKTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	n := atomic.AddUint64(&fkDBSeq, 1)
	dsn := fmt.Sprintf("file:fk_%s_%d?mode=memory&cache=shared&_foreign_keys=1", sanitizeName(t.Name()), n)
	return migrateDisputeDB(t, dsn, true)
}

// newWALTestDB opens a file-backed SQLite database in WAL journal mode with a
// connection pool, allowing several goroutines to contend for the single writer
// lock at the same time (real concurrency for the race tests).
func newWALTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	path := filepath.Join(t.TempDir(), "dispute-race.db")
	// Deferred transactions give each contender an MVCC snapshot read at entry;
	// the version-guarded UPDATE is the sole arbiter. busy_timeout makes the
	// single writer serialize rather than hard-fail on first contention.
	dsn := fmt.Sprintf("file:%s?_journal_mode=WAL&_busy_timeout=5000&_foreign_keys=1", path)
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("open wal sqlite: %v", err)
	}
	sqlDB, _ := db.DB()
	sqlDB.SetMaxOpenConns(8)
	sqlDB.SetMaxIdleConns(8)
	if err := db.AutoMigrate(
		&model.User{}, &model.Requirement{}, &model.Bid{},
		&model.Contract{}, &model.DisputeCase{}, &model.DisputeMaterial{},
		&model.OperationLog{},
	); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	return db
}

func sanitizeName(s string) string {
	var b []byte
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			b = append(b, byte(r))
		} else {
			b = append(b, '_')
		}
	}
	return string(b)
}

// TestDisputeForeignKeyAssociations verifies that creating an open case passes
// the real table-association constraints: contract and opener exist; and that
// no invalid relation (non-existent resolver, zero id) is written while no
// ruling exists yet.
func TestDisputeForeignKeyAssociations(t *testing.T) {
	db := newFKTestDB(t)
	f := assembleDisputeFixture(t, db, true)

	// A legitimate case passes all association checks at the database.
	d, err := f.disputeSvc.Open(f.contract.ID, openReq(), f.requester.ID, f.requester.Name, f.requester.Role)
	if err != nil {
		t.Fatalf("open with fk enforced: %v", err)
	}
	if d.ResolverID != nil {
		t.Fatalf("resolver_id = %v before any ruling, want NULL", *d.ResolverID)
	}
	if d.WithdrawnByID != nil {
		t.Fatal("withdrawn_by_id must be NULL while open")
	}

	// Raw-row confirmation that the nullable associations are actually NULL.
	var raw model.DisputeCase
	if err := db.Table("dispute_cases").Where("id = ?", d.ID).First(&raw).Error; err != nil {
		t.Fatal(err)
	}
	if raw.ResolverID != nil || raw.WithdrawnByID != nil {
		t.Fatal("open case must store NULL resolver_id / withdrawn_by_id")
	}

	// Once ruled, resolver_id points at the real admin (FK satisfied).
	resolved, err := f.disputeSvc.Resolve(d.ID, dto.ResolveDisputeRequest{Verdict: constants.VerdictFullToB}, f.admin.ID, f.admin.Name, f.admin.Role)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if resolved.ResolverID == nil || *resolved.ResolverID != f.admin.ID {
		t.Fatalf("resolver_id = %v, want real admin %d", resolved.ResolverID, f.admin.ID)
	}

	// A withdrawn case stores the real withdrawing user id.
	f2 := assembleDisputeFixture(t, newFKTestDB(t), true)
	d2, err := f2.disputeSvc.Open(f2.contract.ID, openReq(), f2.freelancer.ID, f2.freelancer.Name, f2.freelancer.Role)
	if err != nil {
		t.Fatal(err)
	}
	wd, err := f2.disputeSvc.Withdraw(d2.ID, dto.WithdrawDisputeRequest{}, f2.requester.ID, f2.requester.Name)
	if err != nil {
		t.Fatalf("withdraw: %v", err)
	}
	if wd.WithdrawnByID == nil || *wd.WithdrawnByID != f2.requester.ID {
		t.Fatalf("withdrawn_by_id = %v, want requester %d", wd.WithdrawnByID, f2.requester.ID)
	}
}

// TestDisputeInvalidAssociationsRejectedByDB proves the database rejects
// invalid relations rather than silently storing dangling references.
func TestDisputeInvalidAssociationsRejectedByDB(t *testing.T) {
	db := newFKTestDB(t)
	f := assembleDisputeFixture(t, db, true)
	disputeRepo := repository.NewDisputeRepository(db)

	// Case pointing at a non-existent contract.
	badContract := &model.DisputeCase{
		CaseNo: "DSP-BAD-CONTRACT", ContractID: 999999, OpenerID: f.requester.ID,
		OpenerSide: constants.DisputeSideA, Reason: openReq().Reason, Claim: openReq().Claim,
		Status: constants.DisputeOpen,
	}
	if err := disputeRepo.Create(badContract); !isForeignKeyError(err) {
		t.Fatalf("non-existent contract_id: err=%v, want FOREIGN KEY failure", err)
	}

	// Case pointing at a non-existent opener.
	realCase := f.openCase(t)
	badOpener := &model.DisputeCase{
		CaseNo: "DSP-BAD-OPENER", ContractID: realCase.ContractID, OpenerID: 888888,
		OpenerSide: constants.DisputeSideB, Reason: openReq().Reason, Claim: openReq().Claim,
		Status: constants.DisputeOpen,
	}
	if err := disputeRepo.Create(badOpener); !isForeignKeyError(err) {
		t.Fatalf("non-existent opener_id: err=%v, want FOREIGN KEY failure", err)
	}

	// Material pointing at a non-existent case.
	badMaterialCase := &model.DisputeMaterial{
		CaseID: 777777, SubmitterID: f.requester.ID, SubmitterSide: constants.DisputeSideA,
		Kind: constants.DisputeMaterialSupplement, Content: "孤儿材料",
	}
	if err := disputeRepo.CreateMaterial(badMaterialCase); !isForeignKeyError(err) {
		t.Fatalf("non-existent case_id: err=%v, want FOREIGN KEY failure", err)
	}

	// Material pointing at a non-existent submitter.
	badMaterialSubmitter := &model.DisputeMaterial{
		CaseID: realCase.ID, SubmitterID: 666666, SubmitterSide: constants.DisputeSideA,
		Kind: constants.DisputeMaterialSupplement, Content: "无效提交人材料",
	}
	if err := disputeRepo.CreateMaterial(badMaterialSubmitter); !isForeignKeyError(err) {
		t.Fatalf("non-existent submitter_id: err=%v, want FOREIGN KEY failure", err)
	}
}

// openCase is a small helper that files an open case for the fixture.
func (f *disputeFixture) openCase(t *testing.T) *model.DisputeCase {
	t.Helper()
	d, err := f.disputeSvc.Open(f.contract.ID, openReq(), f.requester.ID, f.requester.Name, f.requester.Role)
	if err != nil {
		t.Fatalf("open case: %v", err)
	}
	return d
}

// raceResult records what each concurrent contender observed.
type raceResult struct {
	kind string
	err  error
}

// TestDisputeConcurrentMutationExactlyOneWinner fires supplement, withdraw and
// resolve at the same time over many rounds. Exactly one must succeed; every
// loser receives an explicit conflict (409); and the resulting material count,
// case status, contract status and freeze flag must agree with the single
// winner.
func TestDisputeConcurrentMutationExactlyOneWinner(t *testing.T) {
	const rounds = 12
	for round := 0; round < rounds; round++ {
		t.Run(fmt.Sprintf("round_%d", round), func(t *testing.T) {
			t.Parallel()
			db := newWALTestDB(t)
			f := assembleDisputeFixture(t, db, true)
			d := f.openCase(t)

			// Every contender acts on the SAME case version they observed before the
			// race began — exactly how the UI works (load case -> submit action with
			// that version). The version-guarded UPDATE then allows one winner.
			startVersion := d.Version

			start := make(chan struct{})
			var wg sync.WaitGroup
			results := make([]raceResult, 3)

			// Contender 0: supplement (party B).
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-start
				v := startVersion
				_, err := f.disputeSvc.Supplement(d.ID, dto.SupplementDisputeRequest{
					Content: "并发补充材料", Evidence: []string{"e.png"}, Version: &v,
				}, f.freelancer.ID, f.freelancer.Name)
				results[0] = raceResult{kind: "supplement", err: err}
			}()

			// Contender 1: withdraw (party A).
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-start
				v := startVersion
				_, err := f.disputeSvc.Withdraw(d.ID, dto.WithdrawDisputeRequest{Reason: "并发撤回", Version: &v}, f.requester.ID, f.requester.Name)
				results[1] = raceResult{kind: "withdraw", err: err}
			}()

			// Contender 2: resolve (admin).
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-start
				v := startVersion
				_, err := f.disputeSvc.Resolve(d.ID, dto.ResolveDisputeRequest{Verdict: constants.VerdictFullToB, Version: &v}, f.admin.ID, f.admin.Name, f.admin.Role)
				results[2] = raceResult{kind: "resolve", err: err}
			}()

			close(start)
			wg.Wait()

			winners := 0
			var winnerKind string
			for _, r := range results {
				if r.err == nil {
					winners++
					winnerKind = r.kind
					continue
				}
				// Every loser must get an explicit 409 conflict, never a 5xx/nil.
				if code := conflictCode(t, r.err); code != constants.CodeConflict {
					t.Fatalf("%s lost with code %d err=%v, want 409", r.kind, code, r.err)
				}
			}
			if winners != 1 {
				t.Fatalf("winners = %d, want exactly 1 (results=%+v)", winners, results)
			}

			// Final state must agree with the unique winner.
			finalCase, err := f.disputeSvc.Get(d.ID)
			if err != nil {
				t.Fatal(err)
			}
			c, err := f.contractSvc.Get(f.contract.ID)
			if err != nil {
				t.Fatal(err)
			}
			materials := len(finalCase.Materials)

			switch winnerKind {
			case "supplement":
				if finalCase.Status != constants.DisputeOpen {
					t.Fatalf("supplement won but case=%s, want open", finalCase.Status)
				}
				if c.ActiveDisputeID == nil || *c.ActiveDisputeID != d.ID {
					t.Fatal("supplement won but freeze released")
				}
				if c.Status != constants.ContractInProgress {
					t.Fatalf("supplement won but contract=%s, want in_progress", c.Status)
				}
				if materials != 2 { // opening + one supplement
					t.Fatalf("supplement won but materials=%d, want 2", materials)
				}
			case "withdraw":
				if finalCase.Status != constants.DisputeWithdrawn {
					t.Fatalf("withdraw won but case=%s, want withdrawn", finalCase.Status)
				}
				if c.ActiveDisputeID != nil {
					t.Fatal("withdraw won but freeze remains")
				}
				if c.Status != constants.ContractInProgress {
					t.Fatalf("withdraw won but contract=%s, want in_progress unchanged", c.Status)
				}
				if materials != 1 {
					t.Fatalf("withdraw won but materials=%d, want only opening", materials)
				}
			case "resolve":
				if finalCase.Status != constants.DisputeResolved {
					t.Fatalf("resolve won but case=%s, want resolved", finalCase.Status)
				}
				if c.ActiveDisputeID != nil {
					t.Fatal("resolve won but freeze remains")
				}
				if c.Status != constants.ContractCompleted {
					t.Fatalf("resolve won but contract=%s, want completed", c.Status)
				}
				if c.Settlement == nil || c.Settlement.PartyBAmount != c.TotalAmount {
					t.Fatalf("resolve won but settlement wrong: %+v", c.Settlement)
				}
				if materials != 1 {
					t.Fatalf("resolve won but materials=%d, want only opening", materials)
				}
			}
		})
	}
}

// TestDisputeRejectedRulingPreservesFreezeAndMaterials ensures that when a
// ruling is rejected (non-conserving ratios, forbidden party, or bad verdict),
// nothing changes: the case stays open with its version untouched, the contract
// remains frozen by that case, no settlement is written, and the opening
// material is preserved.
func TestDisputeRejectedRulingPreservesFreezeAndMaterials(t *testing.T) {
	f := assembleDisputeFixture(t, newWALTestDB(t), true)
	d := f.openCase(t)

	rejected := []struct {
		name string
		run  func() error
	}{
		{
			name: "non-conserving proportional",
			run: func() error {
				_, err := f.disputeSvc.Resolve(d.ID, dto.ResolveDisputeRequest{
					Verdict: constants.VerdictProportional, PartyARatio: 0.3, PartyBRatio: 0.3,
				}, f.admin.ID, f.admin.Name, f.admin.Role)
				return err
			},
		},
		{
			name: "party is not allowed to rule",
			run: func() error {
				_, err := f.disputeSvc.Resolve(d.ID, dto.ResolveDisputeRequest{Verdict: constants.VerdictFullToB},
					f.requester.ID, f.requester.Name, f.requester.Role)
				return err
			},
		},
		{
			name: "unknown verdict",
			run: func() error {
				_, err := f.disputeSvc.Resolve(d.ID, dto.ResolveDisputeRequest{Verdict: "give_to_third_party"},
					f.admin.ID, f.admin.Name, f.admin.Role)
				return err
			},
		},
	}

	for _, tc := range rejected {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.run()
			if err == nil {
				t.Fatal("ruling should have been rejected")
			}

			c, gErr := f.contractSvc.Get(f.contract.ID)
			if gErr != nil {
				t.Fatal(gErr)
			}
			if c.ActiveDisputeID == nil || *c.ActiveDisputeID != d.ID {
				t.Fatal("freeze must remain after rejected ruling")
			}
			if c.Settlement != nil {
				t.Fatal("no settlement may be written after rejected ruling")
			}
			if c.Status != constants.ContractInProgress {
				t.Fatalf("contract status changed to %s after rejected ruling", c.Status)
			}

			open, gErr := f.disputeSvc.Get(d.ID)
			if gErr != nil {
				t.Fatal(gErr)
			}
			if open.Status != constants.DisputeOpen {
				t.Fatalf("case status = %s, want still open", open.Status)
			}
			if open.Version != 0 {
				t.Fatalf("version = %d, want 0 (failed validation must not claim a version)", open.Version)
			}
			if open.ResolverID != nil {
				t.Fatal("resolver must remain unset after rejected ruling")
			}
			if len(open.Materials) != 1 {
				t.Fatalf("opening material not preserved: materials=%d", len(open.Materials))
			}
			if open.Materials[0].Kind != constants.DisputeMaterialOpen {
				t.Fatal("the only material must still be the opening one")
			}
		})
	}

	// A valid ruling after the rejected attempts still succeeds exactly once.
	resolved, err := f.disputeSvc.Resolve(d.ID, dto.ResolveDisputeRequest{Verdict: constants.VerdictFullToB},
		f.admin.ID, f.admin.Name, f.admin.Role)
	if err != nil {
		t.Fatalf("valid ruling after rejected ones: %v", err)
	}
	if resolved.Status != constants.DisputeResolved {
		t.Fatal("case should be resolved by the eventually valid ruling")
	}
}

// TestDisputeStaleVersionConflict confirms that supplying a stale observed
// version returns 409 without changing state (client-side optimistic lock).
func TestDisputeStaleVersionConflict(t *testing.T) {
	f := setupDisputeFixture(t, true)
	d := f.openCase(t)
	stale := uint(0)

	// Advance the case via a legitimate supplement (version 0 -> 1).
	if _, err := f.disputeSvc.Supplement(d.ID, dto.SupplementDisputeRequest{Content: "第一次补充"}, f.freelancer.ID, f.freelancer.Name); err != nil {
		t.Fatal(err)
	}

	// A request still presenting version 0 must fail with 409.
	_, err := f.disputeSvc.Supplement(d.ID, dto.SupplementDisputeRequest{Content: "过期版本补充", Version: &stale}, f.freelancer.ID, f.freelancer.Name)
	var appErr *constants.AppError
	if !errors.As(err, &appErr) || appErr.Code != constants.CodeConflict {
		t.Fatalf("stale version err = %v, want 409", err)
	}

	// State unchanged apart from the one accepted supplement.
	open, _ := f.disputeSvc.Get(d.ID)
	if open.Version != 1 || len(open.Materials) != 2 {
		t.Fatalf("stale request changed state: version=%d materials=%d", open.Version, len(open.Materials))
	}
	if open.Materials[1].Content == "过期版本补充" {
		t.Fatal("stale-version material leaked into the case")
	}
}

// isForeignKeyError asserts the database rejected the write specifically due to
// a foreign-key constraint, not some unrelated failure.
func isForeignKeyError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "foreign key constraint failed")
}
