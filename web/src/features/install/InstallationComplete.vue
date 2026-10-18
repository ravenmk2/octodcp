<script setup lang="ts">
import { onMounted, onUnmounted, ref } from "vue";
import { useRouter } from "vue-router";
import { ElButton } from "element-plus";
import UiIcon from "../../shared/UiIcon.vue";

const router = useRouter();
const seconds = ref(5);
const navigating = ref(false);
const error = ref("");
let timer: ReturnType<typeof setInterval> | undefined;

async function enterHome() {
  if (navigating.value) return;
  clearInterval(timer);
  navigating.value = true;
  error.value = "";
  try {
    const failure = await router.replace({ name: "home" });
    if (failure) error.value = "暂时未能打开首页，请重试。";
  } catch {
    error.value = "暂时未能打开首页，请重试。";
  } finally {
    navigating.value = false;
  }
}

onMounted(() => {
  const deadline = Date.now() + seconds.value * 1000;
  timer = setInterval(() => {
    seconds.value = Math.max(0, Math.ceil((deadline - Date.now()) / 1000));
    if (seconds.value === 0) void enterHome();
  }, 250);
});
onUnmounted(() => clearInterval(timer));
</script>

<template>
  <div class="success-orb"><UiIcon name="check" /></div>
  <div class="eyebrow">ALL SET. LET'S BUILD.</div>
  <h1>你的系统，准备就绪</h1>
  <p class="subtitle">
    数据库、主密钥和创始人账号已初始化。<br /><span v-if="error" role="alert">{{
      error
    }}</span
    ><span v-else>{{ seconds }} 秒后自动进入首页。</span>
  </p>
  <div class="install-complete-list">
    <div
      v-for="item in [
        'MySQL 表结构已初始化',
        '系统主密钥已生成',
        '创始人账号已创建',
        '安装入口已关闭',
      ]"
      :key="item"
      class="flex"
    >
      <UiIcon name="check" />{{ item }}
    </div>
  </div>
  <ElButton
    class="installation-enter"
    type="primary"
    :loading="navigating"
    @click="enterHome"
    >立即进入首页<UiIcon name="arrow"
  /></ElButton>
</template>
