import { defineStore } from 'pinia';
import type { DisputeCase } from '../types';
import {
  disputeApi,
  type OpenDisputePayload,
  type ResolveDisputePayload,
  type SupplementDisputePayload,
  type WithdrawDisputePayload
} from '../api/dispute';

// useDisputeStore backs the contract-detail dispute panel and the admin
// workbench. Current contract cases and the admin open-case list live here.
export const useDisputeStore = defineStore('dispute', {
  state: () => ({
    cases: [] as DisputeCase[],
    openCases: [] as DisputeCase[],
    loading: false
  }),
  getters: {
    // Same contract may have many historical cases, but at most one open.
    openCase: (state) => (contractId: number) =>
      state.cases.find((d) => d.contractId === contractId && d.status === 'open'),
    latestCase: (state) => (contractId: number) =>
      state.cases.find((d) => d.contractId === contractId)
  },
  actions: {
    async fetchByContract(contractId: number) {
      this.cases = await disputeApi.listByContract(contractId);
      return this.cases;
    },
    async fetchOpen() {
      this.openCases = await disputeApi.listOpen();
      return this.openCases;
    },
    async open(contractId: number, payload: OpenDisputeViewModel) {
      const d = await disputeApi.open(contractId, payload);
      await this.fetchByContract(contractId);
      return d;
    },
    async supplement(caseId: number, payload: SupplementDisputeViewModel) {
      const d = await disputeApi.supplement(caseId, payload);
      if (d.contractId) {
        await this.fetchByContract(d.contractId);
      }
      return d;
    },
    async withdraw(caseId: number, payload: WithdrawDisputePayload = {}) {
      const d = await disputeApi.withdraw(caseId, payload);
      if (d.contractId) {
        await this.fetchByContract(d.contractId);
      }
      return d;
    },
    async resolve(caseId: number, payload: ResolveDisputePayload) {
      const d = await disputeApi.resolve(caseId, payload);
      if (d.contractId) {
        await this.fetchByContract(d.contractId);
      }
      return d;
    },
    clear() {
      this.cases = [];
      this.openCases = [];
    }
  }
});

// View-model aliases keep the store decoupled from the API payload shapes.
export type OpenDisputeViewModel = OpenDisputePayload;
export type SupplementDisputeViewModel = SupplementDisputePayload;
