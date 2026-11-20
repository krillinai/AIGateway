import { useState, type FormEvent } from "react";
import { Link, useSearchParams } from "react-router-dom";
import { DataTableShell, DetailDrawer, ErrorAlert, MetricCard, PageHeader, PageShell, TableStateRow } from "@/components/governance-ui";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { useModelActivityOverview, useModelRequestDetail, useModelRequests } from "@/hooks/useModelActivity";
import type { ModelActivityContent, ModelActivityFilter, ModelActivityStats, ModelRequest, ModelRequestCursor } from "@/lib/model-activity-api";

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
  const filter: ModelActivityFilter = { range: params.get("range") ?? "7d", user_id: params.get("user_id") ?? "", agent_id: params.get("agent_id") ?? "", unattributed: params.get("unattributed") === "true", session_id: params.get("session_id") ?? "", turn_id: params.get("turn_id") ?? "" };
  return detail ? <ModelActivityRequests key={JSON.stringify(filter)} filter={filter} /> : <ModelActivityOverviewView key={JSON.stringify(filter)} filter={filter} onFilter={(next) => setParams({ source: "model_proxy", range: next.range, user_id: next.user_id ?? "", agent_id: next.agent_id ?? "", ...(next.unattributed ? { unattributed: "true" } : {}) })} />;
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
    <DataTableShell><Table><TableHeader><TableRow>{["用户", "智能体", "请求数", "会话数", "交互轮次", "Token", "最近调用时间", "操作"].map((name) => <TableHead key={name}>{name}</TableHead>)}</TableRow></TableHeader><TableBody>
      {query.isLoading ? <TableStateRow colSpan={8}>加载中</TableStateRow> : !query.isError && query.data?.agents.length === 0 ? <TableStateRow colSpan={8}>当前范围无模型调用</TableStateRow> : query.data?.agents.map((agent) => {
        const search = new URLSearchParams({ source: "model_proxy", range: filter.range, user_id: agent.user_id, ...(agent.agent_id ? { agent_id: agent.agent_id } : { unattributed: "true" }) });
        return <TableRow key={`${agent.user_id}/${agent.agent_id}`}><TableCell>{agent.user_name}</TableCell><TableCell>{agent.agent_name}</TableCell><TableCell>{agent.request_count}</TableCell><TableCell>{agent.session_count}</TableCell><TableCell>{agent.turn_count}</TableCell><TableCell>{count(agent.total_tokens)}</TableCell><TableCell>{date(agent.last_request_at)}</TableCell><TableCell><Button asChild variant="outline" size="sm"><Link to={`/admin/activity/detail?${search}`}>查看调用</Link></Button></TableCell></TableRow>;
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
  const canFilter = Boolean(filter.user_id && filter.agent_id);
  function change(values: Record<string, string>) { const next = new URLSearchParams(params); Object.entries(values).forEach(([key, value]) => value ? next.set(key, value) : next.delete(key)); setParams(next); }
  function submit(event: FormEvent) { event.preventDefault(); change({ session_id: session.trim(), turn_id: session.trim() ? turn.trim() : "", audit_id: "" }); }
  return <PageShell>
    <PageHeader title="模型调用记录" actions={<><Button asChild variant="outline"><Link to="/admin/activity">返回活动</Link></Button><Button variant="outline" disabled={query.isFetching} onClick={() => { void query.refetch(); if (auditId) void detail.refetch(); }}>刷新</Button></>}>{scopeNotice}</PageHeader>
    <form onSubmit={submit} className="mb-4 flex flex-wrap items-end gap-3"><RangeSelect value={filter.range} onChange={(range) => change({ range })} /><div><Label htmlFor="model-session">会话 ID</Label><Input id="model-session" disabled={!canFilter} value={session} onChange={(event) => setSession(event.target.value)} /></div><div><Label htmlFor="model-turn">轮次 ID</Label><Input id="model-turn" disabled={!canFilter} value={turn} onChange={(event) => setTurn(event.target.value)} /></div><Button disabled={!canFilter} type="submit">精确筛选</Button><Button variant="outline" type="button" onClick={() => change({ session_id: "", turn_id: "", audit_id: "" })}>清除会话/轮次</Button></form>
    <p className="mb-3 text-sm text-muted-foreground">{turnNotice}</p>
    {query.isError ? <ErrorAlert error={query.error}>调用记录加载失败</ErrorAlert> : null}
    <DataTableShell><Table><TableHeader><TableRow>{["时间 / 模型", "会话 / 轮次 ID", "状态", "Token / 耗时", "输入摘录 / 助手输出", "操作"].map((name) => <TableHead key={name}>{name}</TableHead>)}</TableRow></TableHeader><TableBody>
      {query.isLoading ? <TableStateRow colSpan={6}>加载中</TableStateRow> : !query.isError && query.data?.items.length === 0 ? <TableStateRow colSpan={6}>当前范围无模型调用</TableStateRow> : query.data?.items.map((request) => <TableRow key={request.audit_id}>
        <TableCell>{date(request.created_at)}<div>{request.model}</div></TableCell><TableCell className="max-w-64 break-all">{request.session_id || "--"}<div>{request.turn_id || "--"}</div></TableCell><TableCell>{requestStatus(request)}{request.error_code ? <div>{failureHints[request.error_code] ?? "模型调用失败"}（{request.error_code}）</div> : null}{request.output_status === "partial" ? <div>部分输出</div> : null}</TableCell>
        <TableCell>{count(request.total_tokens)} / {count(request.duration_ms)} ms{request.usage_status !== "complete" ? <div>用量{request.usage_status === "partial" ? "部分可用" : "缺失"}</div> : null}</TableCell><TableCell className="max-w-80 whitespace-pre-wrap break-words">{request.input_preview || "用户输入摘录不可用"}<div>{request.output_preview || (request.output_status === "pending" ? "等待输出" : "本次无文本输出")}</div>{request.input_truncated || request.output_truncated ? <div>内容已截断</div> : null}</TableCell><TableCell><Button variant="outline" size="sm" onClick={() => change({ audit_id: request.audit_id })}>查看内容</Button></TableCell>
      </TableRow>)}
    </TableBody></Table></DataTableShell>
    <div className="mt-3 flex items-center gap-3"><Button variant="outline" disabled={cursors.length === 1 || query.isFetching} onClick={() => setCursors(cursors.slice(0, -1))}>上一页</Button><span>第 {cursors.length} 页</span><Button variant="outline" disabled={!query.data?.meta.next_id || query.isFetching} onClick={() => { if (query.data?.meta.next_created_at) setCursors([...cursors, { cursor_created_at: query.data.meta.next_created_at, cursor_id: query.data.meta.next_id }]); }}>下一页</Button></div>
    <DetailDrawer open={Boolean(auditId)} onClose={() => change({ audit_id: "" })} title="模型调用内容" subtitle="请求中的用户输入摘录和助手文本输出">
      {detail.isLoading ? <p>加载中</p> : detail.isError ? <ErrorAlert error={detail.error}>内容加载失败</ErrorAlert> : detail.data ? <div className="grid min-w-0 gap-4 text-sm"><p>{detail.data.user_name} · {detail.data.agent_name} · {requestStatus(detail.data)}</p><p>{detail.data.model} · {date(detail.data.created_at)}<br />Token：{count(detail.data.total_tokens)} · 耗时：{count(detail.data.duration_ms)} ms{detail.data.usage_status !== "complete" ? ` · 用量${detail.data.usage_status === "partial" ? "部分可用" : "缺失"}` : ""}</p><p className="break-all">会话：{detail.data.session_id || "--"}<br />轮次：{detail.data.turn_id || "--"}</p>{detail.data.error_code ? <p>{failureHints[detail.data.error_code] ?? "模型调用失败"}（{detail.data.error_code}）</p> : null}<ActivityText content={detail.data.activity_content} input /><ActivityText content={detail.data.activity_content} /></div> : null}
    </DetailDrawer>
  </PageShell>;
}
function ActivityText({ content, input = false }: { content: ModelActivityContent; input?: boolean }) {
  const text = input ? content.input_text : content.output_text;
  const truncated = input ? content.input_truncated : content.output_truncated;
  return <section className="min-w-0"><h3 className="mb-2 font-medium">{input ? "用户输入摘录" : "助手输出"}</h3><pre className="whitespace-pre-wrap break-words font-sans">{text || (input ? "本次请求没有可读用户文本" : content.output_status === "pending" ? "等待输出" : "本次无文本输出")}</pre>{!input && content.output_status === "partial" ? <p>部分输出：响应未完整结束或包含流错误。</p> : null}{truncated ? <p>内容已截断，最多保存 8000 个字符。</p> : null}</section>;
}
