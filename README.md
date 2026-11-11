> **⚠️ DISCLAIMER: This project is for EDUCATIONAL PURPOSES ONLY.**
> It reverse-engineers an undocumented internal API. Use at your own risk.
> Not affiliated with or endorsed by Google. Misuse may violate Terms of Service.

# gomini

<p align="center">
  <img src="logo.jpeg" width="200" alt="gomini logo">
</p>

Convert Google Gemini's web interface into an OpenAI-compatible API.
Zero cost, cross-platform, single binary.

## Features

- **Optional API Keys**: no auth when `api_keys` is empty, OpenAI-style Bearer auth (case-insensitive with whitespace trimming) when configured
- **OpenAI Compatible**: Drop-in replacement for `/v1/chat/completions` and `/v1/models`
- **Responses API**: `/v1/responses` for OpenAI Codex integration with strict sequence numbers
- **Gemini CLI**: Google native API (`/v1beta/models`) for Gemini CLI compatibility
- **Multimodal Image Support**: Full support for OpenAI image inputs (data URLs and remote HTTPS URLs) uploaded via Google Scotty resumable upload with SSRF protection
- **Live Tool Calling & Streaming**: Full function calling support with live prose streaming and clean `tool_calls` delta assembly
- **Agent Refusal Recovery**: Automated retry for canned Gemini refusal phrases and tutorial misdirections
- **Latest Models**: Support for `gemini-4-argon`, `gemini-3.8-flash`, `gemini-3.7-flash`, `gemini-3.1-pro`, and thinking variants
- **Thinking Depth Controls**: Adjustable via `@think=N` suffix (0=deepest, 4=shallowest), `-low`/`-medium`/`-high` model suffixes, or OpenAI `reasoning_effort`
- **Web Search**: Built-in internet access (Gemini's native search)
- **Cookie Formats & Auto-Sync**: Netscape `cookies.txt` (including `#HttpOnly_`), JSON, or live browser extension sync via `POST /v1/cookies`
- **Dynamic BL & XSRF Refresh**: Automatically scrapes latest build labels and XSRF tokens from `gemini.google.com/app`
- **Temporary Chats**: Optional incognito mode (`temporary_chats: true`) to avoid polluting Google account history
- **Single binary**: Pure Go, zero external dependencies
- **Cross-Platform**: Linux, macOS, Windows (Docker or native binary)

## Quick Start

```bash
go build -o gomini ./cmd/gomini
./gomini
```

Server starts at `http://localhost:8081/v1`.

## Client Configuration

### Cherry Studio / ChatBox / any OpenAI client

| Field    | Value                                                               |
| -------- | ------------------------------------------------------------------- |
| Base URL | `http://localhost:8081/v1`                                          |
| API Key  | any `api_keys` value from `config.json`; anything if not configured |
| Model    | `gemini-3.6-flash`                                                  |

### curl

```bash
curl http://localhost:8081/v1/chat/completions \
  -H "Content-Type: application/json" \
  -d '{"model":"gemini-3.6-flash","messages":[{"role":"user","content":"Hello!"}]}'
```

### OpenAI Python SDK

```python
from openai import OpenAI
client = OpenAI(base_url="http://localhost:8081/v1", api_key="sk-your-key")
resp = client.chat.completions.create(
    model="gemini-3.6-flash",
    messages=[{"role": "user", "content": "Explain quantum computing"}]
)
print(resp.choices[0].message.content)
```

### Gemini CLI

```bash
export GEMINI_API_KEY=none
export GOOGLE_GEMINI_BASE_URL=http://localhost:8081
gemini
```

Supports Google native API endpoints:

- `GET /v1beta/models` — list models
- `POST /v1beta/models/{model}:generateContent` — non-streaming
- `POST /v1beta/models/{model}:streamGenerateContent` — streaming (SSE)

## Available Models

| Model | Description | Output |
|---|---|---|
| `gemini-4-argon` | Next-generation flagship model (needs model ticket/cookie) | ~15k chars |
| `gemini-4-argon-low` / `-medium` / `-high` | Argon with thinking depth variants | ~15k-25k chars |
| `gemini-3.8-flash` | High-efficiency fast model with deep thinking | ~12k chars |
| `gemini-3.8-flash-thinking` | Extended thinking mode for 3.8 Flash | **~25k chars** |
| `gemini-3.7-flash` | Standard Gemini Flash model | ~12k chars |
| `gemini-3.6-flash` | All-around balanced model | ~12k chars |
| `gemini-3.1-pro` | Advanced math & reasoning (needs cookie) | ~12k chars |
| `gemini-auto` | Automatic model selection | varies |
| `gemini-flash-lite` | Fastest lightweight answers | ~10k chars |

### Thinking Depth Controls

You can control reasoning effort and thinking depth in 3 convenient ways:

1. **OpenAI `reasoning_effort` parameter**:
   Set `"reasoning_effort": "low"`, `"medium"`, or `"high"` in your chat request payload.
2. **Model Name Suffixes**:
   Append `-low`, `-medium`, or `-high` directly to model names (e.g. `gemini-4-argon-high`, `gemini-3.8-flash:high`).
3. **Explicit `@think=N` tag**:
   ```
   gemini-3.8-flash@think=0      # deepest thinking (high)
   gemini-3.8-flash@think=2      # balanced thinking (medium)
   gemini-3.8-flash@think=4      # fast / minimal thinking (low)
   ```

## Multimodal Input (Images)

`gomini` supports image inputs formatted according to the OpenAI Vision specification:
- **Base64 Data URLs**: `data:image/png;base64,...`
- **Remote HTTPS Image URLs**: `https://example.com/photo.jpg`

Images are securely uploaded via Google's Scotty resumable upload protocol and bound to the conversation.

### SSRF & Security Protections
For remote image URLs, `gomini` includes strict SSRF defenses:
- Rejects non-routable, private, loopback (`127.0.0.1`, `localhost`), and link-local cloud metadata (`169.254.169.254`) IP addresses.
- Enforces pre-dial DNS validation and socket-level connection checks (`net.Dialer.Control`) to prevent DNS rebinding attacks.
- Enforces a maximum download limit of **10 MiB** and a maximum of **3 HTTP redirects**.
- Validates image magic bytes (PNG, JPEG, GIF, WEBP, BMP, TIFF, AVIF, HEIC).

## Cookies & Authentication for Pro / Argon

Anonymous access works for Flash models, but `gemini-4-argon` and `gemini-3.1-pro` require authenticated Google cookies.

### 1. Browser Extension Auto-Sync (Recommended)
You can sync cookies automatically from your browser using the local sync endpoint:
```bash
POST /v1/cookies
Headers: X-Gemini-Cookie-Sync: 1 (or chrome-extension:// Origin)
Body: {"cookie": "SID=...; SAPISID=..."}
```
This endpoint includes DNS rebinding protection and binds only to localhost.

### 2. Netscape `cookies.txt` or Text Format
Export cookies using any standard browser extension (like *Get cookies.txt LOCALLY*) in Netscape format (`# Netscape HTTP Cookie File` or `#HttpOnly_` lines). Pass the file with:
```bash
./gomini --cookie-file cookie.txt
```

### 3. Dynamic BL & XSRF Token Refresh
On startup and automatically on HTTP 400/405 errors, `gomini` fetches fresh `SNlM0e` XSRF tokens and build labels (`gemini_bl`) directly from `https://gemini.google.com/app`.

## Configuration

Configuration is loaded in strict precedence order:

1. **CLI flags**: `--port`, `--host`, `--config`, etc. (highest precedence)
2. **Environment variables**: `GOMINI_PORT`, `GOMINI_HOST`, `GOMINI_TEMPORARY_CHATS`, etc.
3. **Config file**: `--config custom.json` (or `GOMINI_CONFIG`, or auto-detected `config.json` in working directory)
4. **Built-in Defaults**: Port 8081, Host 0.0.0.0, etc.

Create `config.json`:

```json
{
  "port": 8081,
  "host": "0.0.0.0",
  "retry_attempts": 3,
  "retry_delay_sec": 2,
  "request_timeout_sec": 180,
  "gemini_bl": "boq_assistant-bard-web-server_20260723.00_p0",
  "auth_user": null,
  "xsrf_token": null,
  "default_model": "gemini-3.6-flash",
  "api_keys": [],
  "cookie_file": null,
  "proxy": null,
  "log_requests": true,
  "temporary_chats": false,
  "send_model_ticket": true,
  "ticket_timeout_sec": 25,
  "idle_timeout_sec": 90,
  "model_tickets": {}
}
```

When `api_keys` is `[]`, authentication is disabled. When one or more keys are set, `/v1/*` endpoints require `Authorization: Bearer <key>` (case-insensitive) or `x-api-key: <key>`.

### CLI flags

```
--port                HTTP listen port (default: 8081)
--host                HTTP listen address (default: 0.0.0.0)
--config              Path to config JSON file
--retry-attempts      Number of retries for upstream requests (default: 3)
--retry-delay         Delay between retries in seconds (default: 2)
--request-timeout     Upstream request timeout in seconds (default: 180)
--ticket-timeout      Timeout in seconds for model ticket requests (default: 25)
--idle-timeout        Idle timeout in seconds for upstream connections (default: 90)
--gemini-bl           Gemini build label (bl parameter)
--default-model       Default model name (default: gemini-3.6-flash)
--cookie-file         Path to cookie file (supports Netscape format)
--proxy               HTTP proxy URL
--auth-user           Google auth user index
--xsrf-token          XSRF token
--log-requests        Enable request logging (default: true)
--temporary-chats     Use temporary/incognito chats without Google history (default: false)
--send-model-ticket   Send X-Goog-Ext-525001261-Jspb model routing header (default: true)
```

## Docker

```bash
cp config.example.json config.json
docker build -t gomini .
docker run -d --name gomini -p 8081:8081 -v ./config.json:/etc/gomini/config.json gomini
```

Or use Docker Compose:

```bash
cp config.example.json config.json
docker compose up -d
```

To mount a cookie file:

```bash
docker run -d --name gomini -p 8081:8081 \
  -v ./config.json:/etc/gomini/config.json \
  -v ./cookie.txt:/etc/gomini/cookie.txt \
  gomini
```

Set `"cookie_file": "/etc/gomini/cookie.txt"` in `config.json`.

> **Note**: If you get empty responses (`content: null`) with Docker's default bridge network, switch to host networking: `docker run --network host ...` or add `network_mode: host` in your compose file. This is caused by Gemini's upstream rejecting requests from certain Docker NAT IP ranges.

## Proxy

If you cannot access `gemini.google.com` directly (connection timeout), configure a proxy:

**CLI flag:**

```bash
./gomini --proxy http://127.0.0.1:7890
```

**config.json:**

```json
{ "proxy": "http://127.0.0.1:7890" }
```

**Environment variable:**

```bash
export GOMINI_PROXY=http://127.0.0.1:7890
./gomini
```

Works with Clash, V2Ray, Shadowsocks, or any HTTP proxy.

## Tool Calling

```python
resp = client.chat.completions.create(
    model="gemini-3.6-flash",
    messages=[{"role": "user", "content": "What's the weather in Tokyo?"}],
    tools=[{
        "type": "function",
        "function": {
            "name": "get_weather",
            "description": "Get weather for a city",
            "parameters": {"type": "object", "properties": {"city": {"type": "string"}}, "required": ["city"]}
        }
    }]
)
```

## Agent & IDE Setup (Issue #108 / #107)

`gomini` acts as a drop-in local OpenAI endpoint for AI coding agents and developer environments:

- **Cherry Studio / Chatbox / NextChat**: Set Provider to OpenAI, Base URL to `http://localhost:8081/v1`, and choose `gemini-4-argon` or `gemini-3.8-flash`.
- **OpenWebUI**: Add an OpenAI connection pointing to `http://localhost:8081/v1`.
- **Antigravity CLI / Cursor / Cline**: Configure OpenAI-compatible provider with base URL `http://localhost:8081/v1`.

## Limitations

- **Not real Pro/Argon without Account**: Without an authorized Gemini cookie, requests for `gemini-4-argon` or `gemini-3.1-pro` automatically fall back to `gemini-3.7-flash` or `gemini-3.6-flash`.
- **Single-turn web channel**: Multi-turn history is sent as full context in each request payload.
- **Google Rate Limits**: High-frequency requests may encounter rate limits or temporary IP challenges from Google Web.

## How It Works

This tool bridges OpenAI-compatible API requests to Google Gemini's web StreamGenerate protocol. It authenticates with Google via session cookies and SAPISID hashes, builds JSPB payload arrays, maps model routing via the `X-Goog-Ext-525001261-Jspb` header ticket mechanism (with automatic fallback on rejection), and streams Server-Sent Events (SSE) back to clients in standard OpenAI format.

Building from source requires Go 1.23+. The compiled binary has zero external runtime dependencies.

---

> **⚠️ DISCLAIMER: This project is for EDUCATIONAL PURPOSES ONLY.**
> It reverse-engineers an undocumented internal API. Use at your own risk.
> Not affiliated with or endorsed by Google. Misuse may violate Terms of Service.
