# API 设计

一期 REST API 草案，基础路径 `/api/v1`。下方路径均相对此前缀，健康检查除外。规则的确认状态见 [设计入口](README.md)；本文没有邮箱字段、邮箱登录、邮件找回、项目或团队接口。

目前已实现安装、认证会话、用户管理、系统设置与概览接口，以及健康探针。头像上传/读取与完整审计查询仍为待实现契约；当前资料写入不接收 `avatar_upload_id`，User.avatar 返回 null。验证边界见 [开发说明](development.md)。

## 1. 协议约定

- JSON 字段为 `snake_case`，请求 `Content-Type: application/json`，头像上传单独使用 multipart。拒绝未知 JSON 字段，避免客户端提交 `role` 等未获授权字段后被静默忽略。
- ID 为十进制字符串；编辑版本始终是字符串，客户端按不透明值处理，不解析或自行递增。用户版本当前为十进制计数，系统设置为聚合摘要；布尔值和页码为 JSON 原生类型，时间为带 `Z` 的 RFC 3339 UTC 字符串，毫秒精度。
- 成功响应为 `{"data": ...}`；204 无响应体。错误格式如下；`details` 始终为数组，无附加信息时为 `[]`，字段校验和其他可结构化问题使用同一种条目结构。
- 每个请求生成 32 位十六进制 `X-Request-ID`，错误体使用同一值。JSON 与认证响应设置 `Cache-Control: no-store`；头像使用专门的私有缓存规则。
- API 不返回数据库错误、堆栈、数据源地址、密码哈希或令牌摘要。未知 `/api/` 路径返回 JSON 404，不能回退到 SPA HTML。
- JSON 请求体默认上限 64 KiB；上传限制见头像章节。字符串不静默截断；非密码名称可去除首尾空白，密码保持原样。

```json
{
  "error": {
    "code": "VALIDATION_FAILED",
    "message": "请检查输入内容",
    "details": [ { "code": "invalid_format", "field": "username" } ]
  },
  "request_id": "0123456789abcdef0123456789abcdef"
}
```

每个 detail 必须包含稳定的小写 `code`；可选 `field` 指向请求字段路径，例如 `username` 或 `database.port`；可选 `context` 是该错误码明确规定的白名单 JSON 对象。非字段问题省略 field；同一字段可有多个 detail，不把数组当作只能容纳一个错误的对象。未知 code / context 键可忽略，前端仍展示顶层 message 和 request_id。

顶层 `error.code` 决定整次请求的处理，detail.code 提供局部原因。前端按 field 关联表单控件，其他条目进入页面提示；不直接展示 context 的任意序列化内容。不得把驱动错误、SQL、堆栈、密码、令牌或完整输入放入 details。当前 context 按以下契约使用：版本冲突可含 `{resource,id,current_version}`，限流可含 `{retry_after_seconds}` 并与 Retry-After 一致；字段格式错误不回显原值。

非字段示例（仅向有权查看该资源的调用者返回）：

```json
{
  "error": {
    "code": "VERSION_CONFLICT",
    "message": "资料已被其他操作更新，请重新加载",
    "details": [
      { "code": "version_conflict", "context": { "resource": "user", "id": "42", "current_version": "4" } }
    ]
  },
  "request_id": "0123456789abcdef0123456789abcdef"
}
```

### 认证与 CSRF

Cookie 名称建议为 `octodcp_session`，只承载随机会话令牌，属性为 HttpOnly、SameSite=Lax、Path=/，HTTPS 时 Secure。JSON 不返回会话令牌，前端不把认证信息写入 localStorage。

用户会话使用持久化 Cookie，设置与数据库 `expires_at` 一致的 Expires / Max-Age，默认滑动窗口 30 天。通过认证与权限校验的业务 API 请求自动续期，提交后刷新 Cookie；`GET /auth/session` 返回本次续期后的 expires_at。公开接口、静态资源、健康检查、401/无权限请求及退出/撤销操作不续期，不提供独立 refresh-token 接口，也不通过前端心跳无限保活。窗口在会话创建时确定，修改系统有效期只影响新会话；到期会话不能续期。

先调用 `GET /auth/csrf` 获得 `data.csrf_token`，此后所有 POST、PUT、PATCH、DELETE 都发送 `X-CSRF-Token`，包括登录、注册、安装和退出。客户端 Cookie 与该令牌配套使用；登录、退出和安装状态切换后重新获取令牌。CSRF 响应不缓存，不允许跨域通配读取。

登录和注册按来源 IP 与规范用户名做有界限流；安装写请求按来源 IP 限流。429 返回 `Retry-After`。密码哈希并发也有上限，不能只依靠请求速率限制内存消耗。

未安装时只开放静态安装界面、健康检查、`/setup/*` 和 `/auth/csrf`；其余业务接口返回 503 `INSTANCE_NOT_READY`。已安装后关闭安装写接口。数据库暂时不可用返回 503，不伪装成 401 或重新安装。

### 并发与重试

用户详情/写入响应包含 `ETag: "u-<id>-v<version>"`；设置为 `ETag: "settings-<version>"`，其中设置 version 是 64 位小写十六进制 SHA-256 聚合摘要。摘要包含当前设置注册表版本及按键排序的行版本和有效值，避免多行 KV 被当成独立设置覆盖。修改这些资源必须原样回传服务端的 `If-Match`，只接收单个强 ETag，不接受 `*` 或弱 ETag。

- 缺少前置条件返回 428 `PRECONDITION_REQUIRED`；格式错误返回 422；版本落后返回 412 `VERSION_CONFLICT`，前端重新加载后由用户确认修改。
- 涉及权限与创始人/自身保护的限制先校验，版本校验不能绕过这些限制。
- 无实质变化的写入返回当前资源和 ETag，不递增版本、不新增变更审计。
- 本人改密用当前密码和事务锁校验，不要求 If-Match。管理员重置使用 If-Match。
- 一期不提供通用幂等键。GET 可重试；创建、注册、安装、改密遇到网络中断时先查询状态或重新登录，不自动重复提交。

## 2. DTO 与字段约束

### User

```json
{
  "id": "42",
  "username": "raven",
  "display_name": "Raven",
  "role": "admin",
  "is_founder": true,
  "status": "active",
  "avatar": { "id": "81", "url": "/api/v1/avatars/81" },
  "version": "3",
  "last_login_at": "2026-10-18T08:00:00.000Z",
  "created_at": "2026-10-18T07:00:00.000Z",
  "updated_at": "2026-10-18T07:30:00.000Z"
}
```

`avatar`、`last_login_at` 可以为 null。`role` 为 `admin|user`；创始人由 `is_founder` 派生，不接收客户端写入。用户响应不包含 `auth_version`、密码或凭据；强制改密标记仅在认证会话 DTO 中提供。

| 字段 | 校验 |
| --- | --- |
| `username` | `^[a-z0-9][a-z0-9_-]{2,29}$`；全站唯一，已删除用户名不复用 |
| `display_name` | 去除首尾空白后 1–30 个 Unicode 码点 |
| 新密码 | 8–72 个 Unicode 码点且最多 72 UTF-8 字节；超字节上限返回字段错误 `too_many_bytes`，不截断、不 trim |
| `site_name` | 去除首尾空白后 1–40 个 Unicode 码点 |
| `timezone` | 合法 IANA 标识，如 `Asia/Shanghai`、`UTC` |
| `session_ttl_days` | 整数 1–30，默认 30 |
| `avatar_upload_id` | 十进制 ID 字符串；省略为不修改，null 为移除，有值则绑定当前操作者的未过期草稿 |

注册、删除、密码与会话规则的确认记录见 [设计入口](README.md)。登录接口不套用新密码的最小长度，以便将来调整策略后旧密码仍可正常验证；超出 bcrypt 72 字节上限的登录输入直接返回统一认证失败，不能截断后比较。前端用 UTF-8 字节长度提示用户，密码原文只通过 HTTPS 传递，不在浏览器先做一次 HMAC 代替服务端密码验证。

### AuthSession

```json
{
  "data": {
    "user": { "id": "42", "username": "raven", "display_name": "Raven", "role": "admin", "is_founder": true, "status": "active", "avatar": null, "version": "1", "last_login_at": "2026-10-18T08:00:00.000Z", "created_at": "2026-10-18T07:00:00.000Z", "updated_at": "2026-10-18T07:00:00.000Z" },
    "expires_at": "2026-11-17T08:00:00.000Z",
    "session_ttl_days": 30,
    "require_password_change": false
  }
}
```

AuthSession.session_ttl_days 返回该会话创建时的滑动窗口，expires_at 为最近一次有效续期后的实际到期时间。

强制改密会话仅能使用 `/auth/session`、`/auth/csrf`、`/account/password`、`/auth/logout` 及自己的当前头像 GET。其他已登录接口返回 403 `PASSWORD_CHANGE_REQUIRED`。前端随即进入改密页，服务端同样执行限制。

## 3. 安装接口

安装不需要安装令牌或安装会话。本地配置缺失或为空，且没有安装锁或其他安装痕迹时才开放向导；目标数据库为空才能执行初始化。完成后最后写入 `data/install.lock`，存在时直接跳过安装；配置或数据库异常进入维护状态。详细落盘、恢复与数据库锁规则见 [部署设计](deployment.md)。安装失败时不把完整连接信息返回浏览器。

| 方法与路径 | 授权 | 请求 / 响应 |
| --- | --- | --- |
| `GET /setup/status` | 公开 | 200：`{state, public_url}`；五种启动状态及只读访问地址，不返回路径或连接详情 |
| `POST /setup/database-check` | 未安装 + CSRF | 数据库连接对象；200：`{reachable:true, compatible:true, empty_schema:true}`，失败给出有限的分类错误 |
| `POST /setup/install` | 未安装 + CSRF | 下方对象；201：`{installation_id,state:"ready",authenticated}`；写入持久化安装锁后，为提交成功请求的浏览器创建创始人会话 Cookie；前端显示 5 秒倒计时及手动按钮进入 `/home` |

数据库连接对象：`{host,port,name,username,password,tls_mode}`。`port` 为 1–65535 整数；`tls_mode` 仅允许 `disabled|verify_identity`，不提供跳过证书验证模式。私有 CA 路径由部署配置给出，浏览器不能指定任意磁盘文件。连接检查验证版本、连接、字符集和专用空 schema，不创建数据库，不接受任意 DSN 参数；完整 DDL/写入权限在安装执行时验证，连接检查通过不代表权限齐备。

```json
{
  "database": {
    "host": "mysql",
    "port": 3306,
    "name": "octodcp",
    "username": "octodcp",
    "password": "<由部署维护者输入>",
    "tls_mode": "disabled"
  },
  "site": { "site_name": "OctoDCP", "timezone": "Asia/Shanghai" },
  "founder": {
    "username": "raven",
    "display_name": "Raven",
    "password": "<由创始人设置的新密码>"
  }
}
```

注册默认关闭、会话默认 30 天；安装页面不提供其他业务功能。公开访问地址来自部署配置，不通过安装请求任意修改。数据库检查不持久化明文口令，最终安装请求重新携带；浏览器不在本地持久化口令。业务 JSON 体日志对这些路由禁用。

重复安装请求在完成后返回 409 `ALREADY_INSTALLED`，并发安装返回 409 `INSTALL_IN_PROGRESS`。本地条件在操作期间变化时返回 409 `INSTALLATION_STATE_CHANGED`，随后进入维护状态；非空数据库返回 409 `DATABASE_NOT_EMPTY`。最终安装在数据库命名锁内再次检查空库。中断恢复需要本地维护命令，不提供网页“一键重置”。没有安装会话接口或专用安装 Cookie。最终成功响应可携带普通 `octodcp_session` Cookie，`authenticated=true`；会话创建失败不撤销安装，返回 `authenticated=false` 并使用正常登录。仅查询 ready 或重访安装页绝不创建创始人会话；网络中断不重放安装或补发身份。

## 4. 公开信息与认证

| 方法与路径 | 请求 | 成功响应 |
| --- | --- | --- |
| `GET /public/settings` | 无 | 200：`{site_name,timezone,registration_enabled,session_ttl_days}`，用于访问页文案，不包含部署和数据库信息 |
| `GET /auth/csrf` | 无 | 200：`{csrf_token}`，可在安装前调用 |
| `POST /auth/login` | `{username,password}` | 200：AuthSession，替换现有会话 Cookie |
| `POST /auth/register` | `{username,display_name,password}` | 201：AuthSession，固定正常普通用户，无需审批；设置会话 Cookie，前端进入 `/home` |
| `GET /auth/session` | 用户 Cookie | 200：AuthSession；无有效会话为 401 |
| `POST /auth/logout` | 无 | 204；撤销当前会话并清除 Cookie，会话已失效也可退出 |

未知用户名、密码错误、禁用或已删除账号统一返回 401 `AUTH_INVALID_CREDENTIALS`，不泄露账号是否存在。无会话与过期会话返回 401 `UNAUTHENTICATED`。密码错误的验证路径使用虚拟哈希缓解明显时间差，不返回密码策略细节。

注册开关安装后默认为 false。注册在事务中重新检查该开关，关闭时返回 409 `REGISTRATION_CLOSED`；用户、凭据、会话与审计原子创建，不设置待审批状态。注册成功自动登录，前端重新获取 CSRF 后进入首页。关闭注册不影响已有账号，`POST /admin/users` 不受该开关限制。用户名冲突返回 409 `USERNAME_TAKEN`；请求包含 `role`、`is_founder`、邮箱或其他未定义字段时为 422。退出不要求有效用户身份，但仍需有效 CSRF。

## 5. 当前账号与头像

| 方法与路径 | 请求 | 成功响应 |
| --- | --- | --- |
| `PATCH /account` | If-Match；`{display_name}` | 200：User + ETag |
| `PUT /account/password` | `{current_password,new_password}` | 204；撤销全部会话、清除 Cookie，重新登录 |
| `POST /avatar-uploads` | multipart `file` | 201：`{id,preview_url,expires_at}` |
| `DELETE /avatar-uploads/{id}` | 上传者的 staged 草稿 | 204；非本人或不存在为 404，已绑定/孤立状态为 409 |
| `GET /avatars/{id}` | 头像访问权限 | 200：图像流，或满足条件时 304，无 JSON 外壳 |

本人改密时当前密码错误为 422 `CURRENT_PASSWORD_INVALID`；新旧相同为 422 `PASSWORD_UNCHANGED`，按字段返回错误码。用户名在个人接口不可写；管理员修改自己用户名使用控制台资料接口，创始人同样只能本人操作。

头像上传/访问端点及下述绑定规则尚未实现。后续接入 `avatar_upload_id` 可选字段；当前提交该字段按未知字段拒绝。

上传源允许 JPEG、PNG、WebP，文件大小至多 2 MiB。HTTP multipart 总大小至多 3 MiB，超过返回 413；来源图像最多 4096×4096、16,777,216 像素。服务端重新解码并统一生成 256×256 PNG，不接收 SVG、GIF 或任意附件，不相信扩展名。无效图像为 422，格式不支持为 415。

绑定必须是当前操作者上传的 staged 草稿，且尚未过期；失败为 409 `AVATAR_UPLOAD_UNAVAILABLE`，不泄露其他上传者信息。管理员创建/编辑他人时也先通过同一个上传接口取得属于自己的草稿。移除或替换头像不会立即删除旧文件，详情见 [生命周期](database.md)。

头像 GET 只允许登录用户访问已绑定且目标账号未删除的头像；未绑定草稿仅上传者可读，过期、orphaned 或不可见时返回 404。强制改密会话仅能读取自己的当前头像。响应 `Content-Type: image/png`、`X-Content-Type-Options: nosniff`、`Cache-Control: private, no-cache`，ETag 使用内容摘要；每次条件请求先鉴权再判断 304，不暴露文件系统路径。

## 6. 控制台用户管理

全部要求当前系统管理员身份，不能只依靠前端隐藏菜单。创始人和自身保护规则见 [权限矩阵](domain-model.md)。每个敏感写事务重新验证操作者身份、目标保护与会话版本。

| 方法与路径 | 请求 | 成功响应 |
| --- | --- | --- |
| `GET /admin/overview` | 无 | 200：下方概览 DTO |
| `GET /admin/users` | 筛选和分页 | 200：User 数组 + pagination |
| `GET /admin/users/{id}` | 无 | 200：User + ETag；已删除账号 404 |
| `POST /admin/users` | `{username,display_name,role,password}` | 201：User + ETag + Location；强制下次改密 |
| `PATCH /admin/users/{id}` | If-Match；`{username?,display_name?}` | 200：User + ETag；至少一项 |
| `PUT /admin/users/{id}/role` | If-Match；`{role}` | 200：User + ETag；撤销目标全部会话 |
| `PUT /admin/users/{id}/status` | If-Match；`{status}` | 200：User + ETag；启用/禁用均使既有会话失效 |
| `PUT /admin/users/{id}/password` | If-Match；`{new_password}` | 204；撤销目标全部会话并强制下次改密 |
| `DELETE /admin/users/{id}` | If-Match；`X-Confirm-Username` 等于当前用户名 | 204；逻辑删除、移除凭据、撤销会话、解绑头像 |

删除确认头仅防止界面误选，不代替授权；不匹配为 422 `CONFIRMATION_MISMATCH`。重复删除已删除用户返回 404。系统保留其 ID 与用户名，不提供恢复或复用 API。

不能代改创始人资料、头像或密码，不能改变创始人角色/状态或删除他，返回 403 `FOUNDER_PROTECTED`。不能禁用、删除或降级自己，返回 403 `SELF_PROTECTED`。通过控制台重置自己的密码返回 403 `SELF_PASSWORD_CHANGE_REQUIRED`，需走验证当前密码的本人改密流程。

### 列表与概览

`GET /admin/users` 参数：`page` 默认 1、`page_size` 默认 20 最大 100；`q` 最多 64 码点，匹配用户名或显示名称；可选 `status=active|disabled`、`identity=founder|admin|user`。`identity=admin` 特指非创始人的管理员，避免筛选含义重叠。不提供邮箱搜索。对搜索模式中的 `%`、`_` 做字面转义；一期使用受限 LIKE，不承诺大规模全文检索。

固定排序 `created_at DESC, id DESC`，排除已删除。分页外壳：

```json
{ "data": [], "pagination": { "page": 1, "page_size": 20, "total": 0 } }
```

概览返回 `{users_total,users_active,users_disabled,admins_total,registration_enabled,session_ttl_days,recent_events}`。数量排除已删除；管理员数包含创始人与已禁用管理员。`recent_events` 为最近 5 条 AuditEvent。这里不提供运行状态卡片，版本与数据库等信息留在系统设置。

## 7. 系统设置

| 方法与路径 | 请求 | 成功响应 |
| --- | --- | --- |
| `GET /admin/settings` | 无 | 200：SystemSettings + ETag |
| `PATCH /admin/settings` | If-Match；四个业务字段的非空子集 | 200：SystemSettings + ETag |
| `GET /admin/system-info` | 无 | 200：`{version,data_directory,database_type,master_key_initialized}` |
| `POST /admin/system-info/database-check` | 空 JSON 对象 | 200：`{reachable:true}`；仅检查当前配置连接，失败 503 |

```json
{
  "data": {
    "site_name": "OctoDCP",
    "timezone": "Asia/Shanghai",
    "registration_enabled": false,
    "session_ttl_days": 30,
    "version": "b47e3a9c1d2f8056a1b2c3d4e5f60718293a4b5c6d7e8f90123456789abcdef0",
    "deployment": { "public_url": "https://octodcp.example.com" }
  }
}
```

可写字段只有 `site_name`、`timezone`、`registration_enabled`、`session_ttl_days`，接口不暴露存储 KV 的任意 name/value 写入。字段映射到注册表中的四个配置名，类型和范围保持不变；`deployment.public_url` 只读，提交该字段返回 422。

设置修改在事务中按 name 顺序锁定当前设置视图涉及的全部配置，重新生成聚合版本并校验 If-Match，再原子更新发生变化的配置及审计。关闭注册与进行中的注册通过 `auth.registration_enabled` 同一记录排序；注册若已先提交，则账号有效，不能事后悄悄删除。会话有效期设置只影响新会话。设置 GET 的四个值与版本来自同一个一致快照，不能分次读取后拼接。

系统信息只供管理员查看，不返回数据库用户名、口令、DSN、主密钥内容/指纹、完整配置或环境变量。连接检测设短超时，不允许安装完成后从控制台测试任意主机。

## 8. 审计查询

本节是待实现契约；当前概览已提供最近五项真实事件，尚未开放列表和详情路由。

| 方法与路径 | 参数 / 响应 |
| --- | --- |
| `GET /admin/audit-events` | `cursor?`、`limit` 默认 20 最大 100、`action_prefix?`、`actor_user_id?`、`target_type?`、`target_id?`、`from?`、`to?`；200 列表 |
| `GET /admin/audit-events/{id}` | 200 单个 AuditEvent；不存在 404 |

`from` 包含、`to` 不包含，必须为有效 RFC 3339；固定按 `occurred_at DESC,id DESC`。游标为不透明的编码时间+ID，校验格式；筛选变化时客户端清空游标。响应：`{"data":[],"pagination":{"next_cursor":null,"has_more":false}}`，不计算总数。没有创建、修改或删除审计的 API。

AuditEvent 字段：`id`、`actor:{type,user_id,username,display_name}`、`action`、`target:{type,id,label}`、`occurred_at`、`request_id`、`ip_address`、`detail`。系统事件的 user_id/username 可为空，展示名称可为“部署维护者”。详情始终经过服务端白名单生成。

一期动作集合：`installation.completed`、`user.registered`、`user.created`、`auth.login_succeeded`、`auth.logged_out`、`user.profile_updated`、`user.avatar_changed`、`user.password_changed`、`user.password_reset`、`user.password_recovered`、`user.role_changed`、`user.disabled`、`user.enabled`、`user.deleted`、`settings.updated`。一次资料修改同时改变头像时可以在同一事务写两种对应事件。草稿上传/删除不单独记业务审计；失败登录记限流后的安全日志。

## 9. 错误状态

| HTTP | 典型 code | 处理 |
| --- | --- | --- |
| 401 | `UNAUTHENTICATED`、`AUTH_INVALID_CREDENTIALS` | 清理登录视图或提示统一登录错误 |
| 403 | `FORBIDDEN`、`CSRF_INVALID`、`FOUNDER_PROTECTED`、`SELF_PROTECTED`、`PASSWORD_CHANGE_REQUIRED` | 权限/保护限制；CSRF 失效可重新获取后由用户重试 |
| 404 | `NOT_FOUND` | 资源不存在或不应被当前用户发现 |
| 409 | `USERNAME_TAKEN`、`REGISTRATION_CLOSED`、`AVATAR_UPLOAD_UNAVAILABLE`、安装状态冲突 | 根据业务状态刷新或修正输入 |
| 412 / 428 | `VERSION_CONFLICT` / `PRECONDITION_REQUIRED` | 重新读取版本 / 补充前置条件 |
| 413 / 415 | `PAYLOAD_TOO_LARGE` / `UNSUPPORTED_MEDIA_TYPE` | 调整上传文件或内容类型 |
| 422 | `VALIDATION_FAILED` 等 | 根据 details 中的 field 定位控件，按 code 显示输入错误 |
| 429 | `RATE_LIMITED` | 按 Retry-After 等待 |
| 500 | `INTERNAL_ERROR` | 展示请求 ID；详细原因只写脱敏服务日志 |
| 503 | `INSTANCE_NOT_READY`、`DATABASE_UNAVAILABLE` | 显示暂不可用；不打开安装或误清空系统 |

HTTP 解析失败使用 400 `BAD_REQUEST`，不暴露解析器内部信息。权限检查、并发事务和状态码需要在正式实现中用集成测试验证，本稿不是自动生成的 OpenAPI 文件；待规则确认后从同一 DTO 契约补充 OpenAPI，避免同时维护两套草案。
