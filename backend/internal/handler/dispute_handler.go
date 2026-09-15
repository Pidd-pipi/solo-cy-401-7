package handler

import (
	"log/slog"

	"github.com/gin-gonic/gin"

	"github.com/gigmatch/gigmatch/internal/constants"
	"github.com/gigmatch/gigmatch/internal/dto"
	"github.com/gigmatch/gigmatch/internal/middleware"
	"github.com/gigmatch/gigmatch/internal/service"
	"github.com/gigmatch/gigmatch/internal/util"
)

// DisputeHandler exposes dispute case endpoints.
type DisputeHandler struct {
	svc    *service.DisputeService
	logger *slog.Logger
}

// NewDisputeHandler builds a DisputeHandler.
func NewDisputeHandler(svc *service.DisputeService, logger *slog.Logger) *DisputeHandler {
	return &DisputeHandler{svc: svc, logger: logger}
}

// Open handles POST /contracts/:id/disputes.
func (h *DisputeHandler) Open(c *gin.Context) {
	contractID, ok := parseUintParam(c, "id")
	if !ok {
		return
	}
	var req dto.OpenDisputeRequest
	if !util.BindAndValidate(c, &req) {
		return
	}
	u := middleware.GetCurrentUser(c)
	d, err := h.svc.Open(contractID, req, u.ID, u.Name, u.Role)
	if err != nil {
		util.Fail(c, err)
		return
	}
	util.OK(c, d)
}

// ListByContract handles GET /contracts/:id/disputes.
func (h *DisputeHandler) ListByContract(c *gin.Context) {
	contractID, ok := parseUintParam(c, "id")
	if !ok {
		return
	}
	u := middleware.GetCurrentUser(c)
	list, err := h.svc.ListByContractForView(contractID, u.ID, u.Role)
	if err != nil {
		util.Fail(c, err)
		return
	}
	util.OK(c, list)
}

// Get handles GET /disputes/:id.
func (h *DisputeHandler) Get(c *gin.Context) {
	id, ok := parseUintParam(c, "caseId")
	if !ok {
		return
	}
	u := middleware.GetCurrentUser(c)
	d, err := h.svc.GetForView(id, u.ID, u.Role)
	if err != nil {
		util.Fail(c, err)
		return
	}
	util.OK(c, d)
}

// Supplement handles POST /disputes/:caseId/supplement.
func (h *DisputeHandler) Supplement(c *gin.Context) {
	id, ok := parseUintParam(c, "caseId")
	if !ok {
		return
	}
	var req dto.SupplementDisputeRequest
	if !util.BindAndValidate(c, &req) {
		return
	}
	u := middleware.GetCurrentUser(c)
	d, err := h.svc.Supplement(id, req, u.ID, u.Name)
	if err != nil {
		util.Fail(c, err)
		return
	}
	util.OK(c, d)
}

// Withdraw handles POST /disputes/:caseId/withdraw.
func (h *DisputeHandler) Withdraw(c *gin.Context) {
	id, ok := parseUintParam(c, "caseId")
	if !ok {
		return
	}
	var req dto.WithdrawDisputeRequest
	if !util.BindAndValidate(c, &req) {
		return
	}
	u := middleware.GetCurrentUser(c)
	d, err := h.svc.Withdraw(id, req, u.ID, u.Name)
	if err != nil {
		util.Fail(c, err)
		return
	}
	util.OK(c, d)
}

// Resolve handles POST /disputes/:caseId/resolve (admin only).
func (h *DisputeHandler) Resolve(c *gin.Context) {
	id, ok := parseUintParam(c, "caseId")
	if !ok {
		return
	}
	u := middleware.GetCurrentUser(c)
	if u.Role != constants.RoleAdmin {
		util.Fail(c, constants.ErrForbidden)
		return
	}
	var req dto.ResolveDisputeRequest
	if !util.BindAndValidate(c, &req) {
		return
	}
	d, err := h.svc.Resolve(id, req, u.ID, u.Name, u.Role)
	if err != nil {
		util.Fail(c, err)
		return
	}
	util.OK(c, d)
}

// ListOpen handles GET /disputes (admin workbench: all open cases).
func (h *DisputeHandler) ListOpen(c *gin.Context) {
	u := middleware.GetCurrentUser(c)
	if u.Role != constants.RoleAdmin {
		util.Fail(c, constants.ErrForbidden)
		return
	}
	list, err := h.svc.ListOpen()
	if err != nil {
		util.Fail(c, err)
		return
	}
	util.OK(c, list)
}
