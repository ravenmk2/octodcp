<script setup lang="ts">
import { onMounted, onUnmounted, reactive, ref } from "vue";
import {
  ElButton,
  ElDialog,
  ElDropdown,
  ElDropdownMenu,
  ElDropdownItem,
  ElForm,
  ElFormItem,
  ElInput,
  ElMessage,
  ElMessageBox,
} from "element-plus";
import AppLayout from "../../app/AppLayout.vue";
import UiIcon from "../../shared/UiIcon.vue";
import { api, request } from "../../shared/api";
import {
  useIdentity,
  identityLabel,
  errorMessage,
  validatePassword,
  type User,
} from "../identity/store";
import { useRoute } from "vue-router";
import UserAvatar from "../../shared/UserAvatar.vue";
import AvatarEditor from "../media/AvatarEditor.vue";
const route = useRoute();
const counts = ref<{
  users_total: number;
  users_active: number;
  admins_total: number;
  users_disabled: number;
}>();
const avatarEditor = ref<InstanceType<typeof AvatarEditor>>();
const avatarChange = ref<string | null>();
const avatarProcessing = ref(false);
const identity = useIdentity();
const users = ref<User[]>([]);
const total = ref(0);
const loading = ref(false);
const busy = ref(false);
const error = ref("");
const dialogError = ref("");
const filters = reactive({
  q: "",
  status: String(route.query.status ?? ""),
  identity: ["founder", "admin", "user"].includes(String(route.query.identity))
    ? String(route.query.identity)
    : "",
  page: 1,
  page_size: 20,
});
const dialog = ref(false);
const mode = ref<"create" | "edit" | "password">("create");
const target = ref<User>();
const etag = ref("");
const form = reactive({
  username: "",
  display_name: "",
  role: "user",
  password: "",
  confirm: "",
});
function canManage(user: User) {
  return !user.is_founder && user.id !== identity.user?.id;
}
function canEdit(user: User) {
  return !user.is_founder || user.id === identity.user?.id;
}
async function load() {
  loading.value = true;
  error.value = "";
  try {
    const query = new URLSearchParams(
      Object.entries(filters).map(([key, value]) => [key, String(value)]),
    );
    const [r, overview] = await Promise.all([
      request<User[]>(`/admin/users?${query}`),
      api<{
        users_total: number;
        users_active: number;
        admins_total: number;
        users_disabled: number;
      }>("/admin/overview"),
    ]);
    counts.value = overview;
    users.value = r.data;
    total.value = r.pagination?.total ?? 0;
  } catch (err) {
    error.value = errorMessage(err);
  } finally {
    loading.value = false;
  }
}
function search() {
  filters.page = 1;
  void load();
}
function clearPassword() {
  form.password = "";
  form.confirm = "";
}
function createUser() {
  avatarEditor.value?.reset();
  avatarChange.value = undefined;
  mode.value = "create";
  target.value = undefined;
  Object.assign(form, {
    username: "",
    display_name: "",
    role: "user",
    password: "",
    confirm: "",
  });
  dialogError.value = "";
  dialog.value = true;
}
async function edit(user: User, password = false) {
  try {
    const r = await request<User>(`/admin/users/${user.id}`);
    avatarEditor.value?.reset();
    avatarChange.value = undefined;
    target.value = r.data;
    etag.value = r.etag;
    mode.value = password ? "password" : "edit";
    Object.assign(form, {
      username: r.data.username,
      display_name: r.data.display_name,
      role: r.data.role,
      password: "",
      confirm: "",
    });
    dialogError.value = "";
    dialog.value = true;
  } catch (err) {
    ElMessage.error(errorMessage(err));
  }
}
async function save() {
  if (busy.value || avatarProcessing.value) return;
  dialogError.value = "";
  if (mode.value !== "edit") {
    dialogError.value = validatePassword(form.password);
    if (!dialogError.value && form.password !== form.confirm)
      dialogError.value = "两次输入的密码不一致。";
    if (dialogError.value) return;
  }
  busy.value = true;
  try {
    if (mode.value === "create")
      await request("/admin/users", {
        method: "POST",
        body: {
          username: form.username,
          display_name: form.display_name,
          role: form.role,
          password: form.password,
          ...(avatarChange.value !== undefined
            ? { avatar_upload_id: avatarChange.value }
            : {}),
        },
      });
    else if (mode.value === "password")
      await request(`/admin/users/${target.value!.id}/password`, {
        method: "PUT",
        etag: etag.value,
        body: { new_password: form.password },
      });
    else
      await request(`/admin/users/${target.value!.id}`, {
        method: "PATCH",
        etag: etag.value,
        body: {
          username: form.username,
          display_name: form.display_name,
          ...(avatarChange.value !== undefined
            ? { avatar_upload_id: avatarChange.value }
            : {}),
        },
      });
    avatarEditor.value?.saved();
    clearPassword();
    dialog.value = false;
    ElMessage.success(
      mode.value === "password"
        ? "密码已重置，用户下次登录需要改密。"
        : "用户信息已保存。",
    );
    await identity.refresh();
    await load();
  } catch (err) {
    dialogError.value = errorMessage(err);
  } finally {
    busy.value = false;
  }
}
async function command(value: string | number | object, user: User) {
  if (value === "password") {
    await edit(user, true);
    return;
  }
  try {
    const current = await request<User>(`/admin/users/${user.id}`);
    const u = current.data;
    if (value === "delete") {
      const result = await ElMessageBox.prompt(
        `删除后无法恢复，用户名不会释放。请输入 ${u.username} 确认删除。`,
        "删除用户",
        {
          confirmButtonText: "确认删除",
          cancelButtonText: "取消",
          inputValidator: (v) => v === u.username || "请输入完全一致的用户名。",
        },
      );
      await request(`/admin/users/${u.id}`, {
        method: "DELETE",
        etag: current.etag,
        headers: { "X-Confirm-Username": result.value },
      });
    } else {
      const role = value === "role";
      const next = role
        ? u.role === "admin"
          ? "user"
          : "admin"
        : u.status === "active"
          ? "disabled"
          : "active";
      const label = role
        ? next === "admin"
          ? "设为管理员"
          : "取消管理员身份"
        : next === "disabled"
          ? "禁用"
          : "启用";
      await ElMessageBox.confirm(
        `确定${label}“${u.display_name}”吗？此用户现有的登录会话将失效。`,
        "确认操作",
        { confirmButtonText: "确定", cancelButtonText: "取消" },
      );
      await request(`/admin/users/${u.id}/${role ? "role" : "status"}`, {
        method: "PUT",
        etag: current.etag,
        body: role ? { role: next } : { status: next },
      });
    }
    ElMessage.success("操作已完成。");
    await load();
  } catch (err) {
    if (err !== "cancel" && err !== "close") ElMessage.error(errorMessage(err));
  }
}
onMounted(() => {
  void load();
  if (route.query.create === "1") createUser();
});
onUnmounted(clearPassword);
</script>
<template>
  <AppLayout>
    <div class="page-heading">
      <div>
        <h1>用户管理</h1>
        <p>管理系统成员、账号状态与管理员身份。</p>
      </div>
      <div class="actions">
        <ElButton type="primary" @click="createUser"
          ><UiIcon name="plus" />添加用户</ElButton
        >
      </div>
    </div>
    <div v-if="error" class="error-banner" role="alert">{{ error }}</div>
    <div v-if="counts" class="stats-grid">
      <div
        v-for="stat in [
          {
            label: '全部用户',
            value: counts.users_total,
            description: '共同构建，更好协作',
            icon: 'users' as const,
            color: '',
          },
          {
            label: '正常账号',
            value: counts.users_active,
            description: '可正常登录本系统',
            icon: 'user' as const,
            color: 'green',
          },
          {
            label: '管理员',
            value: counts.admins_total,
            description: '包含 1 位创始人',
            icon: 'shield' as const,
            color: 'orange',
          },
          {
            label: '已禁用',
            value: counts.users_disabled,
            description: '保留数据，暂停访问',
            icon: 'block' as const,
            color: 'gray',
          },
        ]"
        :key="stat.label"
        class="stat"
      >
        <div>
          <div class="stat-label">{{ stat.label }}</div>
          <div class="stat-value">
            {{ stat.value }}<span class="stat-unit">位</span>
          </div>
          <div class="stat-description">{{ stat.description }}</div>
        </div>
        <span class="stat-icon" :class="stat.color"
          ><UiIcon :name="stat.icon"
        /></span>
      </div>
    </div>
    <section class="panel" aria-label="用户列表" :aria-busy="loading">
      <div class="tabs">
        <button
          v-for="status in [
            { value: '', label: '全部用户', count: counts?.users_total },
            { value: 'active', label: '正常账号', count: counts?.users_active },
            {
              value: 'disabled',
              label: '已禁用',
              count: counts?.users_disabled,
            },
          ]"
          :key="status.value"
          class="tab"
          :class="{ active: filters.status === status.value }"
          @click="
            filters.status = status.value;
            search();
          "
        >
          {{ status.label
          }}<span class="tab-count">{{ status.count ?? "…" }}</span>
        </button>
      </div>
      <div class="table-toolbar">
        <label class="search-box"
          ><UiIcon name="search" /><input
            v-model="filters.q"
            aria-label="搜索用户名或显示名称"
            placeholder="搜索用户名或显示名称…"
            @keyup.enter="search" /></label
        ><select
          v-model="filters.identity"
          class="select-filter"
          aria-label="筛选用户身份"
          @change="search"
        >
          <option value="">全部身份</option>
          <option value="founder">创始人</option>
          <option value="admin">管理员</option>
          <option value="user">普通用户</option></select
        ><ElButton :loading="loading" @click="search">查询</ElButton
        ><span class="toolbar-hint"
          ><UiIcon name="shield" />仅管理员可管理本系统用户</span
        >
      </div>
      <div v-if="users.length" class="table-scroll">
        <table>
          <thead>
            <tr>
              <th>用户</th>
              <th>身份</th>
              <th>状态</th>
              <th>创建时间</th>
              <th>最近登录</th>
              <th>操作</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="user in users" :key="user.id">
              <td>
                <div class="user-cell">
                  <UserAvatar :user="user" />
                  <div>
                    <div class="user-primary">
                      {{ user.display_name
                      }}<span
                        v-if="user.id === identity.user?.id"
                        class="you-badge"
                        >你</span
                      >
                    </div>
                    <div class="user-secondary">@{{ user.username }}</div>
                  </div>
                </div>
              </td>
              <td>
                <span
                  class="badge"
                  :class="user.is_founder ? 'founder' : user.role"
                  ><UiIcon v-if="user.role === 'admin'" name="shield" />{{
                    identityLabel(user)
                  }}</span
                >
              </td>
              <td>
                <span class="status" :class="user.status">{{
                  user.status === "active" ? "正常" : "已禁用"
                }}</span>
              </td>
              <td class="mono">{{ identity.date(user.created_at) }}</td>
              <td class="mono">{{ identity.date(user.last_login_at) }}</td>
              <td>
                <div class="row-actions">
                  <button
                    class="btn link"
                    :disabled="!canEdit(user)"
                    @click="edit(user)"
                  >
                    编辑</button
                  ><ElDropdown
                    v-if="canManage(user)"
                    trigger="click"
                    popper-class="action-dropdown"
                    @command="command($event, user)"
                    ><button
                      class="icon-btn row-more"
                      :aria-label="`${user.display_name}的更多操作`"
                    >
                      <UiIcon name="more" /></button
                    ><template #dropdown
                      ><ElDropdownMenu
                        ><ElDropdownItem command="role">{{
                          user.role === "admin"
                            ? "取消管理员身份"
                            : "设为管理员"
                        }}</ElDropdownItem
                        ><ElDropdownItem command="status">{{
                          user.status === "active" ? "禁用用户" : "启用用户"
                        }}</ElDropdownItem
                        ><ElDropdownItem command="password"
                          >重置密码</ElDropdownItem
                        ><ElDropdownItem class="danger" command="delete" divided
                          >删除用户</ElDropdownItem
                        ></ElDropdownMenu
                      ></template
                    ></ElDropdown
                  >
                </div>
              </td>
            </tr>
          </tbody>
        </table>
      </div>
      <div v-else class="empty">
        <UiIcon name="search" />
        <h3>{{ loading ? "正在加载用户" : "没有找到匹配的用户" }}</h3>
        <p v-if="!loading">试试其他关键词，或清除当前筛选条件。</p>
      </div>
      <div class="table-footer">
        <span>共 {{ total }} 位用户 · 每页 {{ filters.page_size }} 位</span>
        <div class="pagination">
          <button
            class="page-button"
            :disabled="filters.page === 1 || loading"
            aria-label="上一页"
            @click="
              filters.page--;
              load();
            "
          >
            <UiIcon name="back" /></button
          ><span
            >{{ filters.page }} /
            {{ Math.max(1, Math.ceil(total / filters.page_size)) }}</span
          ><button
            class="page-button"
            :disabled="filters.page * filters.page_size >= total || loading"
            aria-label="下一页"
            @click="
              filters.page++;
              load();
            "
          >
            <UiIcon name="chevron" />
          </button>
        </div>
      </div>
    </section>
    <div class="info-note">
      <UiIcon name="shield" /><span
        >创始人账号受保护，无法删除、禁用或降级。管理员拥有相同的系统管理权限；普通用户仅可访问个人账号。</span
      >
    </div>
    <ElDialog
      v-model="dialog"
      class="app-dialog"
      align-center
      :title="
        mode === 'create'
          ? '创建用户'
          : mode === 'password'
            ? '重置密码'
            : '编辑用户'
      "
      width="560px"
      :close-on-click-modal="!busy && !avatarProcessing"
      :close-on-press-escape="!busy && !avatarProcessing"
      :show-close="!busy && !avatarProcessing"
      @closed="
        clearPassword();
        avatarEditor?.reset();
        avatarChange = undefined;
      "
    >
      <div v-if="dialogError" class="error-banner" role="alert">
        {{ dialogError }}
      </div>
      <ElForm label-position="top" :disabled="busy" @submit.prevent="save">
        <template v-if="mode !== 'password'">
          <AvatarEditor
            ref="avatarEditor"
            v-model="avatarChange"
            :avatar="target?.avatar"
            :founder="target?.is_founder"
            :disabled="busy"
            @processing="avatarProcessing = $event"
          />
          <div class="form-row spaced">
            <ElFormItem label="显示名称" required
              ><ElInput
                v-model="form.display_name"
                maxlength="30"
                placeholder="姓名或常用称呼" /></ElFormItem
            ><ElFormItem label="用户名" required
              ><ElInput
                v-model="form.username"
                autocomplete="off"
                placeholder="设置用于登录的用户名"
            /></ElFormItem>
          </div>
          <p class="field-help">
            用户名使用 3–30 位小写字母、数字、下划线或短横线，以字母或数字开头。
          </p>
          <ElFormItem v-if="mode === 'create'" label="系统身份" required
            ><div class="role-choices">
              <label class="role-choice"
                ><input
                  v-model="form.role"
                  type="radio"
                  value="user"
                  :disabled="busy"
                /><span
                  ><strong>普通用户</strong
                  ><small>管理个人账号，无法进入控制台。</small></span
                ></label
              ><label class="role-choice"
                ><input
                  v-model="form.role"
                  type="radio"
                  value="admin"
                  :disabled="busy"
                /><span
                  ><strong>管理员</strong
                  ><small>拥有完整系统管理权限。</small></span
                ></label
              >
            </div></ElFormItem
          >
        </template>
        <template v-if="mode !== 'edit'"
          ><ElFormItem label="密码" required
            ><ElInput
              v-model="form.password"
              type="password"
              show-password
              autocomplete="new-password"
          /></ElFormItem>
          <p class="field-help">至少 8 个字符，最多 72 个 UTF-8 字节。</p>
          <ElFormItem label="确认密码" required
            ><ElInput
              v-model="form.confirm"
              type="password"
              show-password
              autocomplete="new-password"
          /></ElFormItem>
          <div class="info-note">
            用户下次登录必须修改密码。请通过合适的方式告知用户初始密码。
          </div></template
        >
        <footer class="form-footer">
          <ElButton :disabled="busy || avatarProcessing" @click="dialog = false"
            >取消</ElButton
          ><ElButton
            type="primary"
            native-type="submit"
            :loading="busy"
            :disabled="busy || avatarProcessing"
            >{{ mode === "create" ? "创建用户" : "保存" }}</ElButton
          >
        </footer>
      </ElForm>
    </ElDialog>
  </AppLayout>
</template>
