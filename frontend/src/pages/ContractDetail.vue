<template>
  <div class="page">
    <el-page-header content="合同详情" @back="$router.push('/dashboard')" />
    <el-card v-if="contract" class="detail-card">
      <div class="c-head">
        <h2>{{ contract.contractNo }}</h2>
        <div class="c-badges">
          <StatusBadge :status="contract.status" kind="contract" />
          <el-tag v-if="frozen" type="danger" effect="dark">争议冻结中</el-tag>
        </div>
      </div>
      <p class="muted">需求：{{ contract.requirement?.title }}</p>
      <p><b>{{ formatCurrency(contract.totalAmount) }}</b>
        <span class="muted"> · {{ contract.paymentType === 'installments' ? '分阶段付款' : '一次性付款' }}</span>
      </p>
      <div class="c-parties">
        <span>甲方：{{ contract.partyA?.name }}</span>
        <span>乙方：{{ contract.partyB?.name }}</span>
      </div>

      <el-alert
        v-if="frozen"
        class="frozen-alert"
        type="warning"
        show-icon
        :closable="false"
        title="该合同存在未结争议案件，完成确认与阶段推进已暂停"
        description="双方可在下方案件中补充材料或撤回；由管理员作出最终裁决。"
      />

      <div class="actions">
        <el-button
          v-if="contract.status === 'pending_signature' && !frozen"
          type="primary"
          @click="sign"
        >签署确认</el-button>
        <el-button
          v-if="contract.status === 'in_progress' && isPartyA && !frozen"
          type="success"
          @click="complete"
        >确认完成</el-button>
      </div>

      <div v-if="contract.settlement" class="settlement">
        <el-divider content-position="left">裁决结算结果</el-divider>
        <el-descriptions :column="1" border size="small">
          <el-descriptions-item label="裁决方式">{{ DisputeVerdictLabel[contract.settlement.verdict] || contract.settlement.verdict }}</el-descriptions-item>
          <el-descriptions-item label="退回甲方">{{ formatCurrency(contract.settlement.partyAAmount) }}（{{ (contract.settlement.partyARatio * 100).toFixed(1) }}%）</el-descriptions-item>
          <el-descriptions-item label="支付乙方">{{ formatCurrency(contract.settlement.partyBAmount) }}（{{ (contract.settlement.partyBRatio * 100).toFixed(1) }}%）</el-descriptions-item>
          <el-descriptions-item label="合计">{{ formatCurrency(contract.settlement.partyAAmount + contract.settlement.partyBAmount) }}</el-descriptions-item>
          <el-descriptions-item v-if="contract.settlement.note" label="裁决说明">{{ contract.settlement.note }}</el-descriptions-item>
        </el-descriptions>
      </div>
    </el-card>

    <el-card v-if="contract" class="stages-card">
      <template #header><b>阶段进度</b></template>
      <ProgressSteps :stages="contract.stages" />
    </el-card>

    <DisputePanel v-if="contract" :contract="contract" @changed="load" />
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue';
import { useRoute } from 'vue-router';
import { ElMessage } from 'element-plus';
import type { Contract } from '../types';
import { DisputeVerdictLabel } from '../types/enums';
import { useContractStore } from '../stores/contract';
import { useUserStore } from '../stores/user';
import { useDisputeStore } from '../stores/dispute';
import { contractApi } from '../api/contract';
import StatusBadge from '../components/common/StatusBadge.vue';
import ProgressSteps from '../components/common/ProgressSteps.vue';
import DisputePanel from '../components/dispute/DisputePanel.vue';
import { formatCurrency } from '../utils/formatCurrency';

const route = useRoute();
const store = useContractStore();
const userStore = useUserStore();
const disputeStore = useDisputeStore();
const contract = ref<Contract | null>(null);

const isPartyA = computed(() => contract.value?.partyAId === userStore.user?.id);
const frozen = computed(() => contract.value?.activeDisputeId != null);

async function load() {
  const id = Number(route.params.id);
  contract.value = await store.fetchDetail(id);
  await disputeStore.fetchByContract(id);
}

async function sign() {
  if (!contract.value) return;
  await contractApi.sign(contract.value.id);
  ElMessage.success('签署成功');
  void load();
}

async function complete() {
  if (!contract.value) return;
  await contractApi.complete(contract.value.id);
  ElMessage.success('已确认完成');
  void load();
}

onMounted(() => void load());
</script>

<style scoped>
.detail-card { margin-bottom: 16px; }
.c-head { display: flex; justify-content: space-between; align-items: center; margin-bottom: 8px; }
.c-badges { display: flex; gap: 8px; align-items: center; }
.c-parties { display: flex; gap: 24px; }
.actions { margin-top: 16px; }
.frozen-alert { margin-top: 12px; }
.settlement { margin-top: 8px; }
.stages-card { margin-bottom: 16px; }
</style>
