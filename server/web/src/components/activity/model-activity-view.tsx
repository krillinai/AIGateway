import { useMemo, useState, type FormEvent } from "react";
import { Link, useSearchParams } from "react-router-dom";
import { ChevronDown, Layers3, MessageSquareText } from "lucide-react";
import { DataTableShell, DetailDrawer, ErrorAlert, MetricCard, PageHeader, PageShell, TableStateRow } from "@/components/governance-ui";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from "@/components/ui/collapsible";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { useModelActivityOverview, useModelRequestDetail, useModelRequests } from "@/hooks/useModelActivity";
import type { ModelActivityContent, ModelActivityFilter, ModelActivityStats, ModelRequest, ModelRequestCursor, ModelRequestPreview } from "@/lib/model-activity-api";

const count = (value: number | null | undefined) => value == null ? "--" : value.toLocaleString();
const date = (value: string | null) => value ? new Date(value).toLocaleString() : "--";
const rate = (value: number | null | undefined) => value == null ? "--" : `${(value * 100).toFixed(1)}%`;
const scopeNotice = "仅统计经过 Gateway 的模型调用；最近调用不代表客户端在线，请求结束不代表本地任务完成。";
const turnNotice = "交互轮次按已关联的 Runtime 执行轮次统计，与本地运行数、用户消息数和客户端上报口径可能不同。";

function UsageNotice({ stats }: { stats?: ModelActivityStats }) {
  return stats && (stats.usage_partial_count > 0 || stats.usage_unavailable_count > 0)
    ? <p className="text-sm text-muted-foreground">用量部分可用 {stats.usage_partial_count} 次，缺失 {stats.usage_unavailable_count} 次；Token 仅累计已知用量。</p> : null;
}
function RangeSelect({ value, onChange }: { value: string; onChange(value: string): void }) {
  return <Select value={value} onValueChange={onChange}><SelectTrigger aria-label="时间范围" className="w-40"><SelectValue /></SelectTrigger><SelectContent><SelectItem value="today">今天</SelectItem><SelectItem value="7d">近 7 天</SelectItem><SelectItem value="30d">近 30 天</SelectItem></SelectContent></Select>;
}

export function ModelActivityView({ detail = false }: { detail?: boolean }) {
  const [params, setParams] = useSearchParams();
  const filter: ModelActivityFilter = { range: params.get("range") ?? "7d", user_id: params.get("user_id") ?? "", client_type: params.get("client_type") ?? "", agent_id: params.get("agent_id") ?? "", unattributed: params.get("unattributed") === "true", session_id: params.get("session_id") ?? "", turn_id: params.get("turn_id") ?? "" };
  return detail ? <ModelActivityRequests key={JSON.stringify(filter)} filter={filter} /> : <ModelActivityOverviewView key={JSON.stringify(filter)} filter={filter} onFilter={(next) => setParams({ source: "model_proxy", range: next.range, user_id: next.user_id ?? "", client_type: next.client_type ?? "", agent_id: next.agent_id ?? "", ...(next.unattributed ? { unattributed: "true" } : {}) })} />;
}

function ModelActivityOverviewView({ filter, onFilter }: { filter: ModelActivityFilter; onFilter(filter: ModelActivityFilter): void }) {
  const [page, setPage] = useState(1);
  const [user, setUser] = useState(filter.user_id ?? "");
  const [agent, setAgent] = useState(filter.agent_id ?? "");
  const query = useModelActivityOverview(filter, page);
  const summary = query.data?.summary;
  function submit(event: FormEvent) { event.preventDefault(); onFilter({ ...filter, user_id: user.trim(), agent_id: agent.trim() }); }
  return <PageShell>
    <PageHeader title="智能体活动" actions={<><Button variant="outline" onClick={() => void query.refetch()} disabled={query.isFetching}>刷新</Button><Button asChild variant="outline"><Link to="/admin/activity?source=reported">客户端上报记录</Link></Button></>}>{scopeNotice}</PageHeader>
    <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-5">
      <MetricCard label="请求数" value={count(summary?.request_count)} /><MetricCard label="活跃用户" value={count(summary?.active_users)} /><MetricCard label="活跃智能体" value={count(summary?.active_agents)} /><MetricCard label="总 Token" value={count(summary?.total_tokens)} /><MetricCard label="网关转发成功率" value={rate(summary?.success_rate)} />
    </div>
    <UsageNotice stats={summary} />
    <form onSubmit={submit} className="my-4 flex flex-wrap items-end gap-3">
      <RangeSelect value={filter.range} onChange={(range) => onFilter({ ...filter, range })} />
      <div><Label htmlFor="model-user">用户 ID</Label><Input id="model-user" value={user} onChange={(event) => setUser(event.target.value)} /></div>
      <div><Label htmlFor="model-agent">智能体 ID</Label><Input id="model-agent" value={agent} disabled={filter.unattributed} onChange={(event) => setAgent(event.target.value)} /></div>
      <Select value={filter.unattributed ? "unattributed" : "all"} onValueChange={(value) => onFilter({ ...filter, unattributed: value === "unattributed", agent_id: "" })}><SelectTrigger aria-label="关联状态" className="w-40"><SelectValue /></SelectTrigger><SelectContent><SelectItem value="all">全部智能体</SelectItem><SelectItem value="unattributed">未关联智能体</SelectItem></SelectContent></Select>
      <Button type="submit">筛选</Button>
    </form>
    {query.isError ? <ErrorAlert error={query.error}>模型活动加载失败</ErrorAlert> : null}
    <p className="my-3 text-sm text-muted-foreground">{turnNotice}</p>
    <DataTableShell><Table><TableHeader><TableRow>{["用户", "客户端 / 智能体", "请求数", "会话数", "交互轮次", "Token", "最近调用时间", "操作"].map((name) => <TableHead key={name}>{name}</TableHead>)}</TableRow></TableHeader><TableBody>
      {query.isLoading ? <TableStateRow colSpan={8}>加载中</TableStateRow> : !query.isError && query.data?.agents.length === 0 ? <TableStateRow colSpan={8}>当前范围无模型调用</TableStateRow> : query.data?.agents.map((agent) => {
        const search = new URLSearchParams({ source: "model_proxy", range: filter.range, user_id: agent.user_id, client_type: agent.client_type ?? "unknown", ...(agent.agent_id ? { agent_id: agent.agent_id } : { unattributed: "true" }) });
        return <TableRow key={`${agent.user_id}/${agent.client_type ?? "unknown"}/${agent.agent_id}`}><TableCell>{agent.user_name}</TableCell><TableCell>{agent.agent_name}</TableCell><TableCell>{agent.request_count}</TableCell><TableCell>{agent.session_count}</TableCell><TableCell>{agent.turn_count}</TableCell><TableCell>{count(agent.total_tokens)}</TableCell><TableCell>{date(agent.last_request_at)}</TableCell><TableCell><Button asChild variant="outline" size="sm"><Link to={`/admin/activity/detail?${search}`}>查看调用</Link></Button></TableCell></TableRow>;
      })}
    </TableBody></Table></DataTableShell>
    <div className="mt-3 flex items-center gap-3"><Button variant="outline" disabled={page === 1 || query.isFetching} onClick={() => setPage(page - 1)}>上一页</Button><span>第 {page} 页 · 共 {query.data?.meta.total ?? 0} 行</span><Button variant="outline" disabled={!query.data || page * query.data.meta.page_size >= query.data.meta.total || query.isFetching} onClick={() => setPage(page + 1)}>下一页</Button></div>
  </PageShell>;
}

function requestStatus(request: ModelRequest) {
  if (request.status === "started") return Date.now() - new Date(request.created_at).getTime() > 17 * 60_000 ? "状态待核对" : "请求处理中";
  return request.status === "succeeded" ? "转发成功" : request.status === "client_disconnected" ? "客户端断开" : "调用失败";
}
const failureHints: Record<string, string> = { model_group_unavailable: "模型组不可用", model_not_found: "模型不可用", upstream_timeout: "上游调用超时", upstream_error: "上游服务错误", upstream_rate_limited: "上游请求受限", client_disconnected: "客户端已断开连接", invalid_request: "模型请求无效" };

function ModelActivityRequests({ filter }: { filter: ModelActivityFilter }) {
  const [params, setParams] = useSearchParams();
  const [cursors, setCursors] = useState<Array<ModelRequestCursor | undefined>>([undefined]);
  const [session, setSession] = useState(filter.session_id ?? "");
  const [turn, setTurn] = useState(filter.turn_id ?? "");
  const query = useModelRequests(filter, cursors[cursors.length - 1]);
  const auditId = params.get("audit_id") ?? "";
  const detail = useModelRequestDetail(auditId);
  const canFilter = Boolean(filter.user_id && (filter.client_type || filter.agent_id));
  const sessionGroups = useMemo(() => groupRequestsBySession(query.data?.items ?? []), [query.data?.items]);
  function change(values: Record<string, string>) { const next = new URLSearchParams(params); Object.entries(values).forEach(([key, value]) => value ? next.set(key, value) : next.delete(key)); setParams(next); }
  function submit(event: FormEvent) { event.preventDefault(); change({ session_id: session.trim(), turn_id: session.trim() ? turn.trim() : "", audit_id: "" }); }
  return <PageShell>
    <PageHeader title="模型调用记录" actions={<><Button asChild variant="outline"><Link to="/admin/activity">返回活动</Link></Button><Button variant="outline" disabled={query.isFetching} onClick={() => { void query.refetch(); if (auditId) void detail.refetch(); }}>刷新</Button></>}>{scopeNotice}</PageHeader>
    <form onSubmit={submit} className="mb-4 flex flex-wrap items-end gap-3"><RangeSelect value={filter.range} onChange={(range) => change({ range })} /><div><Label htmlFor="model-session">会话 ID</Label><Input id="model-session" disabled={!canFilter} value={session} onChange={(event) => setSession(event.target.value)} /></div><div><Label htmlFor="model-turn">轮次 ID</Label><Input id="model-turn" disabled={!canFilter} value={turn} onChange={(event) => setTurn(event.target.value)} /></div><Button disabled={!canFilter} type="submit">精确筛选</Button><Button variant="outline" type="button" onClick={() => change({ session_id: "", turn_id: "", audit_id: "" })}>清除会话/轮次</Button></form>
    <p className="mb-3 text-sm text-muted-foreground">{turnNotice}</p>
    {query.isError ? <ErrorAlert error={query.error}>调用记录加载失败</ErrorAlert> : null}
    {query.isLoading ? <div className="rounded-md border border-border bg-card px-3 py-5 text-center text-sm text-muted-foreground">加载中</div> : null}
    {!query.isLoading && !query.isError && query.data?.items.length === 0 ? <div className="rounded-md border border-border bg-card px-3 py-5 text-center text-sm text-muted-foreground">当前范围无模型调用</div> : null}
    {!query.isLoading && !query.isError && query.data?.items.length ? <div className="grid min-w-0 gap-3" aria-label="按会话分组的模型调用">
      <div className="flex flex-wrap items-center gap-2 text-xs text-muted-foreground"><Layers3 className="size-4" aria-hidden="true" />当前页 {query.data.items.length} 次请求，已归并为 {sessionGroups.length} 个会话</div>
      {sessionGroups.map((group) => <SessionRequestGroup group={group} key={group.key} onOpen={(request) => change({ audit_id: request.audit_id })} />)}
    </div> : null}
    <div className="mt-3 flex items-center gap-3"><Button variant="outline" disabled={cursors.length === 1 || query.isFetching} onClick={() => setCursors(cursors.slice(0, -1))}>上一页</Button><span>第 {cursors.length} 页</span><Button variant="outline" disabled={!query.data?.meta.next_id || query.isFetching} onClick={() => { if (query.data?.meta.next_created_at) setCursors([...cursors, { cursor_created_at: query.data.meta.next_created_at, cursor_id: query.data.meta.next_id }]); }}>下一页</Button></div>
    <DetailDrawer open={Boolean(auditId)} onClose={() => change({ audit_id: "" })} title="模型调用内容" subtitle="请求中的用户输入摘录和助手文本输出">
      {detail.isLoading ? <p>加载中</p> : detail.isError ? <ErrorAlert error={detail.error}>内容加载失败</ErrorAlert> : detail.data ? <div className="grid min-w-0 gap-4 text-sm"><p>{detail.data.user_name} · {detail.data.agent_name} · {requestStatus(detail.data)}</p><p>{detail.data.model} · {date(detail.data.created_at)}<br />Token：{count(detail.data.total_tokens)} · 耗时：{count(detail.data.duration_ms)} ms{detail.data.usage_status !== "complete" ? ` · 用量${detail.data.usage_status === "partial" ? "部分可用" : "缺失"}` : ""}</p><p className="break-all">客户端：{detail.data.client_type || "unknown"}<br />会话：{detail.data.session_id || "--"}<br />轮次：{detail.data.turn_id || "--"}</p>{detail.data.error_code ? <p>{failureHints[detail.data.error_code] ?? "模型调用失败"}（{detail.data.error_code}）</p> : null}<ActivityText content={detail.data.activity_content} input /><ActivityText content={detail.data.activity_content} /></div> : null}
    </DetailDrawer>
  </PageShell>;
}

type TurnRequestGroup = { key: string; turnId: string; requests: ModelRequestPreview[] };
type SessionRequestGroup = { key: string; sessionId: string; turns: TurnRequestGroup[] };

function groupRequestsBySession(requests: ModelRequestPreview[]): SessionRequestGroup[] {
  const sessions = new Map<string, SessionRequestGroup>();
  for (const request of requests) {
    const sessionId = request.session_id || "";
    const sessionKey = sessionId || "__unattributed_session__";
    const session = sessions.get(sessionKey) ?? { key: sessionKey, sessionId, turns: [] };
    const turnId = request.turn_id || "";
    const turnKey = turnId || request.audit_id;
    const turn = session.turns.find((item) => item.key === turnKey);
    if (turn) turn.requests.push(request);
    else session.turns.push({ key: turnKey, turnId, requests: [request] });
    sessions.set(sessionKey, session);
  }
  return Array.from(sessions.values());
}

function SessionRequestGroup({ group, onOpen }: { group: SessionRequestGroup; onOpen(request: ModelRequestPreview): void }) {
  const requestCount = group.turns.reduce((total, turn) => total + turn.requests.length, 0);
  const totalTokens = sumKnownTokens(group.turns.flatMap((turn) => turn.requests));
  const latestRequest = group.turns[0]?.requests[0];
  return <Collapsible defaultOpen>
    <section className="min-w-0 overflow-hidden rounded-md border border-border bg-card">
      <CollapsibleTrigger asChild>
        <Button aria-label={`${group.sessionId ? `会话 ${group.sessionId}` : "未关联会话"}，${group.turns.length} 个轮次，${requestCount} 次请求，展开或收起`} className="group h-auto w-full justify-start rounded-none px-4 py-3 text-left hover:bg-muted/40" variant="ghost">
          <Layers3 className="mt-0.5 size-4 shrink-0 text-primary" aria-hidden="true" />
          <span className="min-w-0 flex-1">
            <span className="block text-[11px] font-normal text-muted-foreground">会话</span>
            <span className="block truncate font-mono text-sm font-semibold" title={group.sessionId || "未关联会话"}>{group.sessionId || "未关联会话"}</span>
            <span className="mt-1 block text-xs font-normal text-muted-foreground">最近调用 {latestRequest ? date(latestRequest.created_at) : "--"}</span>
          </span>
          <span className="flex shrink-0 flex-wrap items-center justify-end gap-1.5">
            <Badge variant="muted">{group.turns.length} 个轮次</Badge>
            <Badge variant="muted">{requestCount} 次请求</Badge>
            {totalTokens !== null ? <Badge variant="muted">{count(totalTokens)} Token</Badge> : null}
          </span>
          <ChevronDown className="ml-2 size-4 shrink-0 transition-transform motion-reduce:transition-none group-data-[state=open]:rotate-180" aria-hidden="true" />
        </Button>
      </CollapsibleTrigger>
      <CollapsibleContent className="border-t border-border">
        <div className="divide-y divide-border">
          {group.turns.map((turn, index) => <TurnRequestGroupView group={turn} defaultOpen={index === 0} key={turn.key} onOpen={onOpen} />)}
        </div>
      </CollapsibleContent>
    </section>
  </Collapsible>;
}

function TurnRequestGroupView({ group, defaultOpen, onOpen }: { group: TurnRequestGroup; defaultOpen: boolean; onOpen(request: ModelRequestPreview): void }) {
  const latestRequest = group.requests[0];
  return <Collapsible defaultOpen={defaultOpen}>
    <section className="min-w-0">
      <CollapsibleTrigger asChild>
        <Button aria-label={`${group.turnId ? `轮次 ${group.turnId}` : "未关联轮次"}，${group.requests.length} 次请求，展开或收起`} className="group h-auto w-full justify-start rounded-none px-4 py-2.5 text-left hover:bg-muted/30" variant="ghost">
          <MessageSquareText className="size-4 shrink-0 text-muted-foreground" aria-hidden="true" />
          <span className="min-w-0 flex-1">
            <span className="block text-[11px] font-normal text-muted-foreground">轮次</span>
            <span className="block truncate font-mono text-xs font-semibold" title={group.turnId || "未关联轮次"}>{group.turnId || "未关联轮次"}</span>
            <span className="mt-1 block text-xs font-normal text-muted-foreground">{latestRequest ? date(latestRequest.created_at) : "--"}</span>
          </span>
          <Badge variant="muted">{group.requests.length} 次请求</Badge>
          <ChevronDown className="ml-2 size-4 shrink-0 transition-transform motion-reduce:transition-none group-data-[state=open]:rotate-180" aria-hidden="true" />
        </Button>
      </CollapsibleTrigger>
      <CollapsibleContent>
        <div className="grid min-w-0 gap-2 bg-muted/20 p-3 sm:p-4">
          {group.requests.map((request) => <ModelRequestItem key={request.audit_id} onOpen={() => onOpen(request)} request={request} />)}
        </div>
      </CollapsibleContent>
    </section>
  </Collapsible>;
}

function ModelRequestItem({ request, onOpen }: { request: ModelRequestPreview; onOpen(): void }) {
  const statusVariant = request.status === "succeeded" ? "success" : request.status === "started" ? "warning" : request.status === "client_disconnected" ? "warning" : "danger";
  return <article className="grid min-w-0 gap-3 border-b border-border px-3 py-3 last:border-b-0 sm:px-4 lg:grid-cols-[minmax(0,1fr)_auto]">
    <div className="grid min-w-0 gap-3">
      <div className="flex min-w-0 flex-wrap items-center gap-x-3 gap-y-1 text-xs text-muted-foreground">
        <span>{date(request.created_at)}</span>
        <span className="font-medium text-foreground">{request.model}</span>
        <Badge variant={statusVariant}>{requestStatus(request)}</Badge>
        <span>{count(request.total_tokens)} Token</span>
        <span>{count(request.duration_ms)} ms</span>
      </div>
      <div className="grid min-w-0 gap-2 text-sm md:grid-cols-2">
        <div className="min-w-0"><div className="mb-1 flex items-center gap-1 text-xs font-medium text-muted-foreground"><MessageSquareText className="size-3.5" aria-hidden="true" />请求</div><p className="whitespace-pre-wrap break-words leading-6">{request.input_preview || "用户输入摘录不可用"}</p></div>
        <div className="min-w-0"><div className="mb-1 text-xs font-medium text-muted-foreground">响应</div><p className="whitespace-pre-wrap break-words leading-6">{request.output_preview || (request.output_status === "pending" ? "等待输出" : "本次无文本输出")}</p></div>
      </div>
      {request.error_code || request.output_status === "partial" || request.usage_status !== "complete" || request.input_truncated || request.output_truncated ? <div className="flex flex-wrap gap-x-3 gap-y-1 text-xs text-muted-foreground">
        {request.error_code ? <span className="text-destructive">{failureHints[request.error_code] ?? "模型调用失败"}（{request.error_code}）</span> : null}
        {request.output_status === "partial" ? <span>部分输出</span> : null}
        {request.usage_status !== "complete" ? <span>用量{request.usage_status === "partial" ? "部分可用" : "缺失"}</span> : null}
        {request.input_truncated || request.output_truncated ? <span>内容已截断</span> : null}
      </div> : null}
    </div>
    <div className="flex items-start justify-end lg:pl-2"><Button variant="outline" size="sm" onClick={onOpen}>查看内容</Button></div>
  </article>;
}

function sumKnownTokens(requests: ModelRequestPreview[]) {
  const known = requests.filter((request) => request.total_tokens != null);
  return known.length ? known.reduce((total, request) => total + (request.total_tokens ?? 0), 0) : null;
}
function ActivityText({ content, input = false }: { content: ModelActivityContent; input?: boolean }) {
  const text = input ? content.input_text : content.output_text;
  const truncated = input ? content.input_truncated : content.output_truncated;
  return <section className="min-w-0"><h3 className="mb-2 font-medium">{input ? "用户输入摘录" : "助手输出"}</h3><pre className="whitespace-pre-wrap break-words font-sans">{text || (input ? "本次请求没有可读用户文本" : content.output_status === "pending" ? "等待输出" : "本次无文本输出")}</pre>{!input && content.output_status === "partial" ? <p>部分输出：响应未完整结束或包含流错误。</p> : null}{truncated ? <p>内容已截断，最多保存 8000 个字符。</p> : null}</section>;
}
