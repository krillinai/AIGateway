import { beforeEach, expect, it, vi } from "vitest";
import { adminApi } from "./api";
import { getModelActivityOverview, getModelRequestDetail, getModelRequests } from "./model-activity-api";
vi.mock("./api", () => ({ adminApi: { get: vi.fn() } }));
beforeEach(() => vi.clearAllMocks());
it("总览与请求使用独立活动接口及完整游标，详情仅传审计ID", async () => {
  await getModelActivityOverview({ range: "7d", user_id: "u", unattributed: true }, 2);
  expect(adminApi.get).toHaveBeenLastCalledWith("/activity/model-overview?range=7d&user_id=u&unattributed=true&page=2&page_size=20");
  await getModelRequests({ range: "today", user_id: "u", agent_id: "a", session_id: "s", turn_id: "t" }, { cursor_created_at: "2026-11-20T00:00:00+08:00", cursor_id: "one" });
  const url = vi.mocked(adminApi.get).mock.calls.at(-1)?.[0];
  expect(new URLSearchParams(url?.split("?")[1]).get("cursor_created_at")).toBe("2026-11-20T00:00:00+08:00");
  await getModelRequestDetail("audit with space");
  expect(adminApi.get).toHaveBeenLastCalledWith("/activity/model-requests/detail?audit_id=audit+with+space");
});
