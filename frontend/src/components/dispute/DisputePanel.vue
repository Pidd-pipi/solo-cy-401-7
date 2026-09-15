<template>
  <el-card class="dispute-card">
    <template #header>
      <div class="d-head">
        <b>争议案件</b>
        <el-tag v-if="openCase" type="danger" size="small">争议处理中 · 流程已暂停</el-tag>
        <el-tag v-else-if="history.length" type="info" size="small">历史案件 {{ history.length }} 件</el-tag>
      </div>
    </template>

    <el-alert
      v-if="openCase"
      class="freeze-tip"
      type="warning"
      :closable="false"
      show-icon
      title="争议处理中：合同完成确认与后续阶段推进已暂停，案件结案后恢复"
    />

    <!-- 进行中的案件 -->
    <div v-if="openCase" class="case-open">
      <div class="case-meta">
        <span>案号：{{ openCase.caseNo }}</span>
        <span>提交方：{{ sideLabel(openCase.openerSide) }}（{{ openCase.opener?.name || '-' }}）</span>
        <span>立案阶段：{{ ContractStatusLabel[openCase.stageSnapshot] || openCase.stageSnapshot }}</span>
      </div>
      <el-descriptions :column="1" border size="small" class="case-desc">
        <el-descriptions-item label="问题说明">{{ openCase.reason }}</el-descriptions-item>
        <el-descriptions-item label="诉求">{{ openCase.claim }}</el-descriptions-item>
      </el-descriptions>

      <div class="materials">
        <div v-for="m in openCase.materials || []" :key="m.id" class="material">
          <div class="m-head">
            <el-tag size="small" :type="m.kind === 'open' ? 'danger' : 'primary'">
              {{ m.kind === 'open' ? '立案材料' : '补充材料' }}
            </el-tag>
            <span class="muted">{{ sideLabel(m.submitterSide) }} · {{ formatDate(m.createdAt) }}</span>
          </div>
          <p class="m-content">{{ m.content }}</p>
          <div v-if="(m.evidence || []).length" class="m-evidence">
            <el-link v-for="(e, i) in m.evidence" :key="i" :href="normalizeEvidence(e)" target="_blank" type="primary" class="evi-link">
              📎 {{ e }}
            </el-link>
          </div>
        </div>
      </div>

      <div class="case-actions">
        <el-button v-if="isParty" type="primary" plain @click="openSupplement">补充材料</el-button>
        <el-button v-if="isParty" type="warning" plain @click="openWithdraw">撤回案件</el-button>
        <el-button v-if="isAdmin" type="danger" @click="openResolve">管理员裁决</el-button>
      </div>
    </div>

    <!-- 无未结案件：当事方可提交 -->
    <div v-else-if="canFile" class="case-empty">
      <p class="muted">交付或款项存在争议？提交后合同流程将暂停，进入协商与裁决。</p>
      <el-button type="danger" plain @click="openFileDialog">提交争议</el-button>
    </div>

    <!-- 历史案件 -->
    <el-collapse v-if="history.length" class="history">
      <el-collapse-item v-for="d in history" :key="d.id" :name="d.id">
        <template #title>
          <span class="hist-title">
            {{ d.caseNo }}
            <el-tag size="small" :type="d.status === 'resolved' ? 'success' : 'info'">
              {{ DisputeStatusLabel[d.status] }}
            </el-tag>
            <span class="muted">{{ formatDate(d.createdAt) }}</span>
          </span>
        </template>
        <p><b>问题说明：</b>{{ d.reason }}</p>
        <p><b>诉求：</b>{{ d.claim }}</p>
        <template v-if="d.status === 'resolved'">
          <p><b>裁决：</b>{{ DisputeVerdictLabel[d.verdict || ''] || d.verdict }}（裁决人 ID：{{ d.resolverId }}）</p>
          <div class="alloc">
            <el-tag type="warning">退回甲方：{{ formatCurrency(d.partyAAmount) }}（{{ (d.partyARatio * 100).toFixed(1) }}%）</el-tag>
            <el-tag type="success">支付乙方：{{ formatCurrency(d.partyBAmount) }}（{{ (d.partyBRatio * 100).toFixed(1) }}%）</el-tag>
            <el-tag type="info">合计：{{ formatCurrency(d.totalSettled) }}</el-tag>
          </div>
          <p v-if="d.rulingNote" class="muted">裁决说明：{{ d.rulingNote }}</p>
        </template>
        <p v-else class="muted">撤回人 ID：{{ d.withdrawnById }}<template v-if="d.withdrawnReason">；原因：{{ d.withdrawnReason }}</template></p>
      </el-collapse-item>
    </el-collapse>

    <!-- 提交争议 -->
    <el-dialog v-model="fileDialog" title="提交争议案件" width="560px">
      <el-form :model="fileForm" label-width="88px">
        <el-form-item label="问题说明" required>
          <el-input v-model="fileForm.reason" type="textarea" :rows="3" maxlength="500" show-word-limit
            placeholder="请描述交付或款项争议的具体情况（至少 5 个字）" />
        </el-form-item>
        <el-form-item label="诉求" required>
          <el-input v-model="fileForm.claim" type="textarea" :rows="2" maxlength="500" show-word-limit
            placeholder="例如：要求退还首付款 / 要求乙方限期修复" />
        </el-form-item>
        <el-form-item label="证据">
          <el-select v-model="fileForm.evidence" multiple filterable allow-create default-first-option
            :reserve-keyword="false" placeholder="输入链接或说明后回车，可添加多条">
          </el-select>
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="fileDialog = false">取消</el-button>
        <el-button type="danger" :loading="submitting" @click="submitFile">确认提交</el-button>
      </template>
    </el-dialog>

    <!-- 补充材料 -->
    <el-dialog v-model="supplementDialog" title="补充材料" width="560px">
      <el-form label-width="88px">
        <el-form-item label="补充说明" required>
          <el-input v-model="supplementForm.content" type="textarea" :rows="4" maxlength="1000" show-word-limit
            placeholder="请输入补充说明（至少 2 个字）" />
        </el-form-item>
        <el-form-item label="证据">
          <el-select v-model="supplementForm.evidence" multiple filterable allow-create default-first-option
            :reserve-keyword="false" placeholder="输入链接或说明后回车">
          </el-select>
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="supplementDialog = false">取消</el-button>
        <el-button type="primary" :loading="submitting" @click="submitSupplement">提交</el-button>
      </template>
    </el-dialog>

    <!-- 撤回 -->
    <el-dialog v-model="withdrawDialog" title="撤回争议案件" width="480px">
      <el-alert type="info" :closable="false" show-icon
        title="撤回后合同解冻，签署、完成确认与阶段推进恢复原有流程。" />
      <el-input v-model="withdrawReason" type="textarea" :rows="2" maxlength="255" show-word-limit
        placeholder="撤回原因（可选，如：双方已协商一致）" style="margin-top: 12px" />
      <template #footer>
        <el-button @click="withdrawDialog = false">取消</el-button>
        <el-button type="warning" :loading="submitting" @click="submitWithdraw">确认撤回</el-button>
      </template>
    </el-dialog>

    <!-- 管理员裁决 -->
    <el-dialog v-model="resolveDialog" title="管理员裁决" width="560px">
      <el-form label-width="110px">
        <el-form-item label="裁决方式" required>
          <el-radio-group v-model="resolveForm.verdict">
            <el-radio value="full_to_b">全额归乙方</el-radio>
            <el-radio value="full_refund_a">全额退回甲方</el-radio>
            <el-radio value="proportional">按比例结算</el-radio>
          </el-radio-group>
        </el-form-item>
        <template v-if="resolveForm.verdict === 'proportional'">
          <el-form-item label="退回甲方比例">
            <el-input-number v-model="resolveForm.partyAPercent" :min="0" :max="100" :step="1" /> %
          </el-form-item>
          <el-form-item label="支付乙方比例">
            <el-input-number v-model="resolveForm.partyBPercent" :min="0" :max="100" :step="1" /> %
          </el-form-item>
          <el-form-item>
            <el-tag :type="ratioSum === 100 ? 'success' : 'danger'">双方比例合计：{{ ratioSum }}%（必须等于 100%）</el-tag>
          </el-form-item>
        </template>
        <el-form-item label="金额分配预览">
          <div class="alloc-preview">
            <el-tag type="warning">甲方：{{ formatCurrency(previewA) }}</el-tag>
            <el-tag type="success">乙方：{{ formatCurrency(previewB) }}</el-tag>
            <el-tag type="info">合计：{{ formatCurrency(previewA + previewB) }}</el-tag>
          </div>
        </el-form-item>
        <el-form-item label="裁决说明">
          <el-input v-model="resolveForm.note" type="textarea" :rows="2" maxlength="500" show-word-limit />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="resolveDialog = false">取消</el-button>
        <el-button type="danger" :loading="submitting" @click="submitResolve">作出裁决</el-button>
      </template>
    </el-dialog>
  </el-card>
</template>

<script setup lang="ts">
import { computed, ref } from 'vue';
import { ElMessage } from 'element-plus';
import type { Contract, DisputeCase } from '../../types';
import {
  ContractStatusLabel,
  DisputeStatusLabel,
  DisputeVerdictLabel,
  DisputeSideLabel
} from '../../types/enums';
import { useUserStore } from '../../stores/user';
import { useDisputeStore } from '../../stores/dispute';
import { formatCurrency, formatDate } from '../../utils/formatCurrency';

const props = defineProps<{ contract: Contract }>();
const emit = defineEmits<{ (e: 'changed'): void }>();

const userStore = useUserStore();
const disputeStore = useDisputeStore();

const submitting = ref(false);

const openCase = computed<DisputeCase | undefined>(() =>
  disputeStore.cases.find((d) => d.contractId === props.contract.id && d.status === 'open')
);
const history = computed<DisputeCase[]>(() =>
  disputeStore.cases
    .filter((d) => d.contractId === props.contract.id && d.status !== 'open')
    .sort((a, b) => (b.id || 0) - (a.id || 0))
);
const isParty = computed(
  () => userStore.user?.id === props.contract.partyAId || userStore.user?.id === props.contract.partyBId
);
const isAdmin = computed(() => userStore.user?.role === 'admin');
const canFile = computed(
  () =>
    isParty.value &&
    !openCase.value &&
    (props.contract.status === 'in_progress' || props.contract.status === 'pending_review')
);

function sideLabel(side: string): string {
  return DisputeSideLabel[side] || side;
}
function normalizeEvidence(e: string): string {
  return /^https?:\/\//.test(e) ? e : '#';
}

// ---- file ----
const fileDialog = ref(false);
const fileForm = ref({ reason: '', claim: '', evidence: [] as string[] });
function openFileDialog() {
  fileForm.value = { reason: '', claim: '', evidence: [] };
  fileDialog.value = true;
}
async function submitFile() {
  if (fileForm.value.reason.trim().length < 5) {
    ElMessage.warning('问题说明至少 5 个字');
    return;
  }
  if (fileForm.value.claim.trim().length < 2) {
    ElMessage.warning('请填写诉求');
    return;
  }
  submitting.value = true;
  try {
    await disputeStore.open(props.contract.id, {
      reason: fileForm.value.reason.trim(),
      claim: fileForm.value.claim.trim(),
      evidence: fileForm.value.evidence
    });
    ElMessage.success('争议已提交，合同流程已暂停');
    fileDialog.value = false;
    emit('changed');
  } finally {
    submitting.value = false;
  }
}

// ---- supplement ----
const supplementDialog = ref(false);
const supplementForm = ref({ content: '', evidence: [] as string[] });
function openSupplement() {
  supplementForm.value = { content: '', evidence: [] };
  supplementDialog.value = true;
}
async function submitSupplement() {
  if (!openCase.value) return;
  if (supplementForm.value.content.trim().length < 2) {
    ElMessage.warning('补充说明至少 2 个字');
    return;
  }
  submitting.value = true;
  try {
    await disputeStore.supplement(openCase.value.id, {
      content: supplementForm.value.content.trim(),
      evidence: supplementForm.value.evidence,
      version: openCase.value.version
    });
    ElMessage.success('材料已补充');
    supplementDialog.value = false;
  } finally {
    submitting.value = false;
  }
}

// ---- withdraw ----
const withdrawDialog = ref(false);
const withdrawReason = ref('');
function openWithdraw() {
  withdrawReason.value = '';
  withdrawDialog.value = true;
}
async function submitWithdraw() {
  if (!openCase.value) return;
  submitting.value = true;
  try {
    await disputeStore.withdraw(openCase.value.id, {
      reason: withdrawReason.value.trim(),
      version: openCase.value.version
    });
    ElMessage.success('案件已撤回，合同恢复原流程');
    withdrawDialog.value = false;
    emit('changed');
  } finally {
    submitting.value = false;
  }
}

// ---- resolve ----
const resolveDialog = ref(false);
const resolveForm = ref({
  verdict: 'full_to_b' as 'full_to_b' | 'full_refund_a' | 'proportional',
  partyAPercent: 40,
  partyBPercent: 60,
  note: ''
});
const ratioSum = computed(() => (resolveForm.value.partyAPercent || 0) + (resolveForm.value.partyBPercent || 0));
const previewA = computed(() => {
  const total = props.contract.totalAmount || 0;
  if (resolveForm.value.verdict === 'full_to_b') return 0;
  if (resolveForm.value.verdict === 'full_refund_a') return total;
  return Math.round(total * (resolveForm.value.partyAPercent || 0)) / 100;
});
const previewB = computed(() => {
  const total = props.contract.totalAmount || 0;
  if (resolveForm.value.verdict === 'full_to_b') return total;
  if (resolveForm.value.verdict === 'full_refund_a') return 0;
  // Cents-exact: B gets the remainder so A + B always equals the total.
  return Math.round(total * 100 - Math.round(total * (resolveForm.value.partyAPercent || 0))) / 100;
});
function openResolve() {
  resolveForm.value = { verdict: 'full_to_b', partyAPercent: 40, partyBPercent: 60, note: '' };
  resolveDialog.value = true;
}
async function submitResolve() {
  if (!openCase.value) return;
  if (resolveForm.value.verdict === 'proportional' && ratioSum.value !== 100) {
    ElMessage.error('余额分配不守恒：双方比例之和必须等于 100%');
    return;
  }
  submitting.value = true;
  try {
    await disputeStore.resolve(openCase.value.id, {
      verdict: resolveForm.value.verdict,
      partyARatio: resolveForm.value.verdict === 'proportional' ? resolveForm.value.partyAPercent / 100 : undefined,
      partyBRatio: resolveForm.value.verdict === 'proportional' ? resolveForm.value.partyBPercent / 100 : undefined,
      note: resolveForm.value.note.trim() || undefined,
      version: openCase.value.version
    });
    ElMessage.success('裁决已作出，合同已按结果结算');
    resolveDialog.value = false;
    emit('changed');
  } finally {
    submitting.value = false;
  }
}
</script>

<style scoped>
.dispute-card { margin-bottom: 16px; }
.d-head { display: flex; justify-content: space-between; align-items: center; }
.freeze-tip { margin-bottom: 12px; }
.case-meta { display: flex; gap: 16px; flex-wrap: wrap; color: #606266; font-size: 13px; margin-bottom: 10px; }
.case-desc { margin-bottom: 12px; }
.materials { display: flex; flex-direction: column; gap: 10px; }
.material { border: 1px solid #ebeef5; border-radius: 6px; padding: 10px 12px; background: #fafafa; }
.m-head { display: flex; align-items: center; gap: 8px; margin-bottom: 6px; }
.m-content { margin: 4px 0; white-space: pre-wrap; }
.m-evidence { display: flex; flex-direction: column; gap: 2px; }
.case-actions { margin-top: 14px; display: flex; gap: 8px; }
.case-empty { padding: 4px 0; }
.history { margin-top: 12px; }
.hist-title { display: inline-flex; align-items: center; gap: 8px; }
.alloc, .alloc-preview { display: flex; gap: 8px; flex-wrap: wrap; margin: 6px 0; }
:deep(.el-select) { width: 100%; }
</style>
