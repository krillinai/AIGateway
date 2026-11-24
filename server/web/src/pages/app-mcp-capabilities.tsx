import { useQuery } from "@tanstack/react-query";

import {
  DataTableShell,
  EmptyState,
  ErrorAlert,
  LoadingState,
  PageHeader,
  PageShell,
  TableStateRow
} from "@/components/governance-ui";
import { Badge } from "@/components/ui/badge";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { listMyAgents, listMyMCPCatalog, type AgentAccess, type AgentMCPUpstream } from "@/lib/agent-api";
import { agentCreationSourceLabel, agentDisplayId } from "@/lib/agent-source";
import { mcpStatusLabel } from "@/lib/mcp-admin-ui";

export function AppMCPCapabilitiesPage() {
  const agentsQuery = useQuery({ queryKey: ["app-agents"], queryFn: listMyAgents });
  const agents = agentsQuery.data ?? [];
  const catalogQuery = useQuery({
    queryKey: ["app-mcp-catalog"],
    queryFn: () => listMyMCPCatalog(),
    enabled: agents.length > 0
  });

  return (
    <PageShell>
      <PageHeader title="MCP 能力目录">当前账户已授权且可用的 MCP 能力，账户下所有 active Agent 继承相同能力。</PageHeader>

      {agentsQuery.isLoading || catalogQuery.isLoading ? (
        <LoadingState label="正在加载 MCP 能力目录" />
      ) : null}
      {agentsQuery.isError || catalogQuery.isError ? (
        <ErrorAlert error={[agentsQuery.error, catalogQuery.error]}>MCP 能力目录加载失败</ErrorAlert>
      ) : null}
      {!agentsQuery.isLoading && !agentsQuery.isError && agents.length > 0 ? <AgentScope agents={agents} /> : null}
      {!agentsQuery.isLoading && !agentsQuery.isError && agents.length === 0 ? (
        <EmptyState title="暂无可用 Agent" />
      ) : null}
      {!catalogQuery.isLoading && !catalogQuery.isError && catalogQuery.data?.upstreams.length === 0 ? (
        <EmptyState title="当前账户暂无已授权 MCP 能力" />
      ) : null}
      {catalogQuery.data?.upstreams.map((upstream) => (
        <UpstreamCatalog key={upstream.id} upstream={upstream} />
      ))}
    </PageShell>
  );
}

function AgentScope({ agents }: { agents: AgentAccess[] }) {
  return (
    <Card>
      <CardHeader>
        <CardTitle>可使用的 Agent</CardTitle>
        <CardDescription>当前账户下的 Agent 共用以下 MCP 授权，外部客户端按最近模型调用记录展示。</CardDescription>
      </CardHeader>
      <CardContent className="overflow-auto">
        <DataTableShell dense embedded fixedLayout minWidth={680}>
          <TableHeader>
            <TableRow>
              <TableHead>Agent ID</TableHead>
              <TableHead>名称</TableHead>
              <TableHead>来源</TableHead>
              <TableHead>状态</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {agents.map(({ agent }) => (
              <TableRow key={agent.agentId}>
                <TableCell className="break-all font-mono text-xs">{agentDisplayId(agent)}</TableCell>
                <TableCell>{agent.name || agent.clientId || "-"}</TableCell>
                <TableCell>
                  <div className="grid gap-1">
                    <Badge variant="outline">{agentCreationSourceLabel(agent.creationSource)}</Badge>
                    {agent.external && agent.activityRequestCount !== undefined ? <span className="text-xs text-muted-foreground">模型调用 {agent.activityRequestCount} 次</span> : null}
                  </div>
                </TableCell>
                <TableCell><Badge variant={agent.status === "active" ? "success" : "muted"}>{mcpStatusLabel(agent.status)}</Badge></TableCell>
              </TableRow>
            ))}
          </TableBody>
        </DataTableShell>
      </CardContent>
    </Card>
  );
}

function UpstreamCatalog({ upstream }: { upstream: AgentMCPUpstream }) {
  return (
    <Card className="shadow-none">
      <CardHeader className="flex flex-col gap-2 sm:flex-row sm:items-start sm:justify-between">
        <div className="flex min-w-0 items-center gap-3">
          <img
            alt={`${upstream.name} 图标`}
            className="size-10 shrink-0 rounded-md border border-border object-cover"
            height={40}
            src={upstream.iconUrl}
            width={40}
          />
          <div className="flex min-w-0 flex-col gap-1.5">
            <CardTitle><h2>{upstream.name}</h2></CardTitle>
            <CardDescription className="break-all font-mono">{upstream.mcpEndpoint}</CardDescription>
          </div>
        </div>
        <div className="flex flex-wrap items-center gap-2">
          {upstream.domain ? <Badge variant="outline">{upstream.domain}</Badge> : null}
          <Badge variant="outline">{upstream.upstreamTransport}</Badge>
          <Badge variant={upstream.status === "active" ? "success" : "muted"}>{mcpStatusLabel(upstream.status)}</Badge>
          <Badge variant="secondary">{upstream.tools.length} 个 Tool</Badge>
        </div>
      </CardHeader>
      <CardContent className="overflow-auto">
        <DataTableShell dense embedded minWidth={620}>
          <TableHeader>
            <TableRow>
              <TableHead>Tool</TableHead>
              <TableHead>说明</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {upstream.tools.length === 0 ? (
              <TableStateRow colSpan={2}><EmptyState title="暂无 Tool" /></TableStateRow>
            ) : upstream.tools.map((tool) => (
              <TableRow key={tool.id}>
                <TableCell>
                  <div className="flex min-w-0 flex-col gap-1">
                    <strong>{tool.title || tool.name}</strong>
                    <code className="break-all text-xs text-muted-foreground">{tool.name}</code>
                    {tool.exposedName !== tool.name ? <code className="break-all text-xs text-muted-foreground">{tool.exposedName}</code> : null}
                  </div>
                </TableCell>
                <TableCell className="max-w-96 text-muted-foreground">{tool.description || "-"}</TableCell>
              </TableRow>
            ))}
          </TableBody>
        </DataTableShell>
      </CardContent>
    </Card>
  );
}
