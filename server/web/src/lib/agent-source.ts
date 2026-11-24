export function agentCreationSourceLabel(source?: string): string {
  switch (source) {
    case "collector":
      return "采集器创建";
    case "clawee_login":
      return "Clawee";
    case "manual":
      return "手动创建";
    case "legacy":
      return "历史数据";
    case "external":
      return "外部客户端";
    default:
      return "未知";
  }
}

export function agentDisplayId(agent: { agentId: string; clientId?: string; external?: boolean; externalClientType?: string }): string {
  return agent.external ? agent.externalClientType || agent.clientId || "外部客户端" : agent.agentId;
}
