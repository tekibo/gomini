<div align="center">

  <img src="logo.jpeg" alt="gomini logo" width="160" style="border-radius: 24px; margin-bottom: 16px;" />

  # gomini

  <p><strong>High-Performance OpenAI-Compatible Bridge for Google Gemini</strong></p>

  <p>
    <a href="#features"><img src="https://img.shields.io/badge/Platform-Linux%20%7C%20macOS%20%7C%20Windows-blue?style=flat-square" alt="Platform" /></a>
    <a href="#quick-start"><img src="https://img.shields.io/badge/Go-1.23+-00ADD8?style=flat-square&logo=go" alt="Go Version" /></a>
    <a href="#docker"><img src="https://img.shields.io/badge/Docker-Ready-2496ED?style=flat-square&logo=docker" alt="Docker Ready" /></a>
    <a href="#api-endpoints"><img src="https://img.shields.io/badge/API-OpenAI%20v1%20Compatible-green?style=flat-square" alt="OpenAI API" /></a>
    <a href="#license"><img src="https://img.shields.io/badge/License-MIT-purple?style=flat-square" alt="License" /></a>
  </p>

  <p align="center">
    <code>gomini</code> transforms Google Gemini into a standard OpenAI and Google AI REST API endpoint.<br />
    Deployable as a single zero-dependency binary or lightweight Docker container.
  </p>

</div>

---

## Table of Contents

- [Overview](#overview)
- [Key Capabilities](#key-capabilities)
- [Quick Start](#quick-start)
- [Models and Reasoning Depth](#models-and-reasoning-depth)
- [Multimodal and Vision](#multimodal-and-vision)
- [Function Calling and Tool Streaming](#function-calling-and-tool-streaming)
- [Authentication and Cookie Sync](#authentication-and-cookie-sync)
- [Configuration Reference](#configuration-reference)
- [Docker Deployment](#docker-deployment)
- [Client and Agent Integrations](#client-and-agent-integrations)
- [API Endpoints](#api-endpoints)
- [Security and Network Protection](#security-and-network-protection)
- [License](#license)

---

## Overview

`gomini` is a standalone API server that bridges popular AI clients, development environments, and autonomous coding agents to Google Gemini. It exposes drop-in endpoints matching the OpenAI specification (`/v1/chat/completions`, `/v1/models`, `/v1/responses`) and Google's native API (`/v1beta/models`), enabling universal compatibility with existing tools and SDKs without code modifications.

```
┌──────────────────────────────────────────────┐
│  Clients: Cursor, OpenWebUI, Cherry Studio   │
│  Agents:  Cline, Antigravity, OpenAI SDK     │
└──────────────────────┬───────────────────────┘
                       │ HTTP / OpenAI Spec
                       ▼
┌──────────────────────────────────────────────┐
│                   gomini                     │
│  - SSE Live Streaming & Tool Buffering       │
│  - Multimodal Ingestion & SSRF Protection    │
│  - Automatic Token Scraper & Cookie Sync     │
└──────────────────────┬───────────────────────┘
                       │ Secure HTTPS
                       ▼
┌──────────────────────────────────────────────┐
│             Google Gemini Cloud              │
└──────────────────────────────────────────────┘
```

---

## Key Capabilities

- **Universal OpenAI Compatibility**: Full support for `/v1/chat/completions`, model listings, and the OpenAI Responses API (`/v1/responses`) with strict sequence numbering.
- **Multimodal Vision Ingestion**: Seamlessly accept images in Base64 data URLs (`data:image/png;base64,...`) or remote HTTPS image URLs.
- **Live Tool Streaming**: Stream prose deltas in real time while buffering and accurately assembling `tool_calls` for function executions.
- **Agent Refusal Recovery**: Built-in automated retries for canned failure strings and tutorial phrasing misdirections, ensuring autonomous agent loops never stall.
- **Adjustable Reasoning Effort**: Granular control over model thinking levels via standard OpenAI `reasoning_effort` parameters, model suffixes, or `@think` directives.
- **Automated Credential Refresh**: Automatically scrapes and refreshes security tokens (`SNlM0e`) and build labels directly from Gemini Web sessions.
- **Browser Extension Cookie Sync**: Dedicated local sync endpoint (`POST /v1/cookies`) with DNS rebinding protection for one-click browser cookie pairing.
- **Temporary Incognito Sessions**: Optional temporary chat mode to avoid polluting your Google account history.
- **Zero Runtime Dependencies**: Single compiled binary with minimal memory footprint (under 30 MB idle).

---

## Quick Start

### Native Binary

Build and launch with Go 1.23+:

```bash
# Clone and build
git clone https://github.com/tekibo/gomini.git
cd gomini
go build -o gomini ./cmd/gomini

# Run server
./gomini -port 8081
```

The server will start listening at `http://localhost:8081/v1`.

### Instant Curl Verification

Check server status:
```bash
curl -s http://localhost:8081/health
```

Perform a basic chat completion:
```bash
curl -s http://localhost:8081/v1/chat/completions \
  -H "Content-Type: application/json" \
  -d '{
    "model": "gemini-3.7-flash",
    "messages": [
      {"role": "user", "content": "Explain relativity in one sentence."}
    ]
  }'
```

---

## Models and Reasoning Depth

### Model Catalog

| Model Identifier | Architecture | Capabilities | Typical Output Limit |
|---|---|---|---|
| `gemini-4-argon` | Next-Generation Flagship | Complex reasoning, deep analysis, coding | Up to 32,000 characters |
| `gemini-4-argon-low` / `-medium` / `-high` | Flagship Thinking Variants | Tuned reasoning depth presets | Up to 32,000 characters |
| `gemini-3.8-flash` | High-Efficiency Flash | Ultra-fast responses with deep thinking | Up to 20,000 characters |
| `gemini-3.8-flash-thinking` | Extended Thinking Flash | Deep problem solving with chain of thought | Up to 25,000 characters |
| `gemini-3.7-flash` | Standard Flash | Fast general-purpose generation | Up to 15,000 characters |
| `gemini-3.6-flash` | Balanced Flash | Consistent baseline speed and accuracy | Up to 12,000 characters |
| `gemini-3.1-pro` | Advanced Reasoning Pro | Mathematics, reasoning, and coding | Up to 15,000 characters |
| `gemini-flash-lite` | Lightweight Flash | Maximum response velocity | Up to 10,000 characters |
| `gemini-auto` | Adaptive Selection | Automatically selects optimal model tier | Dynamic |

### Controlling Thinking Depth

`gomini` provides three ways to manage thinking intensity:

#### 1. Standard OpenAI `reasoning_effort` Field
Set `reasoning_effort` in your API request payload:
```json
{
  "model": "gemini-3.8-flash",
  "reasoning_effort": "high",
  "messages": [{"role": "user", "content": "Solve this equation step-by-step..."}]
}
```
Available levels: `minimal`, `low`, `medium`, `high`, `max`.

#### 2. Model Identifier Suffixes
Request models with thinking variants appended:
- `gemini-4-argon-high`
- `gemini-3.8-flash-medium`
- `gemini-3.8-flash:high`

#### 3. Inline `@think=N` Syntax
Append `@think=N` directly to any model name:
- `@think=0`: Deepest reasoning effort (maximum chain of thought)
- `@think=2`: Balanced reasoning effort
- `@think=4`: Minimal reasoning effort (lowest latency)

---

## Multimodal and Vision

Send images alongside text prompts using standard OpenAI Vision formats:

### Base64 Data URLs
```json
{
  "model": "gemini-3.7-flash",
  "messages": [
    {
      "role": "user",
      "content": [
        {"type": "text", "text": "Describe the contents of this image in detail."},
        {"type": "image_url", "image_url": {"url": "data:image/png;base64,iVBORw0KGgo..."}}
      ]
    }
  ]
}
```

### Remote Public HTTPS URLs
```json
{
  "model": "gemini-3.7-flash",
  "messages": [
    {
      "role": "user",
      "content": [
        {"type": "text", "text": "What species is shown here?"},
        {"type": "image_url", "image_url": {"url": "https://images.example.com/wildlife/photo.jpg"}}
      ]
    }
  ]
}
```

Images are processed via Google's resumable media upload channel and bound directly into the active session context. Supported raster formats include PNG, JPEG, GIF, WEBP, BMP, TIFF, AVIF, and HEIC.

---

## Function Calling and Tool Streaming

`gomini` supports structured OpenAI tool definitions and function calling:

```json
{
  "model": "gemini-3.7-flash",
  "messages": [
    {"role": "user", "content": "Check the weather forecast for Berlin."}
  ],
  "tools": [
    {
      "type": "function",
      "function": {
        "name": "get_current_weather",
        "description": "Get real-time weather information for a specified city.",
        "parameters": {
          "type": "object",
          "properties": {
            "city": {"type": "string", "description": "City name, e.g. Berlin"}
          },
          "required": ["city"]
        }
      }
    }
  ],
  "stream": true
}
```

### Tool Streaming Architecture
When `stream: true` is enabled with tools:
1. Explanatory text and assistant prose stream to the client in real time.
2. Tool call blocks are buffered internally and validated against syntax boundaries.
3. Upon completion, structured OpenAI `tool_calls` deltas are emitted with `finish_reason: "tool_calls"`.
4. If Gemini returns canned error phrases or conversational tutorial explanations, `gomini` transparently triggers automated corrective retries before completing.

---

## Authentication and Cookie Sync

While anonymous usage provides access to Flash models, advanced models (`gemini-4-argon` and `gemini-3.1-pro`) benefit from account authorization.

### 1. Browser Extension Synchronization (Recommended)
Sync active session cookies directly from your web browser without copying text files:

```bash
curl -X POST http://localhost:8081/v1/cookies \
  -H "Content-Type: application/json" \
  -H "X-Gemini-Cookie-Sync: 1" \
  -d '{
    "cookies": {
      "__Secure-1PSID": "your_secure_1psid",
      "SAPISID": "your_sapisid",
      "SID": "your_sid"
    }
  }'
```
*Note: The cookie sync endpoint enforces strict origin verification and is accessible only from localhost to safeguard credentials.*

### 2. Cookie File Formats
You can store cookies in a local file configured via `--cookie-file`:
- **Netscape HTTP Cookie format**: Exported directly from browser extensions such as *Get cookies.txt LOCALLY*. Full support for `#HttpOnly_` prefix lines and 7-column records.
- **Key-Value String format**: Semicolon-separated pairs:
  ```
  SID=xxx; HSID=xxx; SSID=xxx; APISID=xxx; SAPISID=xxx; __Secure-1PSID=xxx
  ```
- **JSON format**:
  ```json
  {
    "cookie": "SID=xxx; SAPISID=xxx; __Secure-1PSID=xxx",
    "sapisid": "xxx"
  }
  ```

### 3. Dynamic Token Scraper
`gomini` automatically scrapes the active `SNlM0e` XSRF token and runtime build label (`gemini_bl`) from Google Gemini at server startup and refreshes them dynamically if an upstream request returns HTTP 400 or 405.

---

## Configuration Reference

### Precedence Hierarchy
Configuration values are loaded using strict priority rules:
1. **Command-Line Flags** (highest priority; overrides all)
2. **Environment Variables**
3. **Configuration File** (`config.json` or path specified by `-config`)
4. **Built-in Defaults** (lowest priority)

### Configuration Schema (`config.json`)

```json
{
  "port": 8081,
  "host": "0.0.0.0",
  "retry_attempts": 3,
  "retry_delay_sec": 2,
  "request_timeout_sec": 180,
  "ticket_timeout_sec": 25,
  "idle_timeout_sec": 90,
  "gemini_bl": "boq_assistant-bard-web-server_20260723.00_p0",
  "auth_user": "0",
  "xsrf_token": null,
  "default_model": "gemini-3.7-flash",
  "api_keys": ["sk-custom-secret-key"],
  "cookie_file": "cookie.txt",
  "proxy": null,
  "log_requests": true,
  "temporary_chats": false,
  "send_model_ticket": true,
  "model_tickets": {}
}
```

### CLI Flags & Environment Variables

| CLI Flag | Environment Variable | Default | Description |
|---|---|---|---|
| `-port` | `GOMINI_PORT` | `8081` | HTTP listen port |
| `-host` | `GOMINI_HOST` | `0.0.0.0` | HTTP bind address |
| `-config` | `GOMINI_CONFIG` | `config.json` | Path to JSON configuration file |
| `-api-keys` | `GOMINI_API_KEYS` | (empty) | Comma-separated list of accepted Bearer keys |
| `-default-model` | `GOMINI_DEFAULT_MODEL` | `gemini-3.6-flash` | Default model when unspecified by client |
| `-retry-attempts` | `GOMINI_RETRY_ATTEMPTS` | `3` | Maximum retry attempts for upstream failures |
| `-retry-delay` | `GOMINI_RETRY_DELAY_SEC` | `2` | Delay between retry attempts in seconds |
| `-request-timeout` | `GOMINI_REQUEST_TIMEOUT_SEC`| `180` | Upstream generation timeout in seconds |
| `-ticket-timeout` | `GOMINI_TICKET_TIMEOUT_SEC` | `25` | Timeout for model ticket routing before fallback |
| `-idle-timeout` | `GOMINI_IDLE_TIMEOUT_SEC` | `90` | Upstream transport idle connection timeout |
| `-cookie-file` | `GOMINI_COOKIE_FILE` | (none) | Path to cookie file (Netscape or text) |
| `-proxy` | `GOMINI_PROXY` | (none) | HTTP or SOCKS5 proxy URL |
| `-log-requests` | `GOMINI_LOG_REQUESTS` | `true` | Log incoming HTTP requests to console |
| `-temporary-chats`| `GOMINI_TEMPORARY_CHATS` | `false` | Enable incognito mode (do not save to history) |
| `-send-model-ticket`| `GOMINI_SEND_MODEL_TICKET` | `true` | Send model routing ticket headers |

### Server API Authentication
When `api_keys` contains one or more keys:
- Incoming requests must include either `Authorization: Bearer <key>` or `x-api-key: <key>`.
- Header parsing is case-insensitive (accepts `Bearer`, `bearer`, or `BEARER`).
- Surrounding whitespace is automatically trimmed.
- When `api_keys` is empty (`[]`), requests are accepted without authentication.

---

## Docker Deployment

### Docker Run

```bash
# Basic container startup
docker run -d \
  --name gomini \
  -p 8081:8081 \
  -v $(pwd)/config.json:/etc/gomini/config.json \
  -v $(pwd)/cookie.txt:/etc/gomini/cookie.txt \
  ghcr.io/tekibo/gomini:latest
```

### Docker Compose

Create `compose.yml`:

```yaml
services:
  gomini:
    image: ghcr.io/tekibo/gomini:latest
    container_name: gomini
    restart: unless-stopped
    ports:
      - "8081:8081"
    volumes:
      - ./config.json:/etc/gomini/config.json:ro
      - ./cookie.txt:/etc/gomini/cookie.txt:ro
```

Run container:
```bash
docker compose up -d
```

*Tip: If you encounter network timeouts inside Docker environments, enable host networking via `network_mode: host`.*

---

## Client and Agent Integrations

`gomini` functions as a direct local replacement for OpenAI services across any AI client or agent framework.

### 1. OpenAI Python SDK
```python
from openai import OpenAI

client = OpenAI(
    base_url="http://localhost:8081/v1",
    api_key="none"  # or your configured api_key
)

response = client.chat.completions.create(
    model="gemini-4-argon",
    messages=[
        {"role": "system", "content": "You are an expert systems engineer."},
        {"role": "user", "content": "Architect a resilient distributed queue."}
    ],
    reasoning_effort="high"
)

print(response.choices[0].message.content)
```

### 2. OpenWebUI
1. Open **Admin Panel** -> **Settings** -> **Connections**.
2. Under **OpenAI API**, add:
   - **URL**: `http://localhost:8081/v1`
   - **Key**: Any placeholder string or your configured key.
3. Save and refresh model selections (`gemini-4-argon`, `gemini-3.8-flash`, etc.).

### 3. Cherry Studio / ChatBox / NextChat
- **API Format**: OpenAI
- **Base URL**: `http://localhost:8081/v1`
- **API Key**: Any string (if `api_keys` is empty) or your configured secret.
- **Model**: `gemini-4-argon`, `gemini-3.8-flash`, or `gemini-3.7-flash`.

### 4. Cline / Cursor / AI IDE Assistants
Configure custom OpenAI compatible provider:
- **Base URL**: `http://localhost:8081/v1`
- **Model ID**: `gemini-4-argon` or `gemini-3.8-flash`
- **Streaming**: Enabled

### 5. Google Gemini Native CLI
`gomini` implements native Google generative language endpoints:
```bash
export GEMINI_API_KEY=none
export GOOGLE_GEMINI_BASE_URL=http://localhost:8081
gemini
```

---

## API Endpoints

### OpenAI Compatible Routes
- `POST /v1/chat/completions`: Create streaming or non-streaming chat completions.
- `GET /v1/models`: Retrieve list of available Gemini models.
- `POST /v1/responses`: OpenAI Codex and Responses API format with strict sequential counters.

### Native Google Routes
- `GET /v1beta/models`: List Google models.
- `POST /v1beta/models/{model}:generateContent`: Synchronous content generation.
- `POST /v1beta/models/{model}:streamGenerateContent`: Server-Sent Event streaming generation.

### Management and Health
- `GET /health` or `GET /`: Health check endpoint returning version and status.
- `POST /v1/cookies`: Secure local browser cookie sync endpoint.
- `OPTIONS /*`: Global CORS preflight handler.

---

## Security and Network Protection

`gomini` contains enterprise-grade network defenses to prevent unauthorized internal network exploration:

- **Server-Side Request Forgery (SSRF) Protection**: When resolving remote images, `gomini` blocks connections to private networks (RFC 1918 `10.0.0.0/8`, `172.16.0.0/12`, `192.168.0.0/16`), loopback addresses (`127.0.0.0/8`, `::1`), link-local and cloud metadata addresses (`169.254.169.254`, `fe80::/10`), CGNAT ranges, and multicast groups.
- **Pre-Dial Verification and Socket Control**: Validates destination IP addresses both during DNS resolution and directly within the kernel socket connect phase (`net.Dialer.Control`), preventing DNS rebinding and TOCTOU vulnerabilities.
- **Download Bounds**: Remote image downloads are capped at a strict 10 MiB limit to prevent memory exhaustion.
- **Redirect Limits**: HTTP redirects are bounded to a maximum of 3 hops with mandatory address re-validation on each hop.
- **Standardized Error Schemas**: Error payloads strictly follow the OpenAI error specification (`type`, `message`, `code`, `param`).

---

## License

This project is licensed under the [MIT License](LICENSE).
