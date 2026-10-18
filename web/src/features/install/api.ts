import { api } from "../../shared/api";

export type InstallState =
  | "setup_required"
  | "initializing"
  | "ready"
  | "recovery_required"
  | "unavailable";
export interface Status {
  state: InstallState;
  public_url: string;
}
export interface Database {
  host: string;
  port: number;
  name: string;
  username: string;
  password: string;
  tls_mode: "disabled" | "verify_identity";
}
export interface InstallRequest {
  database: Database;
  site: { site_name: string; timezone: string };
  founder: { username: string; display_name: string; password: string };
}

export const getStatus = () => api<Status>("/setup/status");
export const checkDatabase = (database: Database) =>
  api("/setup/database-check", database);
export const install = (request: InstallRequest) =>
  api<{ state: "ready"; installation_id: string; authenticated: boolean }>(
    "/setup/install",
    request,
  );
