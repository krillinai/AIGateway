import { useQuery } from "@tanstack/react-query";
import { getModelActivityOverview, getModelRequestDetail, getModelRequests, type ModelActivityFilter, type ModelRequestCursor } from "@/lib/model-activity-api";

const options = { refetchOnWindowFocus: false, refetchOnReconnect: false } as const;
export function useModelActivityOverview(filter: ModelActivityFilter, page: number) {
  return useQuery({ queryKey: ["model-activity", "overview", filter, page], queryFn: () => getModelActivityOverview(filter, page), ...options });
}
export function useModelRequests(filter: ModelActivityFilter, cursor?: ModelRequestCursor) {
  return useQuery({ queryKey: ["model-activity", "requests", filter, cursor], queryFn: () => getModelRequests(filter, cursor), ...options });
}
export function useModelRequestDetail(auditId: string) {
  return useQuery({ queryKey: ["model-activity", "detail", auditId], queryFn: () => getModelRequestDetail(auditId), enabled: Boolean(auditId), ...options });
}
