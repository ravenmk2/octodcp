export interface ErrorDetail {
  code: string;
  field?: string;
  context?: Record<string, unknown>;
}

export class ApiError extends Error {
  constructor(
    public code: string,
    message: string,
    public details: ErrorDetail[] = [],
    public requestId?: string,
  ) {
    super(message);
  }
}

let csrfToken = "";
let authFailure: ((code: string) => void) | undefined;
export function onAuthFailure(handler: (code: string) => void) {
  authFailure = handler;
}

export interface RequestOptions {
  method?: "GET" | "POST" | "PATCH" | "PUT" | "DELETE";
  body?: unknown;
  etag?: string;
  headers?: Record<string, string>;
}
export interface ApiResponse<T> {
  data: T;
  etag: string;
  pagination?: { page?: number; page_size?: number; total?: number };
}

// No credentials or installation inputs are persisted in browser storage.
export async function api<T>(path: string, body?: unknown): Promise<T> {
  return (
    await request<T>(path, {
      method: body === undefined ? "GET" : "POST",
      body,
    })
  ).data;
}

export async function request<T>(
  path: string,
  options: RequestOptions = {},
): Promise<ApiResponse<T>> {
  const method = options.method ?? "GET";
  const write = method !== "GET";
  if (write && !csrfToken) {
    csrfToken = (await api<{ csrf_token: string }>("/auth/csrf")).csrf_token;
  }
  let response: Response;
  try {
    response = await fetch(`/api/v1${path}`, {
      method,
      credentials: "same-origin",
      cache: "no-store",
      headers: {
        ...(write
          ? { "Content-Type": "application/json", "X-CSRF-Token": csrfToken }
          : {}),
        ...(options.etag ? { "If-Match": options.etag } : {}),
        ...options.headers,
      },
      body:
        options.body === undefined ? undefined : JSON.stringify(options.body),
    });
  } catch {
    throw new ApiError("NETWORK_ERROR", "无法连接服务，请检查网络后重试。");
  }
  const payload = await response.json().catch(() => null);
  if (!response.ok) {
    if (payload?.error?.code === "CSRF_INVALID") csrfToken = "";
    if (
      path !== "/auth/session" &&
      ["UNAUTHENTICATED", "PASSWORD_CHANGE_REQUIRED"].includes(
        payload?.error?.code,
      )
    )
      authFailure?.(payload.error.code);
    throw new ApiError(
      payload?.error?.code ?? "HTTP_ERROR",
      payload?.error?.message ?? "服务返回异常，请稍后重试。",
      payload?.error?.details ?? [],
      payload?.request_id,
    );
  }
  if (response.status === 204) return { data: undefined as T, etag: "" };
  if (!payload || !("data" in payload))
    throw new ApiError("INVALID_RESPONSE", "服务响应格式不正确。");
  return {
    data: payload.data as T,
    etag: response.headers.get("ETag") ?? "",
    pagination: payload.pagination,
  };
}

export function resetCSRF() {
  csrfToken = "";
}
