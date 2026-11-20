import { useSearchParams } from "react-router-dom";

import { ModelActivityView } from "@/components/activity/model-activity-view";

import { ActivityDetailView } from "@/components/activity-detail/activity-detail-view";

export function OfficeAgentDetailPage() {
  const [searchParams] = useSearchParams();
  const collectorId = searchParams.get("collector_id") ?? "";
  const agentId = searchParams.get("agent_id") ?? "";

  if (searchParams.get("source") === "model_proxy") return <ModelActivityView detail />;

  return <ActivityDetailView agentId={agentId} collectorId={collectorId} />;
}
