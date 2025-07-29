# Docker-Tester

Test your Docker containers with simple YAML configuration files.

## What It Does

- Tests Docker containers by starting them and checking if they work
- Run multiple tests at the same time
- Wait for containers to be ready before testing
- Check HTTP responses, ports, and logs

## Getting Started

1. **Setup**:
   ```bash
   git clone <repository-url>
   cd dockert
   go mod tidy
   ```

2. **Run tests**:
   ```bash
   go run main.go test-config.yaml
   ```

3. **Stop tests**: Press `Ctrl+C`

## Basic Example

```yaml
tests:
  - name: "nginx-test"
    image: "nginx:alpine"
    exposed_port: "80/tcp"
    wait_for:
      strategy: "http"
      target: "/"
      timeout: 30
    assertions:
      - type: "http_status"
        target: "/"
        expected: "200"
```

## Wait Strategies

**HTTP** - Wait for a web page:
```yaml
wait_for:
  strategy: "http"
  target: "/health"
  timeout: 30
```

**Port** - Wait for a port to open:
```yaml
wait_for:
  strategy: "port" 
  target: "8080/tcp"
  timeout: 30
```

**Log** - Wait for text in logs:
```yaml
wait_for:
  strategy: "log"
  target: "Server started"
  timeout: 60
```

## Test Types

**Check HTTP status**:
```yaml
assertions:
  - type: "http_status"
    target: "/api/health"
    expected: "200"
```

**Check if text exists**:
```yaml
assertions:
  - type: "http_body_contains"
    target: "/api/info"
    expected: "version"
```

**Check if port is open**:
```yaml
assertions:
  - type: "port_open"
    target: "3306/tcp"
    expected: "true"
```

## More Examples

**Database test**:
```yaml
tests:
  - name: "postgres-test"
    image: "postgres:13-alpine"
    exposed_port: "5432/tcp"
    environment:
      POSTGRES_PASSWORD: "testpass"
    wait_for:
      strategy: "log"
      target: "database system is ready"
      timeout: 60
    assertions:
      - type: "port_open"
        target: "5432/tcp"
        expected: "true"
```

**Web server test**:
```yaml
tests:
  - name: "web-test"
    image: "nginx:alpine"
    exposed_port: "80/tcp"
    wait_for:
      strategy: "http"
      target: "/"
      timeout: 30
    assertions:
      - type: "http_status"
        target: "/"
        expected: "200"
```

## Requirements

- Go 1.23+
- Docker

## Installation

```bash
go mod tidy
```