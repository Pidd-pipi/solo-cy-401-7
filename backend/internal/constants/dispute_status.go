package constants

// Dispute case statuses.
const (
	DisputeOpen      = "open"      // 未结：合同流程冻结
	DisputeWithdrawn = "withdrawn" // 撤回：恢复原有流程
	DisputeResolved  = "resolved"  // 已裁决：按裁决结果结算结案
)

// ValidDisputeStatus reports whether a dispute status is valid.
func ValidDisputeStatus(s string) bool {
	switch s {
	case DisputeOpen, DisputeWithdrawn, DisputeResolved:
		return true
	}
	return false
}

// Dispute material kinds.
const (
	DisputeMaterialOpen       = "open"       // 立案材料（问题说明/诉求/证据）
	DisputeMaterialSupplement = "supplement" // 双方后续补充材料
)

// Dispute party sides (relative to the contract).
const (
	DisputeSideA = "party_a" // 甲方（需求方）
	DisputeSideB = "party_b" // 乙方（自由职业者）
)

// Admin verdicts.
const (
	VerdictFullToB      = "full_to_b"     // 全额归乙方
	VerdictFullRefundA  = "full_refund_a" // 全额退回甲方
	VerdictProportional = "proportional"  // 按比例结算
)

// ValidVerdict reports whether a settlement verdict is valid.
func ValidVerdict(v string) bool {
	switch v {
	case VerdictFullToB, VerdictFullRefundA, VerdictProportional:
		return true
	}
	return false
}
