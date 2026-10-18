<script setup lang="ts">
import { computed, onMounted, onUnmounted, reactive, ref } from "vue";
import {
  ElButton,
  ElForm,
  ElFormItem,
  ElInput,
  ElInputNumber,
  ElOption,
  ElSelect,
} from "element-plus";
import { ApiError, resetCSRF } from "../../shared/api";
import UiIcon from "../../shared/UiIcon.vue";
import BrandLogo from "../../shared/BrandLogo.vue";
import InstallationComplete from "./InstallationComplete.vue";
import { checkDatabase, getStatus, install, type Status } from "./api";

const status = ref<Status>();
const loading = ref(true);
const busy = ref(false);
const step = ref(-1);
const confirmPassword = ref("");
const error = ref("");
const requestId = ref("");
const fieldErrors = ref<Record<string, string>>({});
const databaseChecked = ref(false);
const database = reactive({
  host: "",
  port: 3306,
  name: "octodcp",
  username: "octodcp",
  password: "",
  tls_mode: "disabled" as "disabled" | "verify_identity",
});
const site = reactive({ site_name: "OctoDCP", timezone: "Asia/Shanghai" });
const founder = reactive({ username: "", display_name: "", password: "" });
const steps = [
  { title: "欢迎使用", description: "准备开始你的协作之旅" },
  { title: "连接数据库", description: "使用已有 MySQL 数据库" },
  { title: "系统配置", description: "名称、访问地址与时区" },
  { title: "创始人账号", description: "创建首个管理员" },
  { title: "安装完成", description: "一切就绪，即刻开始" },
];
const passwordBytes = computed(
  () => new TextEncoder().encode(founder.password).length,
);
const timezones = [
  ...new Set(["Asia/Shanghai", "UTC", ...Intl.supportedValuesOf("timeZone")]),
];
const messages: Record<string, string> = {
  too_short: "至少需要 8 个字符。",
  too_many_bytes: "不能超过 72 个 UTF-8 字节。",
  invalid_length: "长度不符合要求。",
  invalid_format: "格式不正确。",
  invalid_timezone: "请选择有效的时区。",
  invalid_connection: "请检查数据库连接信息。",
};
let timer: ReturnType<typeof setTimeout> | undefined;

function clearError() {
  error.value = "";
  requestId.value = "";
  fieldErrors.value = {};
}
function showError(value: unknown) {
  if (value instanceof ApiError) {
    error.value = value.message;
    requestId.value = value.requestId ?? "";
    for (const detail of value.details) {
      if (detail.field)
        fieldErrors.value[detail.field] =
          messages[detail.code] ?? "请检查此项输入。";
    }
  } else error.value = "操作未完成，请稍后重试。";
}
async function refresh() {
  clearTimeout(timer);
  clearError();
  loading.value = true;
  try {
    status.value = await getStatus();
    if (status.value.state === "initializing")
      timer = setTimeout(refresh, 2000);
  } catch (err) {
    showError(err);
  } finally {
    loading.value = false;
  }
}
function invalid(field: string, message: string) {
  fieldErrors.value[field] = message;
  error.value = "请检查输入内容。";
}
function validateFounder() {
  if (!/^[a-z0-9][a-z0-9_-]{2,29}$/.test(founder.username))
    invalid(
      "founder.username",
      "使用 3–30 个小写字母、数字、下划线或连字符，以字母或数字开头。",
    );
  const nameLength = Array.from(founder.display_name.trim()).length;
  if (nameLength < 1 || nameLength > 30)
    invalid("founder.display_name", "请输入 1–30 个字符。");
  if (Array.from(founder.password).length < 8)
    invalid("founder.password", "密码至少需要 8 个字符。");
  if (passwordBytes.value > 72)
    invalid("founder.password", "密码不能超过 72 个 UTF-8 字节。");
  if (founder.password !== confirmPassword.value)
    invalid("confirm", "两次输入的密码不一致。");
  return Object.keys(fieldErrors.value).length === 0;
}
async function next() {
  clearError();
  if (busy.value) return;
  if (step.value === -1) {
    step.value = 0;
    return;
  }
  if (step.value === 1) {
    if (
      Array.from(site.site_name.trim()).length < 1 ||
      Array.from(site.site_name.trim()).length > 40
    ) {
      invalid("site.site_name", "请输入 1–40 个字符。");
      return;
    }
    step.value++;
    return;
  }
  if (step.value === 2 && !validateFounder()) return;
  busy.value = true;
  try {
    if (step.value === 0) {
      await checkDatabase(database);
      databaseChecked.value = true;
      step.value++;
    } else {
      await install({
        database: { ...database },
        site: { ...site },
        founder: { ...founder },
      });
      database.password = "";
      founder.password = "";
      confirmPassword.value = "";
      resetCSRF();
      await refresh();
    }
  } catch (err) {
    showError(err);
    // A lost response must not cause an automatic second installation request.
    if (step.value === 2) {
      try {
        const current = await getStatus();
        status.value = current;
        if (current.state === "ready") {
          database.password = "";
          founder.password = "";
          confirmPassword.value = "";
          resetCSRF();
        }
        if (current.state === "initializing") timer = setTimeout(refresh, 2000);
      } catch {
        /* Keep the original error and inputs for a manual retry. */
      }
    }
  } finally {
    busy.value = false;
  }
}
async function testConnection() {
  if (busy.value) return;
  clearError();
  busy.value = true;
  try {
    await checkDatabase(database);
    databaseChecked.value = true;
  } catch (err) {
    databaseChecked.value = false;
    showError(err);
  } finally {
    busy.value = false;
  }
}
function back() {
  if (!busy.value && step.value > -1) {
    clearError();
    step.value--;
  }
}
onMounted(refresh);
onUnmounted(() => {
  clearTimeout(timer);
  database.password = "";
  founder.password = "";
  confirmPassword.value = "";
});
</script>

<template>
  <div class="install-page">
    <header class="install-header">
      <div class="flex">
        <BrandLogo /><span class="install-label">安装向导</span>
      </div>
    </header>
    <main class="install-card">
      <aside class="install-sidebar">
        <div class="eyebrow">GET STARTED</div>
        <h2>开启协作新篇章</h2>
        <nav class="install-steps" aria-label="安装进度">
          <div
            v-for="(item, index) in steps"
            :key="item.title"
            class="install-step"
            :class="{
              active:
                status?.state === 'ready' ? index === 4 : index === step + 1,
              done: status?.state === 'ready' ? index < 4 : index < step + 1,
            }"
            :aria-current="index === step + 1 ? 'step' : undefined"
          >
            <span class="step-number"
              ><UiIcon
                v-if="status?.state === 'ready' ? index < 4 : index < step + 1"
                name="check"
              /><template v-else>{{ index + 1 }}</template></span
            ><span class="step-copy"
              >{{ item.title }}<small>{{ item.description }}</small></span
            >
          </div>
        </nav>
      </aside>
      <ElForm
        class="install-form-side"
        label-position="top"
        :disabled="busy"
        @submit.prevent="next"
      >
        <div class="install-form-body">
          <div
            v-if="loading && !status"
            class="installation-state"
            aria-live="polite"
          >
            <div class="loading-ring" />
            <h1>正在连接系统</h1>
            <p class="subtitle">检查安装状态，请稍候。</p>
          </div>
          <template v-else-if="status?.state === 'setup_required'">
            <template v-if="step === -1"
              ><div class="welcome-icon">
                <img src="/logo.svg" alt="" class="octo-logo" />
              </div>
              <div class="eyebrow">A GOOD START FOR GREAT WORK</div>
              <h1>欢迎使用 OctoDCP</h1>
              <p class="subtitle">
                只需几步，搭建属于你的开发协作系统。<br />准备好 MySQL
                数据库，接下来交给安装向导。
              </p>
              <div
                v-for="feature in [
                  {
                    icon: 'database' as const,
                    title: '连接你的数据库',
                    description: '使用已有 MySQL 数据库，初始化应用表结构。',
                  },
                  {
                    icon: 'key' as const,
                    title: '保护系统凭据',
                    description: '自动生成主密钥，用于保护系统中的敏感配置。',
                  },
                  {
                    icon: 'shield' as const,
                    title: '创建创始人账号',
                    description: '你的首个管理员，也是本系统的永久创始人。',
                  },
                ]"
                :key="feature.title"
                class="install-feature"
              >
                <span class="feature-icon"
                  ><UiIcon :name="feature.icon"
                /></span>
                <div>
                  <h3>{{ feature.title }}</h3>
                  <p>{{ feature.description }}</p>
                </div>
              </div></template
            >
            <template v-else
              ><div class="eyebrow">
                STEP {{ String(step + 1).padStart(2, "0") }} /
                {{ ["DATABASE", "YOUR SYSTEM", "FOUNDER"][step] }}
              </div>
              <h1>
                {{ ["连接数据库", "给协作一个归属", "创建创始人账号"][step] }}
              </h1>
              <p class="subtitle">
                {{
                  [
                    "填写已准备好的 MySQL 8.x 专用空数据库连接信息。",
                    "设置系统名称与时区；访问地址由部署配置指定。",
                    "这是本系统的首个管理员，拥有完整管理权限。",
                  ][step]
                }}
              </p>
              <div v-if="step === 2" class="inline-alert">
                <UiIcon name="shield" /><span
                  >创始人账号无法被删除、禁用或降级，资料与密码仅允许本人修改。请妥善保管账号信息。</span
                >
              </div>
              <template v-if="step === 0">
                <div class="form-row spaced database-row">
                  <ElFormItem
                    label="数据库主机"
                    required
                    :error="fieldErrors['database']"
                    ><ElInput
                      v-model="database.host"
                      placeholder="数据库主机地址"
                      autocomplete="off" /></ElFormItem
                  ><ElFormItem label="端口" required
                    ><ElInputNumber
                      v-model="database.port"
                      :min="1"
                      :max="65535"
                      :controls="false"
                  /></ElFormItem>
                </div>
                <ElFormItem label="数据库名称" required
                  ><ElInput v-model="database.name" autocomplete="off"
                /></ElFormItem>
                <ElFormItem label="数据库用户名" required
                  ><ElInput v-model="database.username" autocomplete="off"
                /></ElFormItem>
                <ElFormItem label="数据库密码"
                  ><ElInput
                    v-model="database.password"
                    type="password"
                    show-password
                    autocomplete="new-password"
                /></ElFormItem>
                <details class="connection-options">
                  <summary>连接选项</summary>
                  <ElFormItem label="数据库 TLS"
                    ><ElSelect v-model="database.tls_mode"
                      ><ElOption label="不启用 TLS" value="disabled" /><ElOption
                        label="验证服务器身份"
                        value="verify_identity" /></ElSelect
                  ></ElFormItem>
                </details>
                <p class="field-help">
                  检查通过后才能继续。向导会在安装时创建表结构，请不要预先导入
                  SQL。私有 CA 证书由部署维护者配置。
                </p>
                <ElButton :loading="busy" @click="testConnection"
                  ><UiIcon name="database" />测试连接</ElButton
                >
                <div
                  v-if="databaseChecked"
                  class="db-test-result installation-db-confirm"
                >
                  <UiIcon name="check" />连接检查通过，可以继续安装
                </div>
              </template>
              <template v-else-if="step === 1">
                <div
                  v-if="databaseChecked"
                  class="db-test-result installation-db-confirm"
                >
                  <UiIcon name="check" />数据库连接检查通过
                </div>
                <ElFormItem
                  label="系统名称"
                  required
                  :error="fieldErrors['site.site_name']"
                  ><ElInput v-model="site.site_name"
                /></ElFormItem>
                <ElFormItem label="访问地址"
                  ><ElInput :model-value="status.public_url" readonly
                /></ElFormItem>
                <p class="field-help">
                  访问地址由部署配置指定，可联系部署维护者调整。
                </p>
                <ElFormItem
                  label="系统时区"
                  required
                  :error="fieldErrors['site.timezone']"
                  ><ElSelect v-model="site.timezone" filterable
                    ><ElOption
                      v-for="timezone in timezones"
                      :key="timezone"
                      :label="timezone"
                      :value="timezone" /></ElSelect
                ></ElFormItem>
                <div class="info-note">
                  自主注册默认关闭。安装后可在系统设置中开启；开启后注册无需审批。
                </div>
              </template>
              <template v-else>
                <div class="form-row spaced">
                  <ElFormItem
                    label="用户名"
                    required
                    :error="fieldErrors['founder.username']"
                    ><ElInput
                      v-model="founder.username"
                      autocomplete="username"
                      placeholder="设置用于登录的用户名"
                  /></ElFormItem>

                  <ElFormItem
                    label="显示名称"
                    required
                    :error="fieldErrors['founder.display_name']"
                    ><ElInput
                      v-model="founder.display_name"
                      autocomplete="name"
                      placeholder="支持中文"
                  /></ElFormItem>
                </div>
                <p class="field-help">
                  3–30 个小写字母、数字、下划线或连字符，以字母或数字开头。
                </p>
                <ElFormItem
                  label="密码"
                  required
                  :error="fieldErrors['founder.password']"
                  ><ElInput
                    v-model="founder.password"
                    type="password"
                    show-password
                    autocomplete="new-password"
                /></ElFormItem>
                <p class="field-help">
                  至少 8 个字符，最多 72 个 UTF-8 字节；空格会保留。<span
                    v-if="founder.password"
                    >当前 {{ passwordBytes }} 字节。</span
                  >
                </p>
                <ElFormItem
                  label="确认密码"
                  required
                  :error="fieldErrors.confirm"
                  ><ElInput
                    v-model="confirmPassword"
                    type="password"
                    show-password
                    autocomplete="new-password"
                /></ElFormItem>
              </template>
            </template>
            <div v-if="error" class="error-banner" role="alert">
              <strong>{{ error }}</strong
              ><small v-if="requestId">请求编号：{{ requestId }}</small>
            </div>
          </template>
          <InstallationComplete v-else-if="status?.state === 'ready'" />
          <div v-else class="installation-state" aria-live="polite">
            <div v-if="status?.state === 'initializing'" class="loading-ring" />
            <div v-else class="welcome-icon"><UiIcon name="shield" /></div>
            <h1>
              {{
                status?.state === "initializing"
                  ? "正在初始化系统"
                  : status?.state === "recovery_required"
                    ? "需要维护者检查"
                    : "暂时无法访问系统"
              }}
            </h1>
            <p class="subtitle">
              {{
                status?.state === "initializing"
                  ? "正在创建系统，请保持页面开启。完成后会自动更新安装状态。"
                  : status?.state === "recovery_required"
                    ? "检测到已有安装记录或初始化中断。请联系部署维护者检查，现有数据会保留。"
                    : error || "数据库或服务暂时不可用，请稍后重新检查。"
              }}
            </p>
            <ElButton
              v-if="status?.state !== 'initializing'"
              :loading="loading"
              @click="refresh"
              ><UiIcon name="refresh" />重新检查</ElButton
            >
          </div>
        </div>
        <footer
          v-if="status?.state === 'setup_required'"
          class="install-form-footer"
        >
          <ElButton v-if="step > -1" :disabled="busy" @click="back"
            ><UiIcon name="back" />上一步</ElButton
          ><span v-else class="step-counter">大约需要 3 分钟</span
          ><ElButton
            type="primary"
            native-type="submit"
            :loading="busy"
            :disabled="busy"
            >{{
              busy
                ? step === 2
                  ? "正在初始化系统"
                  : "正在验证"
                : step === -1
                  ? "开始安装"
                  : step === 2
                    ? "完成安装"
                    : "下一步"
            }}<UiIcon v-if="!busy" name="arrow"
          /></ElButton>
        </footer>
      </ElForm>
    </main>
    <footer class="install-footer">
      OctoDCP · Octopus-assisted Development &amp; Collaboration Platform<br />DEVELOP.
      CONNECT. COLLABORATE.
    </footer>
  </div>
</template>
