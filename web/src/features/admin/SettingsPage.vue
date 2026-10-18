<script setup lang="ts">
import { onMounted, reactive, ref, computed } from "vue";
import {
  ElButton,
  ElForm,
  ElFormItem,
  ElInput,
  ElMessage,
  ElOption,
  ElSelect,
  ElSwitch,
} from "element-plus";
import AppLayout from "../../app/AppLayout.vue";
import { api, request } from "../../shared/api";
import {
  useIdentity,
  errorMessage,
  type PublicSettings,
} from "../identity/store";
interface Settings extends PublicSettings {
  version: string;
  deployment: { public_url: string };
}
interface SystemInfo {
  version: string;
  data_directory: string;
  database_type: string;
  master_key_initialized: boolean;
}
import { useRoute } from "vue-router";
import UiIcon from "../../shared/UiIcon.vue";
const route = useRoute();
const tab = computed(() =>
  ["general", "access", "system"].includes(String(route.query.tab))
    ? String(route.query.tab)
    : "general",
);
const identity = useIdentity();
const form = reactive({
  site_name: "",
  timezone: "Asia/Shanghai",
  registration_enabled: false,
  session_ttl_days: 30,
});
const publicURL = ref("");
const etag = ref("");
const info = ref<SystemInfo>();
const busy = ref(false);
const loading = ref(true);
const checking = ref(false);
const error = ref("");
const timezones = [
  ...new Set(["Asia/Shanghai", "UTC", ...Intl.supportedValuesOf("timeZone")]),
];
async function load() {
  loading.value = true;
  error.value = "";
  try {
    const [settings, system] = await Promise.all([
      request<Settings>("/admin/settings"),
      api<SystemInfo>("/admin/system-info"),
    ]);
    Object.assign(form, {
      site_name: settings.data.site_name,
      timezone: settings.data.timezone,
      registration_enabled: settings.data.registration_enabled,
      session_ttl_days: settings.data.session_ttl_days,
    });
    publicURL.value = settings.data.deployment.public_url;
    etag.value = settings.etag;
    info.value = system;
  } catch (err) {
    error.value = errorMessage(err);
  } finally {
    loading.value = false;
  }
}
async function save() {
  if (busy.value) return;
  busy.value = true;
  error.value = "";
  try {
    const r = await request<Settings>("/admin/settings", {
      method: "PATCH",
      etag: etag.value,
      body: { ...form },
    });
    Object.assign(form, {
      site_name: r.data.site_name,
      timezone: r.data.timezone,
      registration_enabled: r.data.registration_enabled,
      session_ttl_days: r.data.session_ttl_days,
    });
    etag.value = r.etag;
    await identity.refresh();
    ElMessage.success("系统设置已保存。");
  } catch (err) {
    error.value = errorMessage(err);
  } finally {
    busy.value = false;
  }
}
async function check() {
  checking.value = true;
  try {
    await api("/admin/system-info/database-check", {});
    ElMessage.success("数据库连接正常。");
  } catch (err) {
    ElMessage.error(errorMessage(err));
  } finally {
    checking.value = false;
  }
}
onMounted(load);
</script>
<template>
  <AppLayout>
    <div class="page-heading">
      <div>
        <h1>系统设置</h1>
        <p>配置本系统的基本信息、注册策略与登录会话。</p>
      </div>
      <div class="actions">
        <ElButton :loading="loading" :disabled="busy" @click="load"
          ><UiIcon name="refresh" />重新加载</ElButton
        >
      </div>
    </div>
    <div v-if="error" class="error-banner" role="alert">{{ error }}</div>
    <div class="settings-layout">
      <nav class="settings-nav" aria-label="系统设置分类">
        <RouterLink to="/admin/settings" :class="{ active: tab === 'general' }"
          ><UiIcon name="globe" />基本设置</RouterLink
        ><RouterLink
          to="/admin/settings?tab=access"
          :class="{ active: tab === 'access' }"
          ><UiIcon name="shield" />注册与会话</RouterLink
        ><RouterLink
          to="/admin/settings?tab=system"
          :class="{ active: tab === 'system' }"
          ><UiIcon name="database" />运行信息</RouterLink
        >
      </nav>
      <section v-if="etag" class="panel">
        <div class="panel-header">
          <h2>
            {{
              { general: "基本设置", access: "注册与会话", system: "运行信息" }[
                tab
              ]
            }}
          </h2>
          <p class="section-description">
            {{
              {
                general: "为你的开发协作系统设置名称与访问地址。",
                access: "控制账号如何加入系统，以及登录会话的有效时间。",
                system: "查看应用安装状态与运行配置。",
              }[tab]
            }}
          </p>
        </div>
        <ElForm
          v-if="tab !== 'system'"
          label-position="top"
          :disabled="busy"
          @submit.prevent="save"
        >
          <div class="settings-body">
            <template v-if="tab === 'general'">
              <div class="settings-section">
                <h3>系统信息</h3>
                <ElFormItem label="系统名称" required
                  ><ElInput
                    v-model="form.site_name"
                    maxlength="40" /></ElFormItem
                ><ElFormItem label="访问地址"
                  ><ElInput :model-value="publicURL" readonly
                /></ElFormItem>
                <p class="field-help">
                  由部署配置指定，仅在此展示；通过配置文件或环境变量修改。
                </p>
              </div>
              <div class="settings-section">
                <h3>区域设置</h3>
                <ElFormItem label="系统时区" required
                  ><ElSelect v-model="form.timezone" filterable
                    ><ElOption
                      v-for="timezone in timezones"
                      :key="timezone"
                      :label="timezone"
                      :value="timezone" /></ElSelect
                ></ElFormItem>
                <p class="field-help">影响系统日期与审计记录的显示。</p>
              </div>
            </template>
            <template v-else>
              <div class="settings-section">
                <h3>用户注册</h3>
                <div class="setting-toggle-row">
                  <div>
                    <h3>允许用户自主注册</h3>
                    <p>
                      默认关闭。开启后，用户可注册普通用户账号，无需审批，成功后自动登录。
                    </p>
                  </div>
                  <ElSwitch
                    v-model="form.registration_enabled"
                    aria-label="允许用户自主注册"
                  />
                </div>
                <div class="field-hint">
                  关闭仅阻止新注册，不影响已有账号；管理员仍可在用户管理中添加用户。
                </div>
              </div>
              <div class="settings-section">
                <h3>登录会话</h3>
                <ElFormItem label="会话闲置有效期"
                  ><ElSelect v-model="form.session_ttl_days"
                    ><ElOption
                      v-for="days in 30"
                      :key="days"
                      :label="`${days} 天`"
                      :value="days" /></ElSelect
                ></ElFormItem>
                <p class="field-help">
                  默认 30
                  天；有效访问时自动续期，连续相应天数未访问则需重新登录。修改只影响新会话。
                </p>
              </div>
              <div class="login-protection">
                <UiIcon
                  name="shield"
                />禁用账号或重置密码后，该账号的已有会话将被撤销。
              </div>
            </template>
          </div>
          <div class="settings-save">
            <span>配置变更后对整个系统生效</span
            ><ElButton
              type="primary"
              native-type="submit"
              :loading="busy"
              :disabled="busy"
              >保存更改</ElButton
            >
          </div>
        </ElForm>
        <div v-else-if="info" class="settings-body">
          <div class="settings-section">
            <h3>应用信息</h3>
            <dl class="detail-list">
              <dt>版本</dt>
              <dd class="mono">{{ info.version }}</dd>
              <dt>数据目录</dt>
              <dd class="mono">{{ info.data_directory }}</dd>
              <dt>主密钥</dt>
              <dd>
                <span class="status active">{{
                  info.master_key_initialized ? "已初始化" : "未初始化"
                }}</span>
              </dd>
            </dl>
          </div>
          <div class="settings-section">
            <div class="between section-heading">
              <h3>数据库</h3>
              <ElButton :loading="checking" @click="check"
                ><UiIcon name="database" />检测连接</ElButton
              >
            </div>
            <dl class="detail-list">
              <dt>类型</dt>
              <dd>MySQL</dd>
              <dt>安装状态</dt>
              <dd><span class="status active">已初始化</span></dd>
            </dl>
          </div>
        </div>
      </section>
      <div v-else-if="loading" class="panel empty">正在加载系统设置…</div>
    </div>
  </AppLayout>
</template>
