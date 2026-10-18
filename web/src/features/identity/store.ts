import { defineStore } from "pinia";
import { api, ApiError, request, resetCSRF } from "../../shared/api";
import { getStatus, type Status } from "../install/api";

export interface User {
  id: string;
  username: string;
  display_name: string;
  role: "admin" | "user";
  is_founder: boolean;
  status: "active" | "disabled";
  avatar: { id: string; url: string } | null;
  version: string;
  last_login_at: string | null;
  created_at: string;
  updated_at: string;
}
export interface AuthSession {
  user: User;
  expires_at: string;
  session_ttl_days: number;
  require_password_change: boolean;
}
export interface PublicSettings {
  site_name: string;
  timezone: string;
  registration_enabled: boolean;
  session_ttl_days: number;
}

export const useIdentity = defineStore("identity", {
  state: () => ({
    session: null as AuthSession | null,
    setup: null as Status | null,
    settings: {
      site_name: "OctoDCP",
      timezone: "Asia/Shanghai",
      registration_enabled: false,
      session_ttl_days: 30,
    } as PublicSettings,
  }),
  getters: {
    user: (state) => state.session?.user,
    isAdmin: (state) => state.session?.user.role === "admin",
  },
  actions: {
    async refresh() {
      this.setup = await getStatus();
      if (this.setup.state !== "ready") {
        this.session = null;
        return;
      }
      this.settings = await api<PublicSettings>("/public/settings");
      try {
        this.session = await api<AuthSession>("/auth/session");
      } catch (err) {
        if (err instanceof ApiError && err.code === "UNAUTHENTICATED")
          this.session = null;
        else throw err;
      }
    },
    async login(username: string, password: string) {
      this.session = await api<AuthSession>("/auth/login", {
        username,
        password,
      });
      resetCSRF();
    },
    async register(username: string, display_name: string, password: string) {
      this.session = await api<AuthSession>("/auth/register", {
        username,
        display_name,
        password,
      });
      resetCSRF();
    },
    async logout() {
      await request("/auth/logout", { method: "POST" });
      this.session = null;
      resetCSRF();
    },
    date(value: string | null) {
      return value
        ? new Intl.DateTimeFormat("zh-CN", {
            timeZone: this.settings.timezone,
            dateStyle: "medium",
            timeStyle: "short",
          }).format(new Date(value))
        : "尚无记录";
    },
  },
});

export function identityLabel(user: User) {
  return user.is_founder
    ? "创始人"
    : user.role === "admin"
      ? "管理员"
      : "普通用户";
}
const fieldLabels: Record<string, string> = {
  username: "用户名",
  display_name: "显示名称",
  password: "密码",
  new_password: "新密码",
  site_name: "系统名称",
  timezone: "系统时区",
  session_ttl_days: "会话有效期",
};
const validationMessages: Record<string, string> = {
  too_short: "至少需要 8 个字符",
  too_many_bytes: "不能超过 72 个 UTF-8 字节",
  invalid_length: "长度不符合要求",
  invalid_format: "格式不正确",
  invalid_timezone: "请选择有效时区",
  invalid_value: "请选择有效值",
  null_not_allowed: "不能为空",
};
export function errorMessage(err: unknown) {
  if (!(err instanceof ApiError)) return "操作未完成，请稍后重试。";
  const details =
    err.code === "VALIDATION_FAILED"
      ? err.details
          .filter(
            (d) =>
              d.field && fieldLabels[d.field] && validationMessages[d.code],
          )
          .map((d) => `${fieldLabels[d.field!]}：${validationMessages[d.code]}`)
          .join("；")
      : "";
  return `${details || err.message}${err.requestId ? `（请求编号：${err.requestId}）` : ""}`;
}
export function validatePassword(password: string) {
  return Array.from(password).length < 8
    ? "密码至少需要 8 个字符。"
    : new TextEncoder().encode(password).length > 72
      ? "密码不能超过 72 个 UTF-8 字节。"
      : "";
}
