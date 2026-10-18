<script setup lang="ts">
import { onMounted, ref } from "vue";
import { useRouter } from "vue-router";
import { ElButton } from "element-plus";
import AppLayout from "../../app/AppLayout.vue";
import { api } from "../../shared/api";
import { useIdentity, errorMessage } from "../identity/store";
interface Event {
  id: string;
  action: string;
  actor: { display_name: string };
  target: { label: string };
  occurred_at: string;
}
interface Overview {
  users_total: number;
  users_active: number;
  users_disabled: number;
  admins_total: number;
  registration_enabled: boolean;
  session_ttl_days: number;
  recent_events: Event[];
}
import UiIcon from "../../shared/UiIcon.vue";
const identity = useIdentity();
const router = useRouter();
const data = ref<Overview>();
const error = ref("");
const loading = ref(true);
const actions: Record<string, string> = {
  "installation.completed": "完成系统安装",
  "auth.login_succeeded": "登录系统",
  "auth.logged_out": "退出登录",
  "user.created": "创建用户",
  "user.registered": "自主注册",
  "user.profile_updated": "更新用户资料",
  "user.password_changed": "修改密码",
  "user.password_reset": "重置密码",
  "user.role_changed": "调整用户角色",
  "user.disabled": "禁用用户",
  "user.enabled": "启用用户",
  "user.deleted": "删除用户",
  "settings.updated": "更新系统设置",
};
async function load() {
  loading.value = true;
  error.value = "";
  try {
    data.value = await api<Overview>("/admin/overview");
  } catch (err) {
    error.value = errorMessage(err);
  } finally {
    loading.value = false;
  }
}
onMounted(load);
</script>
<template>
  <AppLayout>
    <div class="page-heading">
      <div>
        <h1>系统概览</h1>
        <p>管理系统用户、访问策略与关键操作。</p>
      </div>
      <div class="actions">
        <ElButton :loading="loading" @click="load"
          ><UiIcon name="refresh" />刷新</ElButton
        ><ElButton type="primary" @click="router.push('/admin/users?create=1')"
          ><UiIcon name="plus" />添加用户</ElButton
        >
      </div>
    </div>
    <div v-if="error" class="error-banner" role="alert">{{ error }}</div>
    <template v-if="data">
      <div class="overview-site">
        <span class="overview-site-icon"><UiIcon name="shield" /></span>
        <div>
          <strong>{{ identity.settings.site_name }}</strong>
        </div>
        <span class="overview-site-label">控制台</span>
      </div>
      <div class="stats-grid overview-metrics">
        <RouterLink class="stat overview-stat" to="/admin/users"
          ><span
            ><span class="stat-label">全部用户</span
            ><span class="stat-value">{{ data.users_total }}</span
            ><span class="stat-description">查看全部系统成员</span></span
          ><span class="stat-icon"><UiIcon name="users" /></span
        ></RouterLink>
        <RouterLink class="stat overview-stat" to="/admin/users?status=active"
          ><span
            ><span class="stat-label">正常账号</span
            ><span class="stat-value">{{ data.users_active }}</span
            ><span class="stat-description"
              >{{ data.users_disabled }} 个账号已禁用</span
            ></span
          ><span class="stat-icon green"><UiIcon name="user" /></span
        ></RouterLink>
        <RouterLink class="stat overview-stat" to="/admin/users"
          ><span
            ><span class="stat-label">管理员</span
            ><span class="stat-value">{{ data.admins_total }}</span
            ><span class="stat-description">包含 1 位创始人</span></span
          ><span class="stat-icon orange"><UiIcon name="shield" /></span
        ></RouterLink>
        <RouterLink class="stat overview-stat" to="/admin/settings?tab=access"
          ><span
            ><span class="stat-label">自主注册</span
            ><span class="stat-value">{{
              data.registration_enabled ? "已开放" : "已关闭"
            }}</span
            ><span class="stat-description">查看注册与会话设置</span></span
          ><span class="stat-icon gray"><UiIcon name="key" /></span
        ></RouterLink>
      </div>
      <div class="overview-shortcuts">
        <RouterLink class="overview-shortcut" to="/admin/users"
          ><span class="shortcut-icon"><UiIcon name="users" /></span
          ><span
            ><strong>用户管理</strong><small>成员、身份与账号状态</small></span
          ><UiIcon name="chevron"
        /></RouterLink>
        <RouterLink class="overview-shortcut" to="/admin/settings"
          ><span class="shortcut-icon"><UiIcon name="settings" /></span
          ><span
            ><strong>系统设置</strong><small>系统信息与访问策略</small></span
          ><UiIcon name="chevron"
        /></RouterLink>
        <RouterLink class="overview-shortcut" to="/admin/settings?tab=system"
          ><span class="shortcut-icon"><UiIcon name="database" /></span
          ><span
            ><strong>运行信息</strong><small>应用版本与数据库连接</small></span
          ><UiIcon name="chevron"
        /></RouterLink>
      </div>
      <div class="overview-grid">
        <section class="panel overview-activity">
          <div class="overview-panel-heading">
            <div>
              <h2>最近操作</h2>
              <p>系统近期的关键变更</p>
            </div>
            <UiIcon name="log" />
          </div>
          <div v-if="!data.recent_events.length" class="empty">
            <h3>还没有操作记录</h3>
            <p>系统的关键操作会显示在这里。</p>
          </div>
          <div
            v-for="event in data.recent_events"
            :key="event.id"
            class="activity-item"
          >
            <span class="activity-icon"><UiIcon name="log" /></span
            ><span class="activity-copy"
              ><strong>{{ actions[event.action] ?? event.action }}</strong
              ><span
                >{{ event.actor.display_name
                }}<span class="activity-dot">·</span
                >{{ event.target.label }}</span
              ><time :datetime="event.occurred_at">{{
                identity.date(event.occurred_at)
              }}</time></span
            >
          </div>
        </section>
        <div class="overview-side">
          <section class="panel overview-access">
            <div class="overview-panel-heading">
              <div>
                <h2>访问策略</h2>
                <p>当前生效的账号设置</p>
              </div>
              <UiIcon name="lock" />
            </div>
            <dl class="overview-status">
              <div>
                <dt>用户注册</dt>
                <dd>
                  {{
                    data.registration_enabled ? "允许自主注册" : "由管理员添加"
                  }}
                </dd>
              </div>
              <div>
                <dt>会话闲置有效期</dt>
                <dd>{{ data.session_ttl_days }} 天</dd>
              </div>
              <div>
                <dt>创始人账号</dt>
                <dd>始终受保护</dd>
              </div>
            </dl>
            <RouterLink
              class="overview-panel-link"
              to="/admin/settings?tab=access"
              >管理注册与会话<UiIcon name="arrow"
            /></RouterLink>
          </section>
        </div>
      </div>
    </template>
    <div v-else-if="loading" class="panel empty">正在加载概览…</div>
  </AppLayout>
</template>
