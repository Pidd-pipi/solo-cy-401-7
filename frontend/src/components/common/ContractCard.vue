<template>
  <el-card class="contract-card" shadow="hover" @click="$router.push(`/contracts/${contract.id}`)">
    <div class="c-head">
      <b>{{ contract.contractNo }}</b>
      <div class="c-badges">
        <el-tag v-if="frozen" type="danger" size="small" effect="dark">争议中</el-tag>
        <StatusBadge :status="contract.status" kind="contract" />
      </div>
    </div>
    <p class="muted">{{ contract.requirement?.title || '相关需求' }}</p>
    <p><b>{{ formatCurrency(contract.totalAmount) }}</b>
      <span class="muted"> · {{ contract.paymentType === 'installments' ? '分阶段付款' : '一次性付款' }}</span>
    </p>
    <div class="c-parties muted">
      <span>{{ contract.partyA?.name }}（甲方）</span>
      <span>↔</span>
      <span>{{ contract.partyB?.name }}（乙方）</span>
    </div>
  </el-card>
</template>

<script setup lang="ts">
import { computed } from 'vue';
import type { Contract } from '../../types';
import StatusBadge from './StatusBadge.vue';
import { formatCurrency } from '../../utils/formatCurrency';

const props = defineProps<{ contract: Contract }>();
const frozen = computed(() => props.contract.activeDisputeId != null);
</script>

<style scoped>
.contract-card { margin-bottom: 16px; cursor: pointer; }
.c-head { display: flex; justify-content: space-between; align-items: center; margin-bottom: 8px; }
.c-badges { display: flex; gap: 6px; align-items: center; }
.c-parties { display: flex; gap: 8px; margin-top: 8px; }
</style>
