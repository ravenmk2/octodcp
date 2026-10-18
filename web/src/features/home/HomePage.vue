<script setup lang="ts">
import AppLayout from "../../app/AppLayout.vue";
import UiIcon from "../../shared/UiIcon.vue";
import { useIdentity } from "../identity/store";
const identity = useIdentity();
</script>
<template>
  <AppLayout>
    <div class="page-heading">
      <div>
        <h1>首页</h1>
        <p>{{ identity.user?.display_name }}，欢迎回来。</p>
      </div>
    </div>
    <section class="panel workspace-welcome">
      <div class="workspace-welcome-copy">
        <span class="eyebrow">YOUR WORKSPACE</span>
        <h2>从这里开始，专注每一次协作。</h2>
        <p>管理个人资料和账号安全，让协作从一个完整的账号开始。</p>
        <span class="workspace-site"
          ><UiIcon name="globe" />{{ identity.settings.site_name
          }}<span class="status active">账号正常</span></span
        >
      </div>
      <span class="workspace-welcome-icon"
        ><img src="/logo.svg" class="octo-logo" alt=""
      /></span>
    </section>
    <div class="workspace-links">
      <RouterLink class="overview-shortcut" to="/account"
        ><span class="shortcut-icon"><UiIcon name="user" /></span
        ><span
          ><strong>个人资料</strong
          ><small>更新头像和显示名称</small></span
        ><UiIcon name="chevron"
      /></RouterLink>
      <RouterLink class="overview-shortcut" to="/account/security"
        ><span class="shortcut-icon"><UiIcon name="key" /></span
        ><span
          ><strong>账号安全</strong><small>修改密码与查看登录会话</small></span
        ><UiIcon name="chevron"
      /></RouterLink>
      <RouterLink v-if="identity.isAdmin" class="overview-shortcut" to="/admin"
        ><span class="shortcut-icon"><UiIcon name="shield" /></span
        ><span
          ><strong>控制台</strong><small>查看系统概览与管理设置</small></span
        ><UiIcon name="chevron"
      /></RouterLink>
    </div>
  </AppLayout>
</template>
