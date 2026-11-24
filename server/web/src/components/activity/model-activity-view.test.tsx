import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { beforeEach, expect, it, vi } from "vitest";
import { ModelActivityView } from "./model-activity-view";
import { getModelActivityOverview, getModelRequestDetail, getModelRequests, type ModelActivityOverview, type ModelRequestPreview } from "@/lib/model-activity-api";

vi.mock("@/lib/model-activity-api", () => ({ getModelActivityOverview: vi.fn(), getModelRequests: vi.fn(), getModelRequestDetail: vi.fn() }));
const stats = { request_count: 3, session_count: 1, turn_count: 1, total_tokens: null, success_rate: null, avg_duration_ms: null, last_request_at: null, processing_count: 0, stale_started_count: 0, usage_complete_count: 0, usage_partial_count: 0, usage_unavailable_count: 3 };
const meta = { source: "model_proxy" as const, schema_version: 1 as const, range: "7d", timezone: "Asia/Shanghai", start: "2026-11-14T00:00:00+08:00", end: "2026-11-21T00:00:00+08:00" };
const overview: ModelActivityOverview = { summary: { ...stats, active_users: 1, active_agents: 1 }, agents: [{ ...stats, user_id: "user", user_name: "张三", agent_id: "agent", agent_name: "工作助手" }], meta: { ...meta, page: 1, page_size: 20, total: 21 } };
const request: ModelRequestPreview = { audit_id: "audit", user_id: "user", user_name: "张三", agent_id: "agent", agent_name: "工作助手", session_id: "session", turn_id: "turn", model: "model", created_at: new Date(Date.now() - 18 * 60_000).toISOString(), completed_at: null, status: "started", error_code: "", total_tokens: null, usage_status: "unavailable", duration_ms: null, input_preview: "输入预览", output_preview: "输出预览", input_status: "available", output_status: "partial", input_truncated: false, output_truncated: true };
function show(path = "/admin/activity", detail = false) {
  return render(<QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}><MemoryRouter initialEntries={[path]}><ModelActivityView detail={detail} /></MemoryRouter></QueryClientProvider>);
}
beforeEach(() => {
  vi.resetAllMocks();
  vi.mocked(getModelActivityOverview).mockResolvedValue(overview);
  vi.mocked(getModelRequests).mockResolvedValue({ items: [request], meta: { ...meta, limit: 20, next_created_at: request.created_at, next_id: request.audit_id } });
  vi.mocked(getModelRequestDetail).mockResolvedValue({ ...request, activity_content: { schema_version: 1, input_text: "<script>输入</script>", output_text: "已取得的输出", input_status: "available", output_status: "partial", input_truncated: false, output_truncated: true, tool_calls: [{ type: "custom_tool_call", name: "exec", call_id: "call_1", input_text: "{\"cmd\":\"pwd\"}" }] }, meta });
});
it("展示模型口径、缺失用量和兼容入口，分页与刷新按需查询", async () => {
  show();
  expect(await screen.findByText("工作助手")).toBeInTheDocument();
  expect(screen.getByText(/仅统计经过 Gateway/)).toBeInTheDocument();
  expect(screen.getByText(/用量部分可用 0 次，缺失 3 次/)).toBeInTheDocument();
  expect(screen.getByRole("link", { name: "客户端上报记录" })).toHaveAttribute("href", "/admin/activity?source=reported");
  expect(screen.getByRole("link", { name: "查看调用" })).toHaveAttribute("href", expect.stringContaining("source=model_proxy"));
  fireEvent.click(screen.getByRole("button", { name: "下一页" }));
  await waitFor(() => expect(getModelActivityOverview).toHaveBeenLastCalledWith(expect.anything(), 2));
  await waitFor(() => expect(screen.getByRole("button", { name: "刷新" })).toBeEnabled());
  fireEvent.click(screen.getByRole("button", { name: "刷新" }));
  await waitFor(() => expect(getModelActivityOverview).toHaveBeenCalledTimes(3));
});
it("区分空结果与接口失败", async () => {
  vi.mocked(getModelActivityOverview).mockResolvedValueOnce({ ...overview, agents: [], meta: { ...overview.meta, total: 0 } });
  const view = show();
  expect(await screen.findByText("当前范围无模型调用")).toBeInTheDocument();
  view.unmount();
  vi.mocked(getModelActivityOverview).mockRejectedValue(new Error("模型活动暂不可用"));
  show();
  expect(await screen.findByText("模型活动加载失败")).toBeInTheDocument();
  expect(screen.queryByText("当前范围无模型调用")).not.toBeInTheDocument();
});
it("游标翻页、精确筛选重置游标，详情只使用审计ID且纯文本渲染", async () => {
  show("/admin/activity/detail?source=model_proxy&user_id=user&agent_id=agent", true);
  expect(await screen.findByText("输入预览")).toBeInTheDocument();
  expect(screen.getByText("状态待核对")).toBeInTheDocument();
  fireEvent.click(screen.getByRole("button", { name: "下一页" }));
  await waitFor(() => expect(getModelRequests).toHaveBeenLastCalledWith(expect.anything(), { cursor_created_at: request.created_at, cursor_id: request.audit_id }));
  fireEvent.change(screen.getByLabelText("会话 ID"), { target: { value: "session" } });
  fireEvent.change(screen.getByLabelText("轮次 ID"), { target: { value: "turn" } });
  fireEvent.click(screen.getByRole("button", { name: "精确筛选" }));
  await waitFor(() => expect(getModelRequests).toHaveBeenLastCalledWith(expect.objectContaining({ user_id: "user", agent_id: "agent", session_id: "session", turn_id: "turn" }), undefined));
  fireEvent.click(await screen.findByRole("button", { name: "查看内容" }));
  expect(await screen.findByText("<script>输入</script>")).toBeInTheDocument();
  expect(getModelRequestDetail).toHaveBeenLastCalledWith("audit");
  expect(document.querySelector("script")).toBeNull();
  expect(screen.getByText(/部分输出：/)).toBeInTheDocument();
  expect(screen.getByText(/最多保存 8000/)).toBeInTheDocument();
  expect(screen.getByText("工具调用（1）")).toBeInTheDocument();
  expect(screen.getByText("exec")).toBeInTheDocument();
});
it("按会话归并轮次并支持分层收起", async () => {
  const secondTurn = { ...request, audit_id: "audit-2", turn_id: "turn-2", input_preview: "第二轮请求", output_preview: "第二轮响应", created_at: new Date(Date.now() - 12 * 60_000).toISOString() };
  const secondSession = { ...request, audit_id: "audit-3", session_id: "session-2", turn_id: "turn-3", input_preview: "另一个会话请求", output_preview: "另一个会话响应" };
  vi.mocked(getModelRequests).mockResolvedValueOnce({ items: [request, secondTurn, secondSession], meta: { ...meta, limit: 20, next_created_at: null, next_id: "" } });

  show("/admin/activity/detail?source=model_proxy&user_id=user&agent_id=agent", true);

  expect(await screen.findByText("2 个轮次")).toBeInTheDocument();
  expect(screen.getByText("当前页 3 次请求，已归并为 2 个会话")).toBeInTheDocument();
  const secondTurnToggle = screen.getByRole("button", { name: /轮次 turn-2/ });
  fireEvent.click(secondTurnToggle);
  expect(await screen.findByText("第二轮请求")).toBeInTheDocument();

  fireEvent.click(screen.getByRole("button", { name: /会话 session，2 个轮次/ }));
  expect(screen.queryByText("第二轮请求")).not.toBeInTheDocument();
});
it("未关联调用不能按会话或轮次混合筛选", async () => {
  show("/admin/activity/detail?source=model_proxy&user_id=user&unattributed=true", true);
  await screen.findByText("输入预览");
  expect(screen.getByLabelText("会话 ID")).toBeDisabled();
  expect(screen.getByRole("button", { name: "精确筛选" })).toBeDisabled();
});
it("列表摘要标识没有文本输出的工具调用", async () => {
  vi.mocked(getModelRequests).mockResolvedValueOnce({ items: [{ ...request, output_preview: "", tool_call_count: 1 }], meta: { ...meta, limit: 20, next_created_at: null, next_id: "" } });
  show("/admin/activity/detail?source=model_proxy&user_id=user&agent_id=agent", true);
  expect(await screen.findByText("本次包含 1 次工具调用")).toBeInTheDocument();
});
