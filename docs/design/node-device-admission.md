# 节点设备准入与用户范围缓存

`POST /v1/server/device` 校验节点是否服务请求中的订阅，再通过 Redis Lua 原子申请
订阅的来源 IP 名额。租约为 300 秒，同一 IP 在不同节点上共享名额；在线上报续租。
成功响应仍为 `{"allowed":true,"ttl_seconds":300}`，设备数达到限制时返回
`allowed:false`。Redis 或用户范围读取失败不会授予名额。

## 共享用户索引

设备准入、流量上报、在线上报和 `GET /v1/server/alivelist` 共用进程内的
`(server_id, protocol)` 索引。索引只保留 SID、DeviceLimit 和只读 SID 集合，不保留
用户 UUID 或完整 JSON。设备准入热路径只读取 Redis generation 并按 SID 查表，
不会重新传输、解析整份用户列表，也不会为每次上报重新创建 SID 集合。

- 每次访问读取现有 `ServerCacheGeneration`；generation 改变就刷新索引。
- 从 Redis 列表加载时读取其 PTTL，索引期限不超过源列表剩余期限，最长 5 分钟。
  数据库重建的期限沿用 `ServerCacheTTL`，保留原有时间到期刷新行为。
- 发布索引前再次检查 generation，丢弃跨越失效操作的旧结果。连续变化超过三次
  返回错误，不将旧结果标成新 generation。
- 空列表中的占位用户不会获得准入权限；SID 1 的真实订阅仍通过 UUID 校验区分。
- LRU 最多保留 256 个范围、合计 100 万个订阅索引项。单个范围超过预算时独占缓存，
  避免该范围每次请求都重新解析。这里限制的是索引项数量，并非进程内存的硬字节上限。

## 并发与过载

同一进程内，同一范围的索引加载及用户列表重建分别使用 singleflight 合并。
重建前复查 Redis，避免早先的缓存未命中再次回源。所有端点共用重建入口；不同后端
进程仍各有自己的索引和 singleflight，Redis 的 generation 检查阻止失效前的重建
重新填入旧缓存。

每个服务实例最多同时执行 128 个设备准入、4 个索引加载和 4 个列表重建。
超额工作立即返回错误，节点上报／设备接口映射为 HTTP 503，并带
`Retry-After: 1`、`Cache-Control: no-store`，JSON 与 Protobuf 协商均保留。
设备准入 context 期限为 5 秒，共享加载为 10 秒；取消一个等待者不会取消其他请求
依赖的共享加载，等待者可以独立退出。这些上限为实现常量。

节点收到过载或服务错误后应拒绝没有有效租约的新连接，并对后续申请使用有抖动的退避；
`Retry-After` 是重试提示，不是放行许可。

## 验证与压测

回归测试覆盖共享加载、generation 变化、失效中的数据库重建、TTL、占位用户、
范围隔离、Redis 故障、并发准入及 503 响应：

```sh
go test -race ./internal/module/network/...
```

微基准对比 1 千和 10 万用户下的热索引读取与原来的 Redis 整表读取、JSON 解析及线性查找：

```sh
go test ./internal/module/network/internal/serverapi -run '^$' \
  -bench '^BenchmarkServedUserLookup$' -benchmem
```

微基准使用 miniredis，不能代替部署环境的 HTTP 并发压测。部署验收应使用相同准入
并发度分别测试两种列表规模，并覆盖冷启动、缓存到期和批量失效，观察分配量、常驻
内存、Redis／数据库延迟、503 比例及订阅接口延迟。在线人数接口仍需按所服务用户
查询在线计数，其总工作量随用户数增长。
