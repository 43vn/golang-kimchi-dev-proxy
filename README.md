# Kimchi

Proxy HTTP viết bằng Go, làm cầu nối giữa **Anthropic Messages API** và một upstream tương thích **OpenAI** (`https://llm.kimchi.dev/openai/v1/chat/completions`). Server tự dịch qua lại giữa hai định dạng request/response (kể cả SSE streaming) và chuyển tiếp API key của client xuống upstream.

## Tính năng

- `POST /v1/chat/completions` — proxy thẳng định dạng OpenAI, đồng thời chèn thêm system message `"You are Kimchi, an AI coding agent"` (`role: developer`) vào đầu mảng `messages`.
- `POST /v1/messages` — Anthropic Messages API:
  - Dịch Anthropic request → OpenAI, gọi upstream, dịch OpenAI response → Anthropic.
  - Hỗ trợ cả streaming (SSE kiểu Anthropic) và non-streaming.
  - Phát hiện response upstream không phải JSON/SSE hợp lệ (ví dụ XML, HTML, mảng thô) và trả về `502 Bad Gateway` thay vì nuốt im lặng.
  - Synthesize `[DONE]` nếu upstream đóng stream mà không có marker.
  - Heartbeat keep-alive mỗi 30 giây.
- `POST /v1/messages/count_tokens` — ước lượng số token (xem chi tiết bên dưới).
- `GET /v1/models` — danh sách mô hình hợp nhất giữa OpenAI và Anthropic.
- Middleware `stripDoubleV1` gỡ các tiền tố `/v1/v1` lặp lại để tránh 404 khi một số client chèn thừa `/v1`.

## Cấu trúc thư mục

```
.
├── main.go                       # Entry point, khai báo routes gin
├── handler.go                    # Handlers Anthropic: /v1/messages, count_tokens, /v1/models, /healthz
├── stream_executor.go            # Kiến trúc streaming channel-based (forward SSE)
├── internal/
│   ├── translator/claude/        # Chuyển đổi Anthropic ↔ OpenAI (request + response, stream + non-stream)
│   ├── translator/common/        # Kiểu dữ liệu dùng chung cho translator
│   ├── signature/                # Hỗ trợ ký header upstream (provider.go)
│   ├── thinking/                 # Extended thinking conversion
│   └── util/                     # Hàm tiện ích
├── Makefile                      # build / run / test / lint / install-service
├── Containerfile                 # Image cho Docker/Podman
├── kimchi.service                # systemd user unit
├── plans/                        # Plan files theo workflow đa-agent
└── skills/, agents/, .agents/,   # Hạ tầng AI agent (anti-hallucination, TDD, council-voter, ...)
    .claude/, .codex/, .opencode/
```

Lưu ý: handler `healthzHandler` đã được định nghĩa trong `handler.go` nhưng **chưa được đăng ký** trong router của `main.go`. Bạn cần tự thêm `r.GET("/healthz", healthzHandler)` nếu muốn dùng.

## Yêu cầu

- **Go ≥ 1.25** (xem `go.mod`).
- Tùy chọn:
  - `golangci-lint` cho `make lint`.
  - `air` cho `make dev` (hot reload).
  - systemd user instance cho `make install-service`.
- Để chạy container: Docker hoặc Podman.

## Build & Run

### Build nhị phân

```bash
make build              # tạo ./bin/kimchi
```

`version` được inject lúc build qua `-ldflags "-X main.version=$(git describe --tags --always --dirty)"`. Nếu không có git tag, sẽ là `dev`.

### Chạy local

```bash
make run                # build rồi chạy ở :18998
make run-dev            # go run trực tiếp, :18998
make dev                # hot reload với air
```

Cổng có thể đổi bằng biến môi trường:

```bash
SERVER_ADDR=:9000 ./bin/kimchi
```

Hoặc bằng cờ CLI `--port` (alias `--addr`):

```bash
./bin/kimchi --port 9000        # tự thêm tiền tố ":"
./bin/kimchi --port :9000
./bin/kimchi --addr 0.0.0.0:9000
```

Thứ tự ưu tiên: `--port` / `--addr` > `SERVER_ADDR` env > `:18998`.

### Chạy với container

```bash
docker build -f Containerfile -t kimchi .
docker run --rm -p 18998:18998 kimchi
```

Image chạy trên Alpine, lắng nghe `:18998` (default của server). Đổi cổng host bằng `docker run --rm -p <host>:18998 kimchi --port :18998`.

## Biến môi trường

| Biến         | Mặc định | Mô tả                                            |
| ------------ | -------- | ------------------------------------------------ |
| `SERVER_ADDR`| `:18998` | Địa chỉ lắng nghe của server                    |

## API

### Authentication

Mọi request đến các endpoint Anthropic đều cần API key, truyền qua một trong hai header:

- `Authorization: Bearer <key>`
- `x-api-key: <key>`

Endpoint OpenAI (`/v1/chat/completions`) chuyển nguyên header `Authorization` của client xuống upstream. Endpoint Anthropic (`/v1/messages`, `/v1/messages/count_tokens`) sẽ tự chuẩn hóa thành `Bearer <key>` trước khi gọi upstream.

### `POST /v1/chat/completions` (OpenAI)

```bash
curl -X POST http://localhost:18998/v1/chat/completions \
  -H "Authorization: Bearer $API_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "minimax-m3",
    "messages": [{"role": "user", "content": "Hello"}]
  }'
```

Kimchi đọc body, chèn một system message vào đầu `messages`, rồi proxy nguyên xi sang upstream. Response (kể cả SSE streaming) được flush thẳng về client.

### `POST /v1/messages` (Anthropic)

- Body phải có `model` (string) — thiếu trả về `400 invalid_request_error`.
- `stream: true` → response dạng SSE Anthropic.
- `stream: false` (mặc định) → response JSON hoàn chỉnh.

Streaming:

```bash
curl -N -X POST http://localhost:18998/v1/messages \
  -H "x-api-key: $API_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "claude-sonnet-4-20250514",
    "max_tokens": 1024,
    "stream": true,
    "messages": [{"role": "user", "content": "Hello"}]
  }'
```

Non-streaming:

```bash
curl -X POST http://localhost:18998/v1/messages \
  -H "x-api-key: $API_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "claude-sonnet-4-20250514",
    "max_tokens": 1024,
    "messages": [{"role": "user", "content": "Hello"}]
  }'
```

Lỗi trả về theo schema `AnthropicErrorResponse` (`{"error":{"type":"...","message":"..."}}`).

### `POST /v1/messages/count_tokens`

Hiện **không forward** xuống upstream — server ước lượng cục bộ bằng cách cộng `len(content)` của các message rồi chia 4 (tối thiểu 10). Hữu ích để smoke test; không nên dùng cho tính billing.

```json
{ "input_tokens": 42 }
```

### `GET /v1/models`

Trả về JSON hợp nhất các model OpenAI (`minimax-m3`, `kimi-k2.7`, `deepseek-v4-flash`) và Anthropic (`claude-sonnet-4-20250514`, `claude-opus-4-20250514`, `claude-3-5-sonnet-20241022`, `claude-3-5-haiku-20241022`, `claude-3-opus-20240229`).

## Phát triển

### Test & lint

```bash
make test          # go test ./... -count=1 -v
make test-short    # go test ./... -count=1
make vet
make lint          # golangci-lint (nếu đã cài)
```

### Cleanup

```bash
make clean         # xoá ./bin
```

### Cài đặt systemd user service

```bash
make install-service          # copy unit, enable + restart
systemctl --user status kimchi.service
journalctl --user -u kimchi.service -f

make uninstall-service        # stop, disable, xoá unit
```

Service lắng nghe `:18998`, `Restart=on-failure` sau 5s. `WorkingDirectory` được hard-code trong `kimchi.service` — nếu bạn đổi cổng hoặc đường dẫn, sửa file này trước khi `make install-service`.

## Ghi chú vận hành

- Upstream URL được hard-code trong `main.go` (`upstreamURL`). Đổi upstream = sửa hằng số rồi build lại.
- Khi client request `Authorization: Bearer ...`, header đó được forward nguyên văn xuống upstream — không có cơ chế rotate key phía server.
- Khi upstream trả về 2xx với body không phải JSON/SSE, server sẽ trả `502` thay vì JSON rỗng (xem `handleNonStreamingResponse` và `handleStreamingResponse` trong `stream_executor.go`).
- Project sử dụng workflow đa-agent (Claude/Codex/OpenCode) với các kỹ năng bắt buộc trong `skills/` — xem `AGENTS.md` để biết quy trình khi đóng góp.
