import { adminApi } from "./api";

export type ModelActivityContent = {
  schema_version: 1;
  input_text: string;
  output_text: string;
  input_status: "available" | "unavailable";
  output_status: "pending" | "available" | "partial" | "unavailable";
  input_truncated: boolean;
  output_truncated: boolean;
};
export type ModelActivityStats = {
  request_count: number; session_count: number; turn_count: number; total_tokens: number | null;
  success_rate: number | null; avg_duration_ms: number | null; last_request_at: string | null;
  processing_count: number; stale_started_count: number;
  usage_complete_count: number; usage_partial_count: number; usage_unavailable_count: number;
};
export type ModelActivityAgent = ModelActivityStats & { user_id: string; user_name: string; agent_id: string; agent_name: string };
export type ModelActivityFilter = { range: string; user_id?: string; agent_id?: string; unattributed?: boolean; session_id?: string; turn_id?: string };
export type ModelActivityMeta = { source: "model_proxy"; schema_version: 1; range: string; timezone: string; start: string; end: string };
export type ModelActivityOverview = {
  summary: ModelActivityStats & { active_users: number; active_agents: number };
  agents: ModelActivityAgent[];
  meta: ModelActivityMeta & { page: number; page_size: number; total: number };
};
export type ModelRequest = {
  audit_id: string; user_id: string; user_name: string; agent_id: string; agent_name: string;
  session_id: string; turn_id: string; model: string; created_at: string; completed_at: string | null;
  status: string; error_code: string; total_tokens: number | null;
  usage_status: "complete" | "partial" | "unavailable"; duration_ms: number | null;
};
export type ModelRequestPreview = ModelRequest & Omit<ModelActivityContent, "schema_version" | "input_text" | "output_text"> & { input_preview: string; output_preview: string };
export type ModelRequestDetail = ModelRequest & { activity_content: ModelActivityContent; meta: { source: "model_proxy"; schema_version: 1 } };
export type ModelRequestCursor = { cursor_created_at: string; cursor_id: string };
export type ModelRequests = { items: ModelRequestPreview[]; meta: ModelActivityMeta & { limit: number; next_created_at: string | null; next_id: string } };

function query(params: object) {
  const search = new URLSearchParams();
  Object.entries(params).forEach(([key, value]) => { if (value !== undefined && value !== "" && value !== false) search.set(key, String(value)); });
  return search.toString();
}
export function getModelActivityOverview(filter: ModelActivityFilter, page = 1) {
  return adminApi.get<ModelActivityOverview>(`/activity/model-overview?${query({ ...filter, page, page_size: 20 })}`);
}
export function getModelRequests(filter: ModelActivityFilter, cursor?: ModelRequestCursor) {
  return adminApi.get<ModelRequests>(`/activity/model-requests?${query({ ...filter, ...cursor, limit: 20 })}`);
}
export function getModelRequestDetail(auditId: string) {
  return adminApi.get<ModelRequestDetail>(`/activity/model-requests/detail?${query({ audit_id: auditId })}`);
}
