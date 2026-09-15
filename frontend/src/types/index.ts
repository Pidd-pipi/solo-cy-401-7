export * from './enums';

export interface User {
  id: number;
  username: string;
  email?: string;
  name?: string;
  role: string;
  avatar?: string;
  skills?: string[];
  bio?: string;
  contact?: string;
  rating?: number;
  createdAt?: string;
}

export interface Requirement {
  id: number;
  title: string;
  description: string;
  minBudget: number;
  maxBudget: number;
  deadline: string;
  skills: string[];
  status: string;
  publisherId: number;
  winnerId?: number;
  createdAt?: string;
  publisher?: User;
  bids?: Bid[];
}

export interface Bid {
  id: number;
  requirementId: number;
  bidderId: number;
  amount: number;
  durationDays: number;
  proposal: string;
  attachments: string[];
  status: string;
  createdAt?: string;
  bidder?: User;
}

export interface ContractStage {
  name: string;
  amount: number;
  status: string;
  dueAt: string;
}

export interface Settlement {
  caseId: number;
  verdict: string;
  partyARatio: number;
  partyBRatio: number;
  partyAAmount: number;
  partyBAmount: number;
  totalAmount: number;
  note?: string;
}

export interface Contract {
  id: number;
  contractNo: string;
  totalAmount: number;
  paymentType: string;
  stages: ContractStage[];
  status: string;
  activeDisputeId?: number | null;
  settlement?: Settlement | null;
  requirementId: number;
  partyAId: number;
  partyBId: number;
  createdAt?: string;
  partyA?: User;
  partyB?: User;
  requirement?: Requirement;
}

export interface DisputeMaterial {
  id: number;
  caseId: number;
  submitterId: number;
  submitterSide: string;
  kind: string;
  content: string;
  evidence: string[];
  createdAt?: string;
}

export interface DisputeCase {
  id: number;
  caseNo: string;
  contractId: number;
  openerId: number;
  openerSide: string;
  reason: string;
  claim: string;
  stageSnapshot: string;
  status: string;
  verdict?: string;
  partyARatio: number;
  partyBRatio: number;
  partyAAmount: number;
  partyBAmount: number;
  totalSettled: number;
  rulingNote?: string;
  resolverId?: number;
  resolvedAt?: string | null;
  withdrawnById?: number;
  withdrawnReason?: string;
  withdrawnAt?: string | null;
  version: number;
  createdAt?: string;
  materials?: DisputeMaterial[];
  contract?: Contract;
  opener?: User;
}

export interface PageResult<T> {
  items: T[];
  total: number;
  page: number;
  page_size: number;
}

export interface DashboardData {
  myRequirements: Requirement[];
  myBids: Bid[];
  myContracts: Contract[];
  openDisputes: DisputeCase[];
  counts: Record<string, number>;
}
