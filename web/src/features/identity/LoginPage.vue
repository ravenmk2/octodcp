<script setup lang="ts">
import { computed, onUnmounted, reactive, ref, watch } from "vue";
import { useRoute, useRouter } from "vue-router";
import { ElButton, ElForm, ElFormItem, ElInput, ElMessage } from "element-plus";
import { useIdentity, errorMessage, validatePassword } from "./store";
import UiIcon from "../../shared/UiIcon.vue";
import BrandLogo from "../../shared/BrandLogo.vue";
import OrbitArt from "../../shared/OrbitArt.vue";
const identity = useIdentity();
const router = useRouter();
const route = useRoute();
const registering = computed(() => route.path === "/register");
const form = reactive({
  username: "",
  display_name: "",
  password: "",
  confirm: "",
});
const busy = ref(false);
const error = ref("");
async function submit() {
  if (busy.value) return;
  error.value = "";
  if (registering.value) {
    error.value = validatePassword(form.password);
    if (!error.value && form.password !== form.confirm)
      error.value = "两次输入的密码不一致。";
    if (error.value) return;
  }
  busy.value = true;
  try {
    if (registering.value)
      await identity.register(form.username, form.display_name, form.password);
    else await identity.login(form.username, form.password);
    form.password = "";
    form.confirm = "";
    await router.replace(
      identity.session?.require_password_change ? "/account/security" : "/home",
    );
  } catch (err) {
    error.value = errorMessage(err);
  } finally {
    busy.value = false;
  }
}
watch(
  () => route.path,
  () => {
    error.value = "";
    form.password = "";
    form.confirm = "";
  },
);
onUnmounted(() => {
  form.password = "";
  form.confirm = "";
});
</script>

<template>
  <div class="auth-page">
    <aside class="auth-visual">
      <BrandLogo /><OrbitArt />
      <div class="auth-message">
        <div class="eyebrow">YOUR NEXT CHAPTER STARTS HERE</div>
        <h1>让每一次协作，<br />都向前一步。</h1>
        <p>
          连接人与工具，让开发更有序。<br />你的开发协作空间，从 OctoDCP 开始。
        </p>
      </div>
      <div class="auth-visual-footer">
        <span class="tiny-line" />DEVELOP. CONNECT. COLLABORATE.
      </div>
    </aside>
    <main class="auth-main">
      <div class="auth-form">
        <div class="eyebrow">
          WELCOME TO {{ identity.settings.site_name.toUpperCase() }}
        </div>
        <h1>{{ registering ? "创建你的账号" : "欢迎回来" }}</h1>
        <p class="subtitle">
          {{
            registering
              ? "无需审批，创建成功后自动登录并进入首页。"
              : "登录账号，继续你的开发协作之旅。"
          }}
        </p>
        <template v-if="registering && !identity.settings.registration_enabled"
          ><div class="register-closed">
            <div class="confirm-icon"><UiIcon name="lock" /></div>
            <h3>本系统暂未开放注册</h3>
            <p>请联系系统管理员为你创建账号。</p>
          </div>
          <RouterLink class="btn auth-return" to="/login"
            >返回登录</RouterLink
          ></template
        >
        <template v-else>
          <ElForm
            label-position="top"
            :disabled="busy"
            @submit.prevent="submit"
          >
            <ElFormItem v-if="registering" label="显示名称" required
              ><ElInput
                v-model="form.display_name"
                name="display-name"
                autocomplete="nickname"
                placeholder="你的姓名或常用称呼"
            /></ElFormItem>
            <ElFormItem label="用户名" required
              ><ElInput
                v-model="form.username"
                name="username"
                autocomplete="username"
                autocapitalize="none"
                :spellcheck="false"
                :placeholder="
                  registering ? '设置用于登录的用户名' : '请输入用户名'
                "
            /></ElFormItem>
            <ElFormItem label="密码" required
              ><ElInput
                v-model="form.password"
                name="password"
                type="password"
                show-password
                :autocomplete="
                  registering ? 'new-password' : 'current-password'
                "
                :placeholder="registering ? '至少 8 个字符' : '请输入密码'"
            /></ElFormItem>
            <ElFormItem v-if="registering" label="确认密码" required
              ><ElInput
                v-model="form.confirm"
                name="confirm-password"
                type="password"
                show-password
                autocomplete="new-password"
                placeholder="再次输入密码"
            /></ElFormItem>
            <div v-if="!registering" class="between auth-session-hint">
              <span class="muted"
                >{{
                  identity.settings.session_ttl_days
                }}
                天内有效，使用时自动续期</span
              ><button
                class="btn link"
                type="button"
                @click="
                  ElMessage.info(
                    '请联系管理员重置密码。创始人需由部署维护者协助恢复。',
                  )
                "
              >
                忘记密码？
              </button>
            </div>
            <div v-if="error" class="error-text" role="alert">{{ error }}</div>
            <ElButton
              class="auth-submit"
              type="primary"
              native-type="submit"
              :loading="busy"
              :disabled="busy"
              >{{ registering ? "创建账号" : "登录" }}<UiIcon name="arrow"
            /></ElButton>
          </ElForm>
          <div class="auth-bottom">
            {{ registering ? "已有账号？" : "还没有账号？"
            }}<RouterLink :to="registering ? '/login' : '/register'">{{
              registering
                ? "返回登录"
                : identity.settings.registration_enabled
                  ? "创建账号"
                  : "注册暂未开放"
            }}</RouterLink>
          </div>
        </template>
        <div class="auth-legal">
          Octopus-assisted Development &amp; Collaboration Platform
        </div>
      </div>
    </main>
  </div>
</template>
