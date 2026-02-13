# Sprite API Reference

## Base URL
- Management: `https://api.sprites.dev/v1/sprites`
- Per-sprite: `https://api.sprites.dev/v1/sprites/{name}`

## Authentication
`Authorization: Bearer {token}`

---

## SPRITES (Management)

### Create
```
POST /v1/sprites
Body: {"name":"my-sprite"}
Response: Sprite
```

### List
```
GET /v1/sprites
Query: prefix={prefix}
Response: {"sprites":[Sprite...]}
```

### Get
```
GET /v1/sprites/{name}
Response: Sprite
```

### Update
```
PUT /v1/sprites/{name}
Body: {"url_settings":{"auth":"public"}}
```

### Delete
```
DELETE /v1/sprites/{name}
```

### Sprite Object
```json
{
  "id": "sprite-uuid",
  "name": "my-sprite",
  "organization": "org-slug",
  "status": "running",
  "created_at": "2026-01-07T00:26:56Z",
  "updated_at": "2026-01-07T00:26:57Z",
  "url": "https://my-sprite-xyz.sprites.app",
  "url_settings": {"auth": "sprite"}
}
```
Status: `running`, `stopped`, `suspended`, `error`, `failed`
Auth: `sprite` (default, requires token), `public`

---

## EXEC

### Execute Command (WebSocket)
```
WSS /exec?cmd={cmd}&cmd={arg1}...
```
Query params: `cmd` (repeated), `id`, `path`, `tty`, `stdin`, `cols`, `rows`, `max_run_after_disconnect`, `env`

Binary protocol:
- `0x00` stdin
- `0x01` stdout
- `0x02` stderr
- `0x03` exit
- `0x04` stdin EOF

JSON messages:
```json
{"type":"session_info","session_id":"...","command":[],"created":"...","cols":80,"rows":24,"is_owner":true,"tty":true}
{"type":"exit","exit_code":0}
{"type":"resize","cols":80,"rows":24}
{"type":"port_notification","port":8080,"address":"...","pid":123}
```

### Execute Command (HTTP)
```
POST /exec?cmd={cmd}&path={path}&stdin={bool}&env={k=v}&dir={dir}
Body: raw stdin bytes
Response: raw stdout bytes
```

### List Sessions
```
GET /exec
Response: SessionInfo[]
```

### Attach to Session
```
WSS /exec/{session_id}
```

### Kill Session
```
POST /exec/{session_id}/kill?signal={signal}&timeout={duration}
Response: NDJSON stream
```
Events: `ExecKillSignalEvent`, `ExecKillTimeoutEvent`, `ExecKillExitedEvent`, `ExecKillKilledEvent`, `ExecKillErrorEvent`, `ExecKillCompleteEvent`

---

## CHECKPOINT

### Create
```
POST /checkpoint
Body: {"comment": "..."}
Response: NDJSON stream -> StreamInfoEvent, StreamErrorEvent, StreamCompleteEvent
```

### List
```
GET /checkpoints
Response: CheckpointInfo[]
```

### Get
```
GET /checkpoints/{id}
Response: CheckpointInfo
```
```json
{"id":"...","create_time":"...","source_id":"...","comment":"..."}
```

### Restore
```
POST /checkpoints/{id}/restore
Response: NDJSON stream
```

### Delete
```
DELETE /checkpoints/{id}
```

---

## PROXY

### TCP Tunnel
```
WSS /proxy
First message: {"host":"localhost","port":8080}
Subsequent: raw TCP bytes (no framing)
```

---

## POLICY

### Network Policy
```
GET /policy/network
POST /policy/network
Body: {"rules":[{"domain":"*.example.com","action":"allow"},{"include":"github"}]}
```
Actions: `allow`, `deny`
Presets (include): `github`, etc.

### Privileges Policy
```
GET /policy/privileges
POST /policy/privileges
DELETE /policy/privileges
```

---

## SERVICES

### List
```
GET /services
Response: ServiceResponse[]
```

### Get
```
GET /services/{name}
Response: ServiceResponse
```
```json
{"name":"web","cmd":"python","args":["-m","http.server"],"needs":[],"http_port":8000,"state":"running"}
```

### Create/Update
```
PUT /services/{name}?duration={duration}
Body: {"cmd":"python","args":["-m","http.server","8000"],"needs":["postgres"],"http_port":8000}
Response: NDJSON stream
```

### Start
```
POST /services/{name}/start?duration={duration}
Response: NDJSON stream
```

### Stop
```
POST /services/{name}/stop?timeout={timeout}
Response: NDJSON stream
```

### Delete
```
DELETE /services/{name}
```

### Logs
```
GET /services/{name}/logs?lines={n}&duration={duration}
Response: NDJSON stream
```

Service log events:
```json
{"type":"stdout","data":"...","timestamp":"..."}
{"type":"stderr","data":"...","timestamp":"..."}
{"type":"exit","exit_code":0,"timestamp":"..."}
{"type":"started","timestamp":"..."}
{"type":"stopping","timestamp":"..."}
{"type":"stopped","exit_code":0,"timestamp":"..."}
{"type":"error","data":"...","timestamp":"..."}
{"type":"complete","log_files":[],"timestamp":"..."}
```

---

## FS (Preview)

### Read
```
GET /fs/read?path={path}&workingDir={dir}
Response: raw file bytes
Supports: Range headers
```

### Write
```
PUT /fs/write?path={path}&workingDir={dir}&mode={mode}&mkdir={bool}
Body: raw file bytes
Response: {"path":"...","size":1234,"mode":"0644"}
```

### List
```
GET /fs/list?path={path}&workingDir={dir}
Response: {"path":"...","entries":[...],"count":5}
```
Entry:
```json
{"name":"file.txt","path":"/full/path","type":"file","size":1234,"mode":"0644","modTime":"...","isDir":false}
```

### Delete
```
DELETE /fs/delete
Body: {"path":"/file","workingDir":"/","recursive":false,"asRoot":false}
Response: {"deleted":["/file"],"count":1}
```

### Rename
```
POST /fs/rename
Body: {"source":"/old","dest":"/new","workingDir":"/","asRoot":false}
Response: {}
```

### Copy
```
POST /fs/copy
Body: {"source":"/src","dest":"/dst","workingDir":"/","recursive":true,"preserveAttrs":true,"asRoot":false}
Response: {"copied":[...],"count":5,"totalBytes":12345}
```

### Chmod
```
POST /fs/chmod
Body: {"path":"/file","workingDir":"/","mode":"0755","recursive":false,"asRoot":false}
Response: {"affected":["/file"],"count":1}
```

### Chown
```
POST /fs/chown
Body: {"path":"/file","workingDir":"/","uid":1000,"gid":1000,"recursive":false,"asRoot":false}
Response: {"affected":["/file"],"count":1}
```

### Watch (WebSocket)
```
WSS /fs/watch
```
Messages:
```json
{"type":"subscribe","paths":["/dir"],"recursive":true,"workingDir":"/"}
{"type":"unsubscribe","paths":["/dir"]}
{"type":"subscribed","paths":["/dir"]}
{"type":"event","event":"write","path":"/dir/file","timestamp":"...","size":1234,"isDir":false}
{"type":"error","message":"...","path":"/dir"}
```
Event types: `write`, `create`, `remove`, `rename`, `chmod`

---

## Types

### StreamEvent (NDJSON)
```json
{"type":"info","data":"message","time":"..."}
{"type":"error","error":"message","time":"..."}
{"type":"complete","data":"...","time":"..."}
```

### Error Response
```json
{"error":"message","code":"ERR_CODE","path":"/optional/path"}
```
