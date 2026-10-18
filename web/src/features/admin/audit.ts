export interface AuditEvent {
  id: string;
  actor: {
    type: string;
    user_id: string | null;
    username: string | null;
    display_name: string;
  };
  action: string;
  target: { type: string; id: string | null; label: string };
  occurred_at: string;
  request_id: string | null;
  ip_address: string | null;
  detail: Record<string, unknown>;
}
export const actionLabels: Record<string, string> = {
  "installation.completed": "完成系统安装",
  "auth.login_succeeded": "登录系统",
  "auth.logged_out": "退出登录",
  "user.created": "创建用户",
  "user.registered": "自主注册",
  "user.profile_updated": "更新用户资料",
  "user.avatar_changed": "更新用户头像",
  "user.password_changed": "修改密码",
  "user.password_reset": "重置密码",
  "user.password_recovered": "恢复账号密码",
  "user.role_changed": "调整用户角色",
  "user.disabled": "禁用用户",
  "user.enabled": "启用用户",
  "user.deleted": "删除用户",
  "settings.updated": "更新系统设置",
};
export function actionIcon(action: string) {
  if (action.includes("password") || action.startsWith("auth."))
    return "key" as const;
  if (action.startsWith("settings.")) return "settings" as const;
  if (action.startsWith("installation.")) return "database" as const;
  if (action.startsWith("user.")) return "user" as const;
  return "log" as const;
}
