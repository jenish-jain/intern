### Build stage
FROM golang:1.23-alpine AS build

WORKDIR /src

RUN apk add --no-cache git

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build -o /out/agent ./cmd/agent

### Runtime stage
# Uses the full golang:alpine image (not a scratch/alpine-only image) because
# quality gates (RUN_VET_BEFORE_PR, RUN_TESTS_BEFORE_PR, self-healing) shell
# out to `go vet`/`go test` against the *target* repo being modified.
FROM golang:1.23-alpine

RUN apk add --no-cache ca-certificates git

WORKDIR /app
COPY --from=build /out/agent /app/agent

# Cloud Run injects PORT; default kept for local `docker run`.
ENV PORT=8080
EXPOSE 8080

ENTRYPOINT ["/app/agent", "serve"]
