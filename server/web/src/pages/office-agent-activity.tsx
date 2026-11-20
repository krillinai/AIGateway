import { Link, useSearchParams } from "react-router-dom";
import { ActivityDashboardView } from "@/components/activity/activity-dashboard-view";
import { ModelActivityView } from "@/components/activity/model-activity-view";
import { Button } from "@/components/ui/button";

export function OfficeAgentActivityPage() {
  const [params] = useSearchParams();
  if (params.get("source") === "reported") return <ActivityDashboardView navigation={<div className="mb-4 flex items-center gap-3"><span>数据来源：客户端上报记录</span><Button asChild variant="outline"><Link to="/admin/activity">模型活动</Link></Button></div>} />;
  return <ModelActivityView />;
}
