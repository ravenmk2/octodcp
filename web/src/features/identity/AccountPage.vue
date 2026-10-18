<script setup lang="ts">
import { computed, onUnmounted, reactive, ref, watch } from "vue";
import { useRoute, useRouter } from "vue-router";
import {
  ElButton,
  ElDialog,
  ElForm,
  ElFormItem,
  ElInput,
  ElMessage,
} from "element-plus";
import AppLayout from "../../app/AppLayout.vue";
import { request, resetCSRF } from "../../shared/api";
import {
  useIdentity,
  errorMessage,
  identityLabel,
  validatePassword,
  type User,
} from "./store";
import UserAvatar from "../../shared/UserAvatar.vue";
import UiIcon from "../../shared/UiIcon.vue";
const identity = useIdentity();
const route = useRoute();
const router = useRouter();
const security = computed(() => route.path.endsWith("/security"));
const passwordDialog = ref(
  Boolean(identity.session?.require_password_change && security.value),
);
const busy = ref(false);
const error = ref("");
const form = reactive({
  display_name: identity.user?.display_name ?? "",
  current: "",
  next: "",
  confirm: "",
});
watch(
  () => route.path,
  () => {
    passwordDialog.value = Boolean(
      identity.session?.require_password_change && security.value,
    );
    error.value = "";
    form.current = "";
    form.next = "";
    form.confirm = "";
  },
);
async function save() {
  if (busy.value || !identity.user) return;
  error.value = "";
  if (security.value) {
    error.value = validatePassword(form.next);
    if (!error.value && form.next !== form.confirm)
      error.value = "两次输入的密码不一致。";
    if (error.value) return;
  }
  busy.value = true;
  try {
    if (security.value) {
      await request("/account/password", {
        method: "PUT",
        body: { current_password: form.current, new_password: form.next },
      });
      identity.session = null;
      resetCSRF();
      form.current = "";
      form.next = "";
      form.confirm = "";
      ElMessage.success("密码已更新，请重新登录。");
      await router.replace("/login");
    } else {
      const r = await request<User>("/account", {
        method: "PATCH",
        etag: `"u-${identity.user.id}-v${identity.user.version}"`,
        body: { display_name: form.display_name },
      });
      if (identity.session) identity.session.user = r.data;
      form.display_name = r.data.display_name;
      ElMessage.success("个人资料已保存。");
    }
  } catch (err) {
    error.value = errorMessage(err);
  } finally {
    busy.value = false;
  }
}
async function logout() {
  try {
    await identity.logout();
    await router.replace("/login");
  } catch (err) {
    error.value = errorMessage(err);
  }
}
function clearPassword() {
  form.current = "";
  form.next = "";
  form.confirm = "";
  error.value = "";
}
onUnmounted(() => {
  form.current = "";
  form.next = "";
  form.confirm = "";
});
</script>
<template>
  <AppLayout>
    <div class="page-heading">
      <div>
        <h1>个人设置</h1>
        <p>管理你的个人资料与账号安全。</p>
      </div>
    </div>
    <div v-if="identity.user" class="profile-layout">
      <aside class="panel profile-summary">
        <UserAvatar :user="identity.user" large />
        <div>
          <h2>{{ identity.user.display_name }}</h2>
          <p class="username">@{{ identity.user.username }}</p>
        </div>
        <span
          class="badge"
          :class="identity.user.is_founder ? 'founder' : identity.user.role"
          ><UiIcon v-if="identity.isAdmin" name="shield" />{{
            identityLabel(identity.user)
          }}</span
        >
        <div class="summary-line">
          加入于 {{ identity.date(identity.user.created_at) }}<br />{{
            identity.user.is_founder
              ? "你是本系统的创始人"
              : identity.isAdmin
                ? "你可以访问系统控制台"
                : "你的开发协作之旅，从这里开始"
          }}
        </div>
      </aside>
      <section class="panel">
        <nav class="tabs" aria-label="个人设置分类">
          <RouterLink class="tab" to="/account" :class="{ active: !security }"
            >个人资料</RouterLink
          ><RouterLink
            class="tab"
            to="/account/security"
            :class="{ active: security }"
            >账号安全</RouterLink
          >
        </nav>
        <ElForm
          v-if="!security"
          label-position="top"
          :disabled="busy"
          @submit.prevent="save"
        >
          <div class="settings-body">
            <div v-if="error" class="error-banner" role="alert">
              {{ error }}
            </div>
            <div class="avatar-editor">
              <UserAvatar :user="identity.user" large />
              <div class="avatar-editor-controls">
                <h3>个人头像</h3>
                <ElButton disabled title="头像上传与裁剪将在后续迭代中接入"
                  >上传头像</ElButton
                >
                <p class="field-hint">头像上传与裁剪功能将在后续迭代中接入。</p>
              </div>
            </div>
            <ElFormItem label="显示名称" required
              ><ElInput
                v-model="form.display_name"
                maxlength="30" /></ElFormItem
            ><ElFormItem label="用户名"
              ><ElInput :model-value="identity.user.username" readonly
            /></ElFormItem>
            <p class="field-help">用户名用于登录，当前阶段由管理员维护。</p>
          </div>
          <div class="settings-save">
            <span>你的系统身份：{{ identityLabel(identity.user) }}</span
            ><ElButton
              type="primary"
              native-type="submit"
              :loading="busy"
              :disabled="busy"
              >保存资料</ElButton
            >
          </div>
        </ElForm>
        <div v-else class="settings-body">
          <div
            v-if="error && !passwordDialog"
            class="error-banner"
            role="alert"
          >
            {{ error }}
          </div>
          <div
            v-if="identity.session?.require_password_change"
            class="inline-alert warning"
          >
            <UiIcon name="shield" /><span
              >此账号使用管理员设置的密码，请先修改密码后继续使用系统。</span
            >
          </div>
          <div class="security-row">
            <div>
              <h3>登录密码</h3>
              <p>定期更新密码，保护你的账号。</p>
            </div>
            <ElButton @click="passwordDialog = true"
              ><UiIcon name="key" />修改密码</ElButton
            >
          </div>
          <div class="security-row">
            <div>
              <h3>登录会话</h3>
              <p>
                闲置有效期
                {{ identity.session?.session_ttl_days }}
                天，使用时自动续期，退出后立即失效。
              </p>
            </div>
          </div>
          <div class="security-row">
            <div class="session-device">
              <span class="device-icon"><UiIcon name="device" /></span>
              <div>
                <h3>当前浏览器</h3>
                <p>当前设备 · 正在使用</p>
              </div>
            </div>
            <span class="session-current">当前会话</span>
          </div>
          <div class="security-row">
            <div>
              <h3>退出账号</h3>
              <p>结束当前浏览器中的登录会话。</p>
            </div>
            <ElButton @click="logout"
              ><UiIcon name="logout" />退出登录</ElButton
            >
          </div>
        </div>
      </section>
    </div>
    <div v-if="identity.user?.is_founder" class="info-note">
      <UiIcon name="shield" /><span
        >创始人身份始终保留。你的账号无法被删除、禁用或降级，资料与密码仅允许你本人修改。</span
      >
    </div>
    <ElDialog
      v-model="passwordDialog"
      class="app-dialog"
      align-center
      title="修改密码"
      width="460px"
      :close-on-click-modal="
        !busy && !identity.session?.require_password_change
      "
      :close-on-press-escape="
        !busy && !identity.session?.require_password_change
      "
      :show-close="!busy && !identity.session?.require_password_change"
      @closed="clearPassword"
    >
      <div
        v-if="identity.session?.require_password_change"
        class="inline-alert warning"
      >
        <UiIcon name="shield" /><span
          >首次登录需要设置新密码，完成后重新登录。</span
        >
      </div>
      <div v-if="error" class="error-banner" role="alert">{{ error }}</div>
      <ElForm label-position="top" :disabled="busy" @submit.prevent="save"
        ><ElFormItem label="当前密码" required
          ><ElInput
            v-model="form.current"
            type="password"
            show-password
            autocomplete="current-password" /></ElFormItem
        ><ElFormItem label="新密码" required
          ><ElInput
            v-model="form.next"
            type="password"
            show-password
            autocomplete="new-password"
        /></ElFormItem>
        <p class="field-help">至少 8 个字符，最多 72 个 UTF-8 字节。</p>
        <ElFormItem label="确认新密码" required
          ><ElInput
            v-model="form.confirm"
            type="password"
            show-password
            autocomplete="new-password"
        /></ElFormItem>
        <div class="inline-alert">
          <UiIcon name="lock" /><span
            >修改后所有登录会话都会失效，需要重新登录。</span
          >
        </div>
        <footer class="form-footer">
          <ElButton
            v-if="!identity.session?.require_password_change"
            :disabled="busy"
            @click="passwordDialog = false"
            >取消</ElButton
          ><ElButton
            type="primary"
            native-type="submit"
            :loading="busy"
            :disabled="busy"
            >更新密码</ElButton
          >
        </footer></ElForm
      >
    </ElDialog>
  </AppLayout>
</template>
