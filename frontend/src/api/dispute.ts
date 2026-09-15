import { getData, postData } from './request';
import type { DisputeCase } from '../types';

export interface OpenDisputePayload {
  reason: string;
  claim: string;
  evidence?: string[];
}

export interface SupplementDisputePayload {
  content: string;
  evidence?: string[];
  version?: number;
}

export interface WithdrawDisputePayload {
  reason?: string;
  version?: number;
}

export interface ResolveDisputePayload {
  verdict: 'full_to_b' | 'full_refund_a' | 'proportional';
  partyARatio?: number;
  partyBRatio?: number;
  note?: string;
  version?: number;
}

export const disputeApi = {
  open: (contractId: number, payload: OpenDisputePayload) =>
    postData<DisputeCase>(`/contracts/${contractId}/disputes`, payload),
  listByContract: (contractId: number) =>
    getData<DisputeCase[]>(`/contracts/${contractId}/disputes`),
  detail: (caseId: number) => getData<DisputeCase>(`/disputes/${caseId}`),
  listOpen: () => getData<DisputeCase[]>('/disputes'),
  supplement: (caseId: number, payload: SupplementDisputePayload) =>
    postData<DisputeCase>(`/disputes/${caseId}/supplement`, payload),
  withdraw: (caseId: number, payload: WithdrawDisputePayload = {}) =>
    postData<DisputeCase>(`/disputes/${caseId}/withdraw`, payload),
  resolve: (caseId: number, payload: ResolveDisputePayload) =>
    postData<DisputeCase>(`/disputes/${caseId}/resolve`, payload)
};
