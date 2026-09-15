<template>
  <div class="page">
    <div class="page-title">
      <h2>我的工作台</h2>
      <span class="muted">你好，{{ userStore.user?.name }}</span>
    </div>

    <el-row :gutter="16" class="stats">
      <el-col :span="6">
        <el-card><div class="stat"><b>{{ data?.counts.requirements || 0 }}</b><span class="muted">已发布需求</span></div></el-card>
      </el-col>
      <el-col :span="6">
        <el-card><div class="stat"><b>{{ data?.counts.bids || 0 }}</b><span class="muted">已提交报价</span></div></el-card>
      </el-col>
      <el-col :span="6">
        <el-card><div class="stat"><b>{{ data?.counts.contracts || 0 }}</b><span class="muted">进行中合同</span></div></el-card>
      </el-col>
      <el-col :span="6">
        <el-card><div class="stat" :class="{ 'stat-warn': (data?.counts.openDisputes || 0) > 0 }">
          <b>{{ data?.counts.openDisputes || 0 }}</b><span class="muted">未结争议案件</span>
        </div></el-card>
      </el-col>
    </el-row>

    <el-alert
      v-if="partyOpenDisputes.length"
      class="dispute-banner"
      type="error"
      show-icon
      :closable="false"
      title="你有未结争议案件，相关合同的完成与阶段推进已暂停"
    />

    <el-tabs v-model="tab">
      <el-tab-pane label="我发布的需求" name="reqs">
        <div class="card-grid">
          <RequirementCard v-for="r in data?.myRequirements || []" :key="r.id" :requirement="r" />
        </div>
        <el-empty v-if="(data?.myRequirements || []).length === 0" description="暂无发布需求" />
      </el-tab-pane>
      <el-tab-pane label="我的报价" name="bids">
        <BidCard v-for="b in data?.myBids || []" :key="b.id" :bid="b" :can-withdraw="b.status === 'pending'" @withdraw="withdrawBid" />
        <el-empty v-if="(data?.myBids || []).length === 0" description="暂无报价" />
      </el-tab-pane>
      <el-tab-pane name="contracts">
        <template #label>
          我的合同<span v-if="partyOpenDisputes.length" class="tab-dot">{{ partyOpenDisputes.length }}</span>
        </template>
        <div class="card-grid">
          <ContractCard v-for="c in data?.myContracts || []" :key="c.id" :contract="c" />
        </div>
        <el-empty v-if="(data?.myContracts || []).length === 0" description="暂无合同" />
      </el-tab-pane>
      <el-tab-pane name="disputes">
        <template #label>
          争议案件<span v-if="partyOpenDisputes.length" class="tab-dot">{{ partyOpenDisputes.length }}</span>
        </template>
        <el-table v-if="partyOpenDisputes.length" :data="partyOpenDisputes" stripe>
          <el-table-column prop="caseNo" label="案号" width="220" />
          <el-table-column prop="reason" label="问题说明" show-overflow-tooltip />
          <el-table-column label="提交方" width="100">
            <template #default="{ row }">{{ sideLabel(row.openerSide) }}</template>
          </el-table-column>
          <el-table-column label="立案阶段" width="110">
            <template #default="{ row }">{{ ContractStatusLabel[row.stageSnapshot] || row.stageSnapshot }}</template>
          </el-table-column>
          <el-table-column label="状态" width="110">
            <template #default>
              <el-tag type="danger" size="small">争议处理中</el-tag>
            </template>
          </el-table-column>
          <el-table-column label="操作" width="120">
            <template #default="{ row }">
              <el-button link type="primary" @click="$router.push(`/contracts/${row.contractId}`)">查看/处理</el-button>
            </template>
          </el-table-column>
        </el-table>
        <el-empty v-else description="暂无未结争议案件" />
      </el-tab-pane>
      <el-tab-pane v-if="userStore.user?.role === 'admin'" name="admin-disputes">
        <template #label>
          待裁决<span v-if="adminOpenCases.length" class="tab-dot">{{ adminOpenCases.length }}</span>
        </template>
        <el-table v-if="adminOpenCases.length" :data="adminOpenCases" stripe>
          <el-table-column prop="caseNo" label="案号" width="220" />
          <el-table-column label="合同" width="160">
            <template #default="{ row }">{{ row.contract?.contractNo || ('合同 #' + row.contractId) }}</template>
          </el-table-column>
          <el-table-column prop="reason" label="问题说明" show-overflow-tooltip />
          <el-table-column prop="claim" label="诉求" show-overflow-tooltip />
          <el-table-column label="提交方" width="100">
            <template #default="{ row }">{{ sideLabel(row.openerSide) }}</template>
          </el-table-column>
          <el-table-column label="操作" width="120">
            <template #default="{ row }">
              <el-button link type="danger" @click="$router.push(`/contracts/${row.contractId}`)">前往裁决</el-button>
            </template>
          </el-table-column>
        </el-table>
        <el-empty v-else description="暂无待裁决案件" />
      </el-tab-pane>
    </el-tabs>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue';
import { ElMessage } from 'element-plus';
import { getData } from '../api/request';
import { bidApi } from '../api/bid';
import { disputeApi } from '../api/dispute';
import type { DashboardData, DisputeCase } from '../types';
import { ContractStatusLabel, DisputeSideLabel } from '../types/enums';
import { useUserStore } from '../stores/user';
import RequirementCard from '../components/common/RequirementCard.vue';
import BidCard from '../components/common/BidCard.vue';
import ContractCard from '../components/common/ContractCard.vue';

const userStore = useUserStore();
const data = ref<DashboardData | null>(null);
const adminOpenCases = ref<DisputeCase[]>([]);
const tab = ref('reqs');

const partyOpenDisputes = computed(() => data.value?.openDisputes || []);

function sideLabel(side: string): string {
  return DisputeSideLabel[side] || side;
}

async function load() {
  data.value = await getData<DashboardData>('/dashboard');
  if (userStore.user?.role === 'admin') {
    adminOpenCases.value = await disputeApi.listOpen();
  }
}

async function withdrawBid(id: number) {
  await bidApi.withdraw(id);
  ElMessage.success('已撤回');
  void load();
}

onMounted(() => void load());
</script>

<style scoped>
.stats { margin-bottom: 20px; }
.stat { display: flex; flex-direction: column; align-items: center; padding: 8px; }
.stat b { font-size: 28px; color: #243b53; }
.stat-warn b { color: #f56c6c; }
.dispute-banner { margin-bottom: 16px; }
.tab-dot {
  display: inline-block; margin-left: 4px; min-width: 18px; padding: 0 5px;
  background: #f56c6c; color: #fff; border-radius: 9px; font-size: 12px; text-align: center;
}
</style>
