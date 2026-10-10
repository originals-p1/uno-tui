# Terminal UNO：跨平台终端多人联机游戏设计文档

> 版本：v1.1（可执行实现规格）
> 技术栈：Go + Bubble Tea v2 + Lip Gloss + TCP + JSON
> 平台：Windows / Linux / macOS
> 网络：局域网直连；跨网使用 ZeroTier 连接房主
> 交付：一个可执行文件 `uno`，同时支持开房和加入

本文档是 v1 的**唯一权威产品与实现规格**。文中的“必须”均为验收要求；示例与实现建议若和明确契约冲突，以明确契约为准。实现过程中不得自行改变规则或协议语义；需要变更时先更新本文档和对应测试。

## 1. 项目目标与范围

### 1.1 目标

实现一个无需浏览器的跨平台终端 UNO，多名玩家通过命令行加入由其中一位玩家托管的房间。客户端呈现适合终端的牌局信息、自己的手牌、可出牌提示、回合历史及快捷键操作。

核心要求：

1. 统一采用 Go；不依赖专用公网服务器。
2. 房主机器既运行 TCP 服务端，也以普通客户端身份参加游戏。
3. 内网玩家连接房主局域网 IP；外网玩家加入同一个 ZeroTier 网络后连接房主的 ZeroTier IP。
4. 服务端权威执行规则，绝不向其他玩家泄露手牌。
5. 轮到玩家时按可用/不可用分组显示手牌；仅可用牌提供数字快捷键。
6. 出牌后手牌剩一张自动喊 UNO，无手动按键、无漏喊罚牌。
7. 跨平台单文件分发，终端尺寸变化时可以适应。

### 1.2 范围与非目标

**v1 必须支持**：2–4 人、一间由房主托管的房间、标准 108 张牌（经典规则）、回合及方向、数字牌、Skip、Reverse、Draw Two、Wild、Wild Draw Four、抽牌、胜利判定、自动 UNO、准备/开局、网络掉线提示、操作错误反馈。

**v1 不要求**：账号体系、排行榜、机器人、多房间、持久化对局、匹配大厅、观战、聊天、房主迁移、自动公网穿透、联网服务发现。

扩展功能应与引擎解耦，并通过后续版本交付。

## 2. 用户使用流程

### 2.1 命令行

```bash
uno host --port 7777 --name Alice --room-key-file ./room.key
uno join 192.168.1.10:7777 --name Bob --room-key-file ./room.key
uno join 10.147.0.2:7777 --name Charlie --room-key-file ./room.key
uno --help
```

- `host`：启动监听 `0.0.0.0:7777` 的 TCP 服务器，然后房主本地客户端连接 `127.0.0.1:7777`，进入房间大厅。未指定 `--room-key-file` 时生成 32 字节密码学随机密钥，以 Base64URL 显示一次；指定文件不存在时创建并以仅当前用户可读写的权限保存，已存在时读取。Windows 上尽力限制 ACL，并明确提示用户保护文件。
- `join`：连接指定服务器地址并进入大厅。地址缺省端口时默认为 7777。入场密钥从 `--room-key-file` 读取；若未提供，则在连接前通过不回显的交互提示输入。不得提供明文 `--room-key` 参数，避免泄漏到 shell 历史和进程列表。
- 房主也必须通过相同的 `hello` 鉴权流程连接，但服务端启动时生成一次性 `host_bootstrap_token`，仅经进程内存交给本地客户端，用于授予 host 身份；玩家名或来源 IP 均不能授予 host 权限。
- 所有玩家在大厅看到姓名、连接情况、准备状态；房主在 2–4 人齐备时启动。
- 房主退出或托管进程终止时，v1 游戏直接结束，所有客户端获得提示。
- 默认仅一间房，暂不需要房间 ID；未来多房间时可引入房间码。

### 2.2 内外网联机

```text
办公室 LAN:
  Player B 192.168.1.20 ---\
                            \ TCP 7777
  Host      192.168.1.10 ---- UNO Server (同一台房主电脑)
             10.147.0.2 ---/
                          /  ZeroTier 虚拟网络
外网:
  Player C 10.147.0.3 ---/
```

ZeroTier **仅提供 IP 层虚拟连接**，不参与 UNO 协议、房间管理或牌局同步。两种连接方式最终进入同一个 `Room`。房主无须开启 NAT 转发。ZeroTier 连接可能是直连或中继；必须先确保各端 ZeroTier 已加入网络并获授权。

**连通性要求**：
- 房主对局监听的地址为 `0.0.0.0`（IPv4 MVP），而非 `127.0.0.1`。
- 房主操作系统防火墙允许程序接收指定 TCP 端口连接，最好限定办公室局域网网段与 ZeroTier 虚拟网段。
- 企业办公网络需遵守所在地网络安全策略；被隔离的办公设备不可假定互通。
- ZeroTier 不是应用层身份认证。除网络访问限制外，使用随机生成的会话入场口令或房间密钥，避免同网段未授权玩家加入。比较密钥时使用恒定时间比较；连续鉴权失败按来源 IP 做有界退避/限速，错误不得泄露身份细节。房间密钥不是互联网级强认证；直接暴露公网仍必须使用 TLS 和更完整的访问控制。

## 3. 游戏规则规格

### 3.1 规则配置与版本

使用一套可测试的“经典 UNO 规则配置”，拒绝把民间规则隐式写入引擎。每局开始固定配置；以后允许通过 `RuleSet` 切换变体。

默认：
- 2–4 名玩家，每人开局 7 张。
- 使用 108 张经典牌：红黄绿蓝各 0×1、1–9×2、Skip×2、Reverse×2、Draw Two×2（每色 25 张），外加 Wild×4、Wild Draw Four×4。
- 出牌必须满足当前颜色、牌面值/动作种类匹配，或是 Wild；Wild Draw Four 有额外合法性约束。
- 游戏按轮次顺序推进；不允许 `+2/+4` 叠加（stacking）。
- 回合外不能出牌、摸牌。
- 玩家出完最后一张即获胜。首版按单局胜负，不累计计分。

### 3.2 回合与牌效

| 牌 | 效果 |
|---|---|
| 数字牌 | 普通出牌，轮到下一位 |
| Skip | 下一位被跳过 |
| Reverse | 反转方向；**2 人局相当于 Skip**，出牌者再行动 |
| Draw Two | 下一位抽 2 张并跳过回合 |
| Wild | 玩家必须指定接下来的颜色 |
| Wild Draw Four | 必须指定颜色；下一位抽 4 张并跳过；仅在出牌者**不持有当前有效颜色牌**时合法（即使持有相同数字/动作的别色牌，仍可合法出 +4） |

Wild Draw Four 的“挑战”规则会增加流程复杂度。**v1 使用严格合法性校验但不提供挑战流程**，必须在 UI 中明确说明这是产品规则选择，与完整官方挑战机制不同；v2 可加入挑战机制。若要完整遵守官方玩法，应将挑战列为开局前必须实现的功能。

### 3.3 出牌、摸牌、回合

1. 服务端在回合开始时计算合法可出牌集合。
2. 玩家选择合法牌后，服务端原子地移除该牌、应用效果、检测 UNO/胜负、推进回合、增加状态版本并广播状态。
3. 玩家选择 `D` 时，摸一张；若摸到牌合法，依本项目规则选择“可选择立刻出这张或跳过”，不可改出原手牌。若摸到牌不合法，服务端在同一事务中直接推进回合。合法时进入 `drawn_decision` 阶段，并仅向该玩家提供 `draw_decision` 私有字段；`P` 表示跳过。`play_drawn_card` 不接收任意 `card_id`，服务端只允许打出当前 `DrawnCardID`。
4. 牌堆不足时，保留弃牌堆最上方牌，将其余弃牌洗回摸牌堆；仍不足时按实际可摸数量处理。
5. 每个客户端只能看到自己的手牌和其他玩家的手牌数量。
6. 发牌后从剩余牌堆顶部向下寻找第一张数字牌作为初始顶牌；将它移入弃牌堆，其前方跳过的动作牌使用本局 RNG 重新洗入剩余摸牌堆。标准牌组必定存在数字牌；若自定义牌组不存在数字牌，`Start()` 返回 `ERR_NO_INITIAL_NUMBER_CARD` 且不改变状态。这样不会丢牌，也不存在无界循环。

### 3.4 自动 UNO

- 任何合法出牌/摸牌流程导致某玩家**最终手牌恰好 1 张**时，自动记录 UNO 状态并生成 `UNO_CALLED` 事件。
- 若下一步手牌数变为 0 或大于 1，则清除 UNO 状态；不能重复刷事件。
- 不存在玩家手动输入 `U`，也不存在忘记喊 UNO 的罚牌机制。
- UNO 状态可以显示在玩家列表中，并在历史区域生成提示。

### 3.5 输入异常与边界条件

- 出牌请求含不可用牌、不属于自己的牌、回合不匹配、状态版本已变化：服务端拒绝，返回错误码，**不修改状态**。
- 多人近乎同时提交操作：同一房间内严格串行处理，并按实际合法回合状态验证。
- 玩家离线：v1 暂停该局并显示断线状态，支持给离线玩家一个可配置的重连宽限期（默认 60 秒）；过期后结束本局。避免隐式自动替离线玩家出牌。
- 房主自身网络客户端掉线但服务端还在：在宽限期内允许本地客户端重连；若托管进程退出则全局结束。
- 服务器切勿信任客户端给出的“合法牌列表”“当前回合”或任何未校验的状态。

### 3.6 权威状态机

房间状态与阶段转移必须由服务端事件循环独占执行：

```text
lobby --start_game--> turn
turn --draw playable--> drawn_decision --play/pass--> turn | finished
turn --play_card-------------------------------> turn | finished
turn/drawn_decision --player disconnected-----> paused
paused --all disconnected players reconnect---> previous_phase
paused --grace expired / host process exits----> finished
```

- `start_game` 仅 host 可发起，要求 2–4 名已连接玩家且除 host 外所有玩家已准备；host 视为始终准备。开局后拒绝新玩家，只允许持有效重连 token 的原玩家重连。
- 进入 `paused` 时保存 `previous_phase`、离线玩家集合及各自截止时间。暂停期间拒绝游戏动作，但允许 `ping`、重连及主动结束。
- 多人断线时分别计算 60 秒宽限期；任一玩家到期即以 `PLAYER_RECONNECT_TIMEOUT` 结束本局。全部恢复后回到精确的 `previous_phase`，不得重新执行已完成的牌效。
- `finished` 是终态。正常胜利设置 winner；异常结束设置 `end_reason`，不得再接受状态修改动作。
- 所有请求都先验证会话、消息格式、阶段、权限、`expected_revision`，再验证具体游戏规则；失败不得修改任何房间状态。

## 4. 终端 UI 设计

### 4.1 总体布局

```text
 UNO  Room: LOCAL                  Players: 4/4
────────────────────────────────────────────────
 Players                 Turn: Bob   ↻
 Alice           3
 Bob             1  🔥 UNO!
 Charlie         5
 Dave            6
────────────────────────────────────────────────
 History
 Bob: 出牌 红4
 Bob: UNO!
 Alice: 摸牌
────────────────────────────────────────────────
 Current: 🟥 红2

 Your Cards (YOUR TURN):
 可用    [1] 红1  [2] 红3  [3] 蓝2
 不可用  蓝3  黄7

 1-3 出牌   D 摸牌   Q 退出
────────────────────────────────────────────────
```

**规则纠正**：顶牌为红 2 时，红色任意数字牌、任意颜色的数字 2、Wild 等都可能合法，因此蓝 2 应当归入“可用”。

### 4.2 根据回合切换手牌视图

- **非自己的回合**：按稳定的原始手牌顺序显示，不动态重排，禁止出牌及摸牌输入；仍可查看公共信息。
- **自己的回合（正常阶段）**：将手牌在视觉上分为“可用”和“不可用”；可用牌靠前显示并分配索引快捷键，不可用牌置灰、不可选。**不修改引擎中的原始手牌顺序**。
- 轮到自己时优先显示数字快捷键 `1`–`9`；当可用牌多于 9 张时，用两位数字编号配合输入缓冲/Enter 确认（或用方向键+Enter）；避免 `10` 与 `1` 的输入歧义。
- 选择 Wild 或 +4 后，显示颜色选择弹层 `R/G/B/Y`；玩家确认后一次性提交出牌请求。
- 在“摸牌后可出刚摸的牌”阶段，专门显示 `Enter 出刚摸的牌`、`P 跳过`，不允许选择原有手牌。
- 自己不在回合时显示 `Waiting for <player>…`，保留自己的牌但不露出合法性索引。
- 某玩家剩一张时玩家列表显示 `UNO!`。

### 4.3 TUI 状态

`LobbyView`、`PlayingView`、`ChoosingColorView`、`DrawDecisionView`、`DisconnectedView`、`GameOverView`、`HelpView`。

键位：

| 场景 | 键位 | 行为 |
|---|---|---|
| 大厅 | `R` | 准备 / 取消准备 |
| 房主大厅 | `S` | 满足人数及准备状态后开始 |
| 自己回合 | `1`–`9` / 多位输入+Enter | 选择可出牌 |
| 自己回合 | 方向键 + Enter | 焦点选牌并出牌 |
| 自己回合 | `D` | 摸一张 |
| 选择颜色 | `R/G/B/Y` | 选红/绿/蓝/黄 |
| 抽牌待决 | Enter / `P` | 出刚抽到的牌 / 结束回合 |
| 任意界面 | `?` | 帮助 |
| 任意界面 | `Q` | 请求退出并确认，防误触 |

**显示策略**：使用 Bubble Tea 的消息驱动事件循环；网络 goroutine 将结构化事件送入 Bubble Tea 的 `Msg`，在 `Update` 中变更只读客户端状态快照，`View` 纯渲染。终端尺寸过小时优先压缩历史窗口、换行手牌；提供窄屏简化模式。颜色不足时自动退化为文字标签（如 `[R]2`），不依赖 Emoji 的固定显示宽度。

## 5. 系统架构

```text
                          Go 单可执行文件 uno
┌─────────────────────────────────────────────────────────┐
│ host 模式                                               │
│  ┌─────────────────┐       loopback TCP                  │
│  │ Room Server     │◄────────────────┐                  │
│  │ ├ Session       │                 │                  │
│  │ ├ Room loop     │             Local Client            │
│  │ └ Game Engine   │                 │                  │
│  └───────▲─────────┘                 ▼                  │
│          │                       Bubble Tea             │
└──────────┼──────────────────────────────────────────────┘
           │ TCP / JSON framed
     ┌─────┴────────────┐
     │                  │
  LAN Clients      ZeroTier Clients
```

### 5.1 模块职责

- `game`：纯规则引擎、牌堆、动作校验、回合推进、生成视图；不依赖网络和 TUI。
- `protocol`：消息类型、编码解码、最大消息大小、协议版本。
- `server`：监听、连接/session 生命周期、身份与重连 token、房间事件队列、广播个性化视图。
- `client`：拨号、读写、自动重连、请求号、状态版本管理。
- `tui`：按状态渲染、键盘输入与提示；不决定动作是否合法。
- `cmd/uno`：命令行选项、host/join 启动和优雅退出。

### 5.2 并发与一致性

一间房的**全部状态修改只在该房间的事件循环内发生**，用 channel 投递玩家动作、连接变更、超时消息，防止并发读写问题。每个连接可以分别运行读取与写入 goroutine，写操作由独立有界队列串行发送，不允许多个 goroutine 同时写同一连接而破坏消息顺序。

`room_revision` 是整个房间的单调版本号，不只是牌局版本。玩家加入/离开、准备、开局、游戏动作、连接状态、暂停、恢复及结束等任何客户端可观察状态变化，在事务提交时都必须恰好递增一次；被拒绝的请求、心跳和重复请求不得递增。客户端仅用更高版本的快照替换当前视图，相同版本的重复快照可忽略，低版本必须忽略。

除首次 `hello` 外，每个改变状态的请求都携带当前 `session_id` 内唯一的 `request_id` 和 `expected_revision`；`hello` 携带 `request_id` 但不携带 `expected_revision`，`ping/pong` 两者都不需要。服务端在会话有效期内缓存最近 256 个状态修改请求的规范化消息摘要及最终响应。处理顺序固定为：帧与消息格式 → hello 鉴权/既有 session 鉴权 → 幂等缓存查找 → revision → phase/权限 → 规则校验 → 原子提交：

- 首次收到：验证并执行，将响应写入缓存后发送。
- 相同 `request_id`、相同 payload：不再执行，原样返回缓存响应。
- 相同 `request_id`、不同 payload：返回 `REQUEST_ID_REUSED`，不改变状态。
- 未命中缓存且 `expected_revision != room_revision`：返回 `STALE_REVISION` 和最新个人快照。

缓存属于逻辑 session，断线重连后继续有效，直至重连宽限期结束。客户端重连后可安全重发尚未得到响应的原请求；不得用新 `request_id` 猜测重试。

### 5.3 视图投影和隐私

```go
type PlayerView struct {
    ID        string `json:"id"`
    Name      string `json:"name"`
    CardCount int    `json:"card_count"`
    UNO       bool   `json:"uno"`
    Connected bool   `json:"connected"`
    Ready     bool   `json:"ready"`
    IsHost    bool   `json:"is_host"`
}

type GameView struct {
    RoomRevision  uint64       `json:"room_revision"`
    Players       []PlayerView `json:"players"`
    CurrentPlayer string       `json:"current_player"`
    Direction     int          `json:"direction"`
    TopCard       CardView     `json:"top_card"`
    ActiveColor   string       `json:"active_color"`
    YourHand      []CardView   `json:"your_hand"`
    PlayableIDs   []string     `json:"playable_ids"`
    Phase         string       `json:"phase"`
    DrawDecision  *DrawDecisionView `json:"draw_decision,omitempty"`
    Paused        *PausedView       `json:"paused,omitempty"`
    WinnerPlayerID string           `json:"winner_player_id,omitempty"`
    EndReason     string            `json:"end_reason,omitempty"`
    LastEventID   uint64            `json:"last_event_id"`
    History       []GameEvent  `json:"history"`
}

type DrawDecisionView struct {
    Card CardView `json:"card"`
}

type PausedView struct {
    DisconnectedPlayerIDs []string `json:"disconnected_player_ids"`
    EarliestDeadline      string   `json:"earliest_deadline"` // RFC 3339 UTC
}
```

`GameView` 由 `BuildViewFor(playerID)` 生成，必须做到不同玩家的视图不同：`YourHand` 仅包含请求者的卡牌，`PlayableIDs` 仅包含请求者的合法牌 ID。`DrawDecision` 仅在该请求者处于 `drawn_decision` 时非空，其他玩家必须收到空字段。牌 ID 为局内唯一的随机或递增标识（不要用“红2”作为唯一 ID，因为同色同值可能有两张）。客户端可根据 `PlayableIDs` 做分组，但服务端必须再次独立验证出牌请求。

## 6. 网络协议

### 6.1 TCP 帧格式

```text
[ 4-byte unsigned length, big-endian ][ UTF-8 JSON, length bytes ]
```

长度字段仅表示 JSON payload 长度，v1 最大值固定为 64 KiB；读帧时先校验长度、再 `io.ReadFull`，防止内存耗尽和拆包粘包。写帧时先序列化并限制大小，再通过单写协程完整写出。连接设置读写超时、心跳及空闲超时。

Go 示例：

```go
func ReadFrame(r io.Reader, max uint32) ([]byte, error) {
    var n uint32
    if err := binary.Read(r, binary.BigEndian, &n); err != nil { return nil, err }
    if n == 0 || n > max { return nil, errors.New("invalid frame length") }
    body := make([]byte, n)
    _, err := io.ReadFull(r, body)
    return body, err
}
```

### 6.2 消息封套

所有帧都使用同一个封套。请求必须有 `request_id`；响应回显 `request_id`；服务端主动事件不带 `request_id`，但必须带 `event_id` 和 `room_revision`。未知 JSON 字段可忽略，以便同一主版本向前兼容；缺少必填字段、未知消息类型或类型错误返回 `INVALID_MESSAGE`。协议主版本不一致立即返回 `PROTOCOL_MISMATCH` 并关闭连接。

```json
{
  "protocol_version": 1,
  "type": "play_card",
  "request_id": "c42",
  "expected_revision": 19,
  "payload": {"card_id": "card-17", "chosen_color": null}
}
```

成功响应：

```json
{
  "protocol_version": 1,
  "type": "response",
  "request_id": "c42",
  "room_revision": 20,
  "payload": {"accepted": true}
}
```

错误响应：

```json
{
  "protocol_version": 1,
  "type": "error",
  "request_id": "c42",
  "room_revision": 20,
  "payload": {"code": "STALE_REVISION", "message": "state changed", "snapshot": {}}
}
```

主要消息类型：

| 方向 | 类型 | 用途 |
|---|---|---|
| C→S | `hello` | 协议版本、玩家名、入场密钥，以及可选重连 token / host bootstrap token |
| S→C | `welcome` | player_id、session_id、session token（私有）、个人房间快照 |
| C→S | `ready` | 准备状态 |
| C→S | `start_game` | 房主请求开局 |
| C→S | `play_card` | 按 card_id 出牌，可携带颜色 |
| C→S | `draw_card` | 摸牌 |
| C→S | `play_drawn_card` / `pass_drawn_card` | 处理刚摸到的牌；前者仅携带可选 chosen_color |
| C→S | `leave` | 主动离开 |
| S→C | `state_snapshot` | 针对当前玩家的完整状态 |
| S→C | `game_event` | 公共事件（不带隐私手牌） |
| S→C | `error` | 结构化错误码与提示 |
| C↔S | `ping` / `pong` | 活性检测 |

典型错误码：`INVALID_MESSAGE`、`AUTH_FAILED`、`NOT_YOUR_TURN`、`CARD_NOT_OWNED`、`CARD_NOT_PLAYABLE`、`STALE_REVISION`、`REQUEST_ID_REUSED`、`INVALID_PHASE`、`ROOM_FULL`、`GAME_ALREADY_STARTED`、`NOT_HOST`、`RECONNECT_EXPIRED`、`PROTOCOL_MISMATCH`。

`state_snapshot` 包含 `last_event_id`，表示快照已吸收截至该 ID 的全部事件。每个 `game_event` 包含严格递增的 `event_id`、产生它的 `room_revision`、事件类型和不含秘密的 payload。客户端收到快照后丢弃 `event_id <= last_event_id` 的事件，并按事件 ID 去重；若发现 ID 缺口，不自行补算状态，而是请求新快照。历史区以快照内的有界 `History` 为准，实时事件仅作为到达下一快照前的增量显示。

### 6.3 核心事务顺序

```text
Client             Room Event Loop              Game Engine
  │ play_card(req)       │                           │
  ├─────────────────────►│ validate session/turn     │
  │                      ├──────────────────────────►│
  │                      │     apply + event         │
  │                      │◄──────────────────────────┤
  │                      │ room_revision++           │
  │                      │ BuildViewFor(each player) │
  │◄─────────────────────┤ personalized snapshot     │
  │                      │                           │
```

不要把自己的手牌通过 `game_event` 全房广播；单独的玩家视图快照用点对点传输。

## 7. 数据模型建议

```go
type Color string // red, yellow, green, blue, wild
type Rank string  // 0..9, skip, reverse, draw_two, wild, wild_draw_four

type Card struct {
    ID    string
    Color Color
    Rank  Rank
}

type Player struct {
    ID        string
    Name      string
    Hand      []Card
    Connected bool
    HasUNO    bool
}

type Phase string // lobby, turn, drawn_decision, paused, finished

type Game struct {
    Players        []Player
    DrawPile       []Card
    DiscardPile    []Card
    CurrentIndex   int
    Direction      int // +1 or -1
    ActiveColor    Color
    Phase          Phase
    RoomRevision   uint64
    DrawnCardID    string
    WinnerPlayerID string
    EndReason      string
    PreviousPhase  Phase
}
```

引擎对外提供 `Start()`、`Apply(Action)`、`LegalMoves(playerID)`、`BuildViewFor(playerID)`。所有转移返回事件和错误，不直接操作连接或终端。

注意：当顶牌为 Wild 时，合法性依据 `ActiveColor`（出牌人声明的颜色）而不是顶牌 `Color=wild`；动作牌匹配按 `Rank` 判断。`Wild Draw Four` 必须进一步确认玩家不存在 `ActiveColor` 同色牌。

## 8. 源码目录与依赖

```text
terminal-uno/
├── cmd/uno/main.go
├── internal/
│   ├── game/
│   │   ├── card.go
│   │   ├── deck.go
│   │   ├── rules.go
│   │   ├── game.go
│   │   ├── view.go
│   │   └── game_test.go
│   ├── protocol/
│   │   ├── message.go
│   │   ├── frame.go
│   │   └── frame_test.go
│   ├── server/
│   │   ├── server.go
│   │   ├── room.go
│   │   ├── session.go
│   │   └── server_test.go
│   ├── client/
│   │   ├── client.go
│   │   └── reconnect.go
│   └── tui/
│       ├── model.go
│       ├── update.go
│       ├── view.go
│       ├── styles.go
│       └── view_test.go
├── go.mod
├── go.sum
├── README.md
└── .github/workflows/build.yml
```

推荐依赖：`github.com/charmbracelet/bubbletea/v2`、对应版本的 `github.com/charmbracelet/lipgloss/v2`（具体模块路径以锁定版本的官方 `go.mod` 为准）。服务端与协议层优先使用 Go 标准库；不要将 TUI 依赖引入 `internal/game`。

## 9. 可靠性、安全与运维

- **身份**：`hello` 绑定玩家 ID。重连 token 使用 `crypto/rand` 生成至少 32 字节熵，服务端只保存其 SHA-256 摘要，客户端仅在 `welcome` 中收到一次原值；token 不写公共状态或日志。成功重连必须轮换 token，并使旧 token 失效。玩家显示名去除首尾空白后长度为 1–24 个 Unicode code point，控制字符非法；显示名不作为身份，同房间内重名时服务端拒绝加入。
- **会话**：`session_id` 是非秘密标识，token 才是凭据。新连接完成 `hello` 前不能进入房间事件循环；鉴权失败统一返回 `AUTH_FAILED`。每个 session 的幂等缓存与重连生命周期一致。
- **连接**：限制客户端数、心跳、最大帧长、超时、读写队列容量，防止恶意输入卡死服务端。
- **公网威胁模型**：在 ZeroTier 和内网访问策略下使用；**ZeroTier 不意味着端到端应用层加密/身份策略可以省略**。若要直接暴露到公网，必须增加 TLS（证书或可信隧道方案）、访问控制和更完善的限流。
- **隐私**：禁止日志输出其他玩家手牌或 session token；不要在全局广播体中包含秘密字段。
- **异常恢复**：网络重连可恢复会话，重新发送最新个人视图；房主进程退出时数据丢失是 v1 已知限制。
- **取消与关闭**：所有 goroutine 绑定 `context`；关闭连接释放读写协程，房主退出要关闭 listener 和房间循环。
- **日志**：采用结构化日志，可记录 session_id、player_id、request_id、错误码和 room_revision；禁止记录房间密钥、重连 token、host bootstrap token、完整 `hello` payload 或任何手牌。

v1 默认运行参数如下，代码中集中定义并允许测试注入；除 `--port` 和未来明确公开的 CLI 配置外，不要求用户调整：

| 参数 | 默认值 | 满限行为 |
|---|---:|---|
| 最大帧 payload | 64 KiB | 返回协议错误并关闭连接 |
| 每连接写队列 | 64 条消息 | 将该客户端标记断线，不能阻塞房间 |
| 每 session 幂等缓存 | 256 项 | 按完成顺序淘汰最旧项 |
| 公共历史 | 100 个事件 | 淘汰最旧事件，event_id 不重置 |
| 服务端 ping 间隔 | 10 秒 | 发送 ping |
| 无有效 pong/消息超时 | 30 秒 | 标记断线并进入暂停逻辑 |
| 重连宽限期 | 60 秒 | 以 `PLAYER_RECONNECT_TIMEOUT` 结束 |
| hello 完成时限 | 10 秒 | 关闭未鉴权连接 |
| 单 IP 鉴权失败限制 | 滚动 1 分钟内 5 次 | 后续尝试统一返回 `AUTH_FAILED`；窗口结束恢复 |

## 10. 跨平台构建和分发

示例：

```bash
GOOS=windows GOARCH=amd64 go build -o dist/uno-windows-amd64.exe ./cmd/uno
GOOS=linux   GOARCH=amd64 go build -o dist/uno-linux-amd64 ./cmd/uno
GOOS=darwin  GOARCH=arm64 go build -o dist/uno-darwin-arm64 ./cmd/uno
```

正式 CI 可补充 Windows arm64、Linux arm64、macOS amd64；在真实终端测试颜色、键盘、Unicode 宽度、窗口缩放和退出时终端状态还原。

## 11. 开发里程碑与验收标准

### M1：纯游戏引擎
- 108 张牌和随机种子可复现洗牌。
- 2–4 玩家发牌、回合、方向、牌效、胜负。
- 自动 UNO，只触发一次且不会在 0 张时触发。
- 测试 Wild +4 限制、2 人 Reverse、摸牌后仅能出新牌、牌堆回收。

### M2：TCP 联机与房主模式
- `uno host` 可开房并作为玩家参与；`uno join IP:PORT` 可加入。
- 两个或更多连接可同步动作；不合法动作不改变状态。
- 玩家只能收到自己的手牌；正确分发 room_revision 和私有状态。
- 本机 loopback、同 LAN、ZeroTier IP 都能复用同一个端口。

### M3：完整终端 UI
- 复现布局，正常阶段可用/不可用分组正确。
- `红2` 顶牌时蓝2为可出牌，蓝3不可出。
- 未轮到自己时手牌顺序稳定，快捷键禁用。
- Wild 选色、摸牌后的决策、UNO 标记、玩家断线提示可用。
- 小窗口/窄终端/Windows Terminal 下不溢出或乱码。

### M4：可靠性与发布
- 客户端断线重连，恢复身份、手牌、牌局版本。
- 心跳/超时/最大消息/入场密钥/错误码。
- Windows、Linux、macOS 二进制交叉编译并测试。
- README 含房主 LAN/ZeroTier 双 IP 加入说明和故障排查。

### 关键自动化测试表

| Case | 预期 |
|---|---|
| 顶牌红2、手牌红1/红3/蓝2/蓝3 | 前 3 张可用，蓝3不可用 |
| 非当前玩家出牌 | `NOT_YOUR_TURN`，room_revision 不变 |
| 玩家打出倒数第二张牌 | `UNO_CALLED` 恰好一次 |
| 玩家打出最后一张牌 | 正确结束游戏，无新 UNO 事件 |
| 两人局 Reverse | 同一个玩家继续行动 |
| +2/+4 被叠加 | 拒绝，不叠加 |
| Wild +4 且持有有效同色牌 | 拒绝出牌 |
| 可抽牌刚摸到合法牌 | 仅可出刚抽到的牌或跳过 |
| 第三方收到 snapshot | 看不到其他玩家手牌 |
| 断线后重连 | 收到最新完整快照，无法重复执行旧动作 |

## 12. 建议实施顺序

1. 先实现引擎和全部规则单测，不写 TUI。
2. 完成 TCP 帧、消息协议、房间单线程事件循环，写两客户端集成测试。
3. 实现 `host`/`join`，验证 LAN 和 ZeroTier 同房间联机。
4. 接入 Bubble Tea UI，完成可用牌分组与快捷键。
5. 做重连、错误反馈、安全限制、CI 跨平台发行。

## 13. 已确认决定与显式假设

**已确认**：Go 全栈、终端客户端、玩家当房主、局域网直连 + ZeroTier 外网连接、自动 UNO、自己回合将可用和不可用手牌分组、数字键选牌、`D` 摸牌、`Q` 退出。

**v1 已锁定规则**：经典 108 张牌、2–4 人、无叠加、严格约束 +4 但无挑战、摸牌后允许打刚摸的牌、断线暂停 60 秒、房主退出结束游戏、单房间、无累计积分。若产品要求“完全对齐官方 UNO”，必须先修订本文档、+4 挑战、开局特殊牌规则和相关测试，再开始对应实现。

## 14. Agent 实施计划

> 执行者要求：按任务顺序实施；每项先写失败测试，再写最小实现，通过该层全部测试后再进入下一项。不要在游戏引擎中引入网络或 TUI 依赖。若仓库尚未初始化 Git，可先执行 `git init`，但是否提交由仓库所有者决定。

### Task 1：项目骨架和领域类型

**创建文件**：`go.mod`、`cmd/uno/main.go`、`internal/game/card.go`、`internal/game/game.go`、`internal/game/game_test.go`。

1. 执行 `go mod init terminal-uno`。
2. 先写牌组数量、唯一 ID、确定性洗牌、初始数字顶牌和无数字牌报错测试。
3. 实现 `Color`、`Rank`、`Card`、`Player`、`Phase`、`Game`、注入式 `rand.Source` 和标准 108 张牌组。
4. 执行 `go test ./internal/game -run 'TestDeck|TestStart' -count=1`，预期 PASS。

### Task 2：纯规则引擎

**创建/修改文件**：`internal/game/rules.go`、`internal/game/game.go`、`internal/game/game_test.go`。

1. 为颜色/数字/动作匹配、+4 同色限制、2 人 Reverse、Skip、+2/+4、不叠加、牌堆回收、胜利和自动 UNO 写表驱动测试。
2. 为正常 `turn`、合法摸牌后的 `drawn_decision`、不可出摸牌的自动过回合、该阶段只能出新牌写状态转移测试。
3. 实现 `Start()`、`LegalMoves()`、`Apply(Action)`；所有失败返回稳定错误码并保持状态深度相等。
4. 执行 `go test ./internal/game -count=1` 和 `go test -race ./internal/game -count=1`，预期均 PASS。

### Task 3：个性化视图和隐私

**创建/修改文件**：`internal/game/view.go`、`internal/game/view_test.go`。

1. 写双玩家视图测试，断言双方只能看见自己的 `YourHand`/`PlayableIDs`。
2. 写摸牌决策隐私测试，断言 `DrawDecision` 只对行动玩家出现。
3. 写事件和历史序列化测试，断言不存在其他玩家手牌和秘密字段。
4. 实现 `BuildViewFor(playerID)`，执行 `go test ./internal/game -run 'TestBuildView|TestDrawDecisionPrivacy' -count=1`。

### Task 4：帧协议和消息契约

**创建文件**：`internal/protocol/message.go`、`internal/protocol/frame.go`、`internal/protocol/frame_test.go`、`internal/protocol/message_test.go`。

1. 测试零长度、超过 64 KiB、截断帧、粘包、未知字段、缺失必填字段和协议版本不匹配。
2. 实现 4 字节大端长度帧、请求/响应/错误/事件封套及类型验证。
3. 对敏感消息增加日志安全测试，确保格式化错误不会包含 token/key。
4. 执行 `go test ./internal/protocol -count=1` 和 `go test -race ./internal/protocol -count=1`。

### Task 5：房间事件循环、room_revision 和幂等

**创建文件**：`internal/server/room.go`、`internal/server/session.go`、`internal/server/room_test.go`。

1. 写状态改变恰好递增一次、拒绝/重复请求不递增的测试。
2. 写相同请求重放、相同 ID 不同 payload、过期 room_revision 返回个人快照、重连后重放的测试。
3. 写并发提交测试，断言只有当前合法动作成功且引擎无数据竞争。
4. 实现单 owner goroutine、每 session 256 项有界幂等缓存、单调 event ID 和个性化广播。
5. 执行 `go test ./internal/server -run 'TestRoom|TestIdempotency|TestRevision' -count=1` 和 `go test -race ./internal/server -count=1`。

### Task 6：鉴权、连接和重连

**创建/修改文件**：`internal/server/server.go`、`internal/server/session.go`、`internal/client/client.go`、`internal/client/reconnect.go` 及对应 `_test.go`。

1. 写房间密钥校验、host bootstrap 身份、token 摘要存储与成功重连轮换测试。
2. 用可控 fake clock 测试断线暂停、多人各自截止时间、完整恢复原 phase 和超时终局。
3. 实现每连接单读协程、单写协程及有界写队列；慢客户端队列满时断开该客户端，不能阻塞房间循环。
4. 用 `net.Pipe` 覆盖心跳、超时、关闭、未响应请求重发和快照恢复。
5. 执行 `go test -race ./internal/server ./internal/client -count=1`。

### Task 7：命令行与密钥 UX

**创建/修改文件**：`cmd/uno/main.go`、`internal/server/server.go`，新增相应测试。

1. 实现 `host`、`join`、`--port`、`--name`、`--room-key-file` 和 `--help`。
2. 测试文件创建/读取、Unix 权限、空密钥、非交互环境缺少密钥、缺省端口和优雅关闭。
3. 确保任何错误和日志不输出密钥；房主启动信息明确列出监听地址与连接方法。
4. 执行 `go test ./cmd/uno/... ./internal/server/... -count=1`。

### Task 8：Bubble Tea TUI

**创建文件**：`internal/tui/model.go`、`internal/tui/update.go`、`internal/tui/view.go`、`internal/tui/styles.go`、`internal/tui/view_test.go`。

1. 锁定兼容的 Bubble Tea v2 与 Lip Gloss v2 版本并运行 `go mod tidy`。
2. 用纯 `Update`/`View` 测试大厅、正常回合、选色、摸牌决策、暂停、断线、终局和帮助。
3. 测试红 2 示例、稳定原手牌顺序、多位快捷键、窄屏、无颜色模式和 Unicode/Emoji 降级。
4. 网络事件仅转换为 Tea `Msg`；所有游戏合法性仍由服务端决定。
5. 执行 `go test ./internal/tui -count=1`。

### Task 9：端到端验收和发布

**创建文件**：`internal/server/integration_test.go`、`.github/workflows/build.yml`；按实际使用补充本 README 的运行与排障说明。

1. 启动真实 loopback server 和 2–4 个客户端，覆盖开局、完整短局、非法动作、断线重连、重复请求和秘密不泄漏。
2. 执行 `go test ./... -count=1`、`go test -race ./... -count=1`、`go vet ./...`，均须成功。
3. 执行三平台构建命令；CI 至少覆盖 Linux 测试、race test 和 Windows/Linux/macOS 构建。
4. 人工验证两个独立终端的 LAN 流程；ZeroTier 验证可作为发布前人工门禁，记录实际环境和结果。

## 15. 全局工程约束

- 使用 Go 当前稳定版本，并在 `go.mod` 和 CI 中锁定最低版本；依赖必须锁入 `go.sum`。
- 所有测试不得依赖真实时间、全局随机源或公网。时间使用注入的 clock，洗牌使用注入的 RNG，网络单测使用 `net.Pipe` 或 loopback。
- 业务错误使用稳定错误码；面向用户的中文/英文文本不得作为程序分支或测试的唯一依据。
- 房间循环不得执行阻塞网络写、磁盘 IO 或休眠。慢连接、计时器和 OS 信号只向循环投递事件。
- 历史记录、幂等缓存、写队列和输入帧均必须有界；具体默认值集中为常量并可在测试中覆盖。
- 任何含 `YourHand`、`DrawDecision` 或 token 的结构不得复用为公共广播 payload。
- 除 `cmd/uno` 外的包不调用 `os.Exit`；资源关闭必须幂等，goroutine 必须能随 context 退出。

## 16. Definition of Done

仅当以下条件全部满足，v1 才可视为完成：

1. 第 11 节全部里程碑与自动化测试通过，第 14 节任务均已实现。
2. `go test ./... -count=1`、`go test -race ./... -count=1`、`go vet ./...` 无失败。
3. Windows、Linux、macOS 构建成功；至少在 Windows Terminal 和一个 Unix 终端完成人工双客户端对局。
4. 抓取一局服务端向两个客户端发送的应用层消息，确认任何客户端均未收到其他玩家手牌、重连 token 或房间密钥。
5. 模拟请求响应丢失、断线重连、慢客户端、非法帧和宽限期超时，房间无重复动作、死锁、panic 或 goroutine 泄漏。
6. README 中所有 CLI 示例与实际 `uno --help` 一致，LAN/ZeroTier、防火墙、密钥交换和常见故障均有可执行说明。
