<script setup lang="ts">
import { computed, ref, watch } from "vue";
import { useRoute, useRouter } from "vue-router";
import {
  ElDropdown,
  ElDropdownMenu,
  ElDropdownItem,
  ElMessage,
} from "element-plus";
import UiIcon from "../shared/UiIcon.vue";
import BrandLogo from "../shared/BrandLogo.vue";
import UserAvatar from "../shared/UserAvatar.vue";
import {
  useIdentity,
  identityLabel,
  errorMessage,
} from "../features/identity/store";
const identity = useIdentity();
const route = useRoute();
const router = useRouter();
const consoleMode = computed(() => route.path.startsWith("/admin"));
const collapsed = ref(
  localStorage.getItem("octodcp.sidebar-collapsed") === "true",
);
const mobileOpen = ref(false);
watch(collapsed, (value) =>
  localStorage.setItem("octodcp.sidebar-collapsed", String(value)),
);
watch(
  () => route.path,
  () => {
    mobileOpen.value = false;
  },
);
async function userCommand(command: string | number | object) {
  if (command === "logout") {
    try {
      await identity.logout();
      await router.replace("/login");
    } catch (err) {
      ElMessage.error(errorMessage(err));
    }
  } else if (typeof command === "string") void router.push(command);
}
</script>
<template>
  <div class="app-shell" :class="{ 'sidebar-collapsed': collapsed }">
    <button
      v-if="mobileOpen"
      class="mobile-scrim show"
      aria-label="关闭导航"
      @click="mobileOpen = false"
    />
    <aside
      class="sidebar"
      :class="{ 'mobile-open': mobileOpen }"
      aria-label="侧边栏"
    >
      <div class="sidebar-heading">
        <BrandLogo />
        <div class="brand-sub">DEVELOP. CONNECT.<br />COLLABORATE.</div>
      </div>
      <nav
        class="sidebar-main"
        :aria-label="consoleMode ? '控制台导航' : '主导航'"
      >
        <template v-if="consoleMode">
          <div class="nav-label">控制台</div>
          <RouterLink
            class="nav-item"
            title="概览"
            aria-label="概览"
            to="/admin"
            :class="{ active: route.path === '/admin' }"
            ><UiIcon name="dashboard" /><span class="nav-text"
              >概览</span
            ></RouterLink
          >
          <RouterLink
            class="nav-item"
            title="用户管理"
            aria-label="用户管理"
            to="/admin/users"
            :class="{ active: route.path === '/admin/users' }"
            ><UiIcon name="users" /><span class="nav-text"
              >用户管理</span
            ></RouterLink
          >
          <RouterLink
            class="nav-item"
            title="系统设置"
            aria-label="系统设置"
            to="/admin/settings"
            :class="{ active: route.path === '/admin/settings' }"
            ><UiIcon name="settings" /><span class="nav-text"
              >系统设置</span
            ></RouterLink
          >
        </template>
        <template v-else>
          <RouterLink
            class="nav-item"
            title="首页"
            aria-label="首页"
            to="/home"
            :class="{ active: route.path === '/home' }"
            ><UiIcon name="home" /><span class="nav-text"
              >首页</span
            ></RouterLink
          >
          <RouterLink
            class="nav-item"
            title="个人设置"
            aria-label="个人设置"
            to="/account"
            :class="{ active: route.path.startsWith('/account') }"
            ><UiIcon name="user" /><span class="nav-text"
              >个人设置</span
            ></RouterLink
          >
        </template>
      </nav>
      <div v-if="identity.user" class="sidebar-bottom">
        <ElDropdown
          class="sidebar-account-dropdown"
          trigger="click"
          placement="top-start"
          popper-class="account-dropdown"
          @command="userCommand"
        >
          <button
            class="user-chip sidebar-account"
            :aria-label="`账号菜单：${identity.user.display_name}`"
            :title="`${identity.user.display_name} · 账号菜单`"
          >
            <UserAvatar :user="identity.user" /><span class="account-text"
              ><span class="account-name">{{ identity.user.display_name }}</span
              ><span class="account-username"
                >@{{ identity.user.username }}</span
              ></span
            >
            <span v-if="identity.isAdmin" class="sidebar-role">{{
              identityLabel(identity.user)
            }}</span
            ><UiIcon name="more" class="account-more" />
          </button>
          <template #dropdown
            ><ElDropdownMenu>
              <div class="account-menu-header">
                <UserAvatar :user="identity.user" />
                <div>
                  <strong>{{ identity.user.display_name }}</strong
                  ><span>@{{ identity.user.username }}</span>
                </div>
              </div>
              <ElDropdownItem command="/account"
                ><UiIcon name="user" />个人设置</ElDropdownItem
              >
              <ElDropdownItem
                v-if="
                  identity.isAdmin && !identity.session?.require_password_change
                "
                command="/admin"
                ><UiIcon name="shield" />控制台</ElDropdownItem
              >
              <ElDropdownItem command="logout" divided
                ><UiIcon name="logout" />退出登录</ElDropdownItem
              >
            </ElDropdownMenu></template
          >
        </ElDropdown>
      </div>
    </aside>
    <div class="main-shell">
      <header class="topbar">
        <div class="topbar-leading">
          <button
            class="icon-btn sidebar-toggle"
            :aria-label="collapsed ? '展开侧栏' : '收起侧栏'"
            @click="collapsed = !collapsed"
          >
            <UiIcon :name="collapsed ? 'expand' : 'panel'" />
          </button>
          <button
            class="icon-btn mobile-menu"
            aria-label="展开导航"
            :aria-expanded="mobileOpen"
            @click="mobileOpen = !mobileOpen"
          >
            <UiIcon name="menu" />
          </button>
          <nav class="breadcrumb" aria-label="当前位置">
            <RouterLink :to="consoleMode ? '/admin' : '/home'">{{
              consoleMode ? "控制台" : "首页"
            }}</RouterLink
            ><template v-if="route.path !== '/home'"
              ><UiIcon name="chevron" /><span
                class="current"
                aria-current="page"
                >{{ route.meta.title }}</span
              ></template
            >
          </nav>
        </div>
        <div class="topbar-right">
          <nav v-if="consoleMode" class="area-navigation" aria-label="返回导航">
            <RouterLink to="/home"><UiIcon name="back" />返回工作区</RouterLink>
          </nav>
          <ElDropdown
            v-if="identity.user"
            class="mobile-account"
            trigger="click"
            popper-class="account-dropdown"
            @command="userCommand"
          >
            <button class="user-chip" aria-label="打开账号菜单">
              <UserAvatar :user="identity.user" />
            </button>
            <template #dropdown
              ><ElDropdownMenu
                ><ElDropdownItem command="/account"
                  ><UiIcon name="user" />个人设置</ElDropdownItem
                ><ElDropdownItem
                  v-if="
                    identity.isAdmin &&
                    !identity.session?.require_password_change
                  "
                  command="/admin"
                  ><UiIcon name="shield" />控制台</ElDropdownItem
                ><ElDropdownItem command="logout" divided
                  ><UiIcon name="logout" />退出登录</ElDropdownItem
                ></ElDropdownMenu
              ></template
            >
          </ElDropdown>
        </div>
      </header>
      <main class="content">
        <slot />
        <footer class="page-footer">
          <span>© 2026 OctoDCP · 开发 · 连接 · 协作</span
          ><span class="flex"
            ><UiIcon name="shield" />让协作有序，让开发专注</span
          >
        </footer>
      </main>
    </div>
  </div>
</template>
