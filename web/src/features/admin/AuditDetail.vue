<script setup lang="ts">
import "./audit.css";
import { ref, watch } from "vue";
import { ElButton, ElDialog } from "element-plus";
import { api } from "../../shared/api";
import { errorMessage, useIdentity } from "../identity/store";
import { actionLabels, type AuditEvent } from "./audit";
const props = defineProps<{ id?: string }>();
const open = defineModel<boolean>({ default: false });
const identity = useIdentity();
const event = ref<AuditEvent>();
const loading = ref(false),
  error = ref("");
let sequence = 0;
async function load() {
  if (!props.id || !open.value) return;
  const current = ++sequence;
  loading.value = true;
  error.value = "";
  event.value = undefined;
  try {
    const data = await api<AuditEvent>(`/admin/audit-events/${props.id}`);
    if (current === sequence) event.value = data;
  } catch (err) {
    if (current === sequence) error.value = errorMessage(err);
  } finally {
    if (current === sequence) loading.value = false;
  }
}
watch([open, () => props.id], () => {
  if (open.value) void load();
  else sequence++;
});
</script>
<template>
  <ElDialog
    v-model="open"
    class="app-dialog audit-dialog"
    title="审计详情"
    width="560px"
    align-center
    append-to-body
  >
    <div v-if="loading" class="audit-loading" role="status">
      正在加载审计详情…
    </div>
    <div v-else-if="error">
      <div class="error-banner" role="alert">{{ error }}</div>
      <ElButton @click="load">重新加载</ElButton>
    </div>
    <template v-else-if="event"
      ><p class="audit-detail-heading">
        {{ actionLabels[event.action] || event.action }} ·
        {{ identity.date(event.occurred_at) }}
      </p>
      <dl class="detail-list audit-metadata">
        <dt>操作者</dt>
        <dd>
          {{ event.actor.display_name
          }}<span v-if="event.actor.username" class="muted">
            @{{ event.actor.username }}</span
          >
        </dd>
        <dt>操作对象</dt>
        <dd>{{ event.target.label }}</dd>
        <dt>事件类型</dt>
        <dd class="mono">{{ event.action }}</dd>
        <dt>执行结果</dt>
        <dd><span class="audit-result">成功</span></dd>
        <dt>来源 IP</dt>
        <dd class="mono">{{ event.ip_address || "未记录" }}</dd>
        <dt>请求编号</dt>
        <dd class="mono">{{ event.request_id || "未记录" }}</dd>
      </dl>
      <h3 class="audit-changes-heading">变更详情</h3>
      <pre v-if="Object.keys(event.detail).length" class="audit-detail">{{
        JSON.stringify(event.detail, null, 2)
      }}</pre>
      <p v-else class="audit-empty-detail">
        本次操作没有可公开的字段变更。
      </p></template
    >
    <footer class="form-footer">
      <ElButton @click="open = false">关闭</ElButton>
    </footer>
  </ElDialog>
</template>
