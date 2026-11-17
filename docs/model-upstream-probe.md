# 模型上游 Responses 探测

[English](./en/model-upstream-probe.md)

有 `MCPUpstreamUpdate` 权限的管理端可调用 `POST /api/v1/admin/model-proxy/upstream-accounts/test`，显式选择已配置在账户上的模型：

```json
{"id":"ACCOUNT_ID","model":"RESPONSES_MODEL_ID"}
```

`model` 会去掉首尾空白；它必须属于账户当前合并后的模型目录（同步模型和手动模型）。未配置的模型返回 HTTP 400，且不请求上游。不传 `model` 或传空白字符串时保留原行为：使用目录第一项；这不表示第一项一定支持 Responses。管理台现有按钮仍使用该兼容行为。

该接口仅用所选模型发出流式 Responses 请求，返回实际探测的 `model`、`reply` 和 `latency_ms`。成功需要文本事件和完成事件，错误或未完成的流不会被当作成功。不更改手动模型、模型目录或模型组路由，也不自动尝试其他模型。

离线回归使用本机 HTTP 测试服务与内存存储，覆盖首项不支持 Responses 的混合目录、显式选择、未配置模型拒绝及目录不变；不读取真实供应商凭证，不使用生产数据库。真实模型/协议验证及凭证私下交付需另行确定范围。
