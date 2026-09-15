// 需求状态（与后端 backend/internal/constants/requirement_status.go 对齐）
export enum RequirementStatus {
  Draft = 'draft',
  Open = 'open',
  Bidding = 'bidding',
  InProgress = 'in_progress',
  PendingReview = 'pending_review',
  Completed = 'completed',
  Cancelled = 'cancelled'
}

// 报价状态（与后端 backend/internal/constants/bid_status.go 对齐）
export enum BidStatus {
  Pending = 'pending',
  Accepted = 'accepted',
  Rejected = 'rejected',
  Withdrawn = 'withdrawn'
}

// 合同状态（与后端 backend/internal/constants/contract_status.go 对齐）
export enum ContractStatus {
  PendingSignature = 'pending_signature',
  InProgress = 'in_progress',
  PendingReview = 'pending_review',
  Completed = 'completed',
  Terminated = 'terminated'
}

// 争议案件状态（与后端 backend/internal/constants/dispute_status.go 对齐）
export enum DisputeStatus {
  Open = 'open',
  Withdrawn = 'withdrawn',
  Resolved = 'resolved'
}

// 争议裁决方式（与后端 backend/internal/constants/dispute_status.go 对齐）
export enum DisputeVerdict {
  FullToB = 'full_to_b',
  FullRefundA = 'full_refund_a',
  Proportional = 'proportional'
}

// 用户角色（与后端 backend/internal/constants/roles.go 对齐）
export enum UserRole {
  Requester = 'requester',
  Freelancer = 'freelancer',
  Both = 'both',
  Admin = 'admin'
}

export const RequirementStatusLabel: Record<string, string> = {
  draft: '草稿',
  open: '待报价',
  bidding: '报价中',
  in_progress: '进行中',
  pending_review: '待验收',
  completed: '已完成',
  cancelled: '已取消'
};

export const BidStatusLabel: Record<string, string> = {
  pending: '待审',
  accepted: '已采纳',
  rejected: '已拒绝',
  withdrawn: '已撤回'
};

export const ContractStatusLabel: Record<string, string> = {
  pending_signature: '待签署',
  in_progress: '执行中',
  pending_review: '待验收',
  completed: '已完成',
  terminated: '已终止'
};

export const DisputeStatusLabel: Record<string, string> = {
  open: '争议处理中',
  withdrawn: '已撤回',
  resolved: '已裁决'
};

export const DisputeSideLabel: Record<string, string> = {
  party_a: '甲方',
  party_b: '乙方'
};

export const DisputeVerdictLabel: Record<string, string> = {
  full_to_b: '全额归乙方',
  full_refund_a: '全额退回甲方',
  proportional: '按比例结算'
};

export const RoleLabel: Record<string, string> = {
  requester: '需求方',
  freelancer: '自由职业者',
  both: '双角色',
  admin: '管理员'
};
