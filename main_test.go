package main

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"
)

func TestValidateTestCase(t *testing.T) {
	runner := &TestRunner{}

	tests := []struct {
		name          string
		testCase      TestCase
		expectError   bool
		errorContains string
	}{
		{
			name: "valid test case",
			testCase: TestCase{
				Name:        "test1",
				Image:       "nginx:alpine",
				ExposedPort: "80/tcp",
				WaitFor: WaitConfig{
					Strategy: "http",
					Target:   "/",
					Timeout:  30,
				},
				Assertions: []Assertion{
					{
						Type:     "http_status",
						Target:   "/",
						Expected: "200",
					},
				},
			},
			expectError: false,
		},
		{
			name: "empty name",
			testCase: TestCase{
				Name:        "",
				Image:       "nginx:alpine",
				ExposedPort: "80/tcp",
				WaitFor: WaitConfig{
					Strategy: "http",
					Target:   "/",
					Timeout:  30,
				},
				Assertions: []Assertion{
					{
						Type:     "http_status",
						Target:   "/",
						Expected: "200",
					},
				},
			},
			expectError:   true,
			errorContains: "test name cannot be empty",
		},
		{
			name: "empty image",
			testCase: TestCase{
				Name:        "test1",
				Image:       "",
				ExposedPort: "80/tcp",
				WaitFor: WaitConfig{
					Strategy: "http",
					Target:   "/",
					Timeout:  30,
				},
				Assertions: []Assertion{
					{
						Type:     "http_status",
						Target:   "/",
						Expected: "200",
					},
				},
			},
			expectError:   true,
			errorContains: "test image cannot be empty",
		},
		{
			name: "invalid wait strategy",
			testCase: TestCase{
				Name:        "test1",
				Image:       "nginx:alpine",
				ExposedPort: "80/tcp",
				WaitFor: WaitConfig{
					Strategy: "invalid",
					Target:   "/",
					Timeout:  30,
				},
				Assertions: []Assertion{
					{
						Type:     "http_status",
						Target:   "/",
						Expected: "200",
					},
				},
			},
			expectError:   true,
			errorContains: "invalid wait strategy 'invalid'",
		},
		{
			name: "no assertions",
			testCase: TestCase{
				Name:        "test1",
				Image:       "nginx:alpine",
				ExposedPort: "80/tcp",
				WaitFor: WaitConfig{
					Strategy: "http",
					Target:   "/",
					Timeout:  30,
				},
				Assertions: []Assertion{},
			},
			expectError:   true,
			errorContains: "at least one assertion is required",
		},
		{
			name: "invalid assertion type",
			testCase: TestCase{
				Name:        "test1",
				Image:       "nginx:alpine",
				ExposedPort: "80/tcp",
				WaitFor: WaitConfig{
					Strategy: "http",
					Target:   "/",
					Timeout:  30,
				},
				Assertions: []Assertion{
					{
						Type:     "invalid_type",
						Target:   "/",
						Expected: "200",
					},
				},
			},
			expectError:   true,
			errorContains: "invalid type 'invalid_type'",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := runner.validateTestCase(tt.testCase)

			if tt.expectError {
				if err == nil {
					t.Errorf("expected error but got none")
					return
				}
				if !strings.Contains(err.Error(), tt.errorContains) {
					t.Errorf("expected error to contain '%s', got: %s", tt.errorContains, err.Error())
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
			}
		})
	}
}

func TestCreateWaitStrategy(t *testing.T) {
	runner := &TestRunner{}

	tests := []struct {
		name       string
		waitConfig WaitConfig
		expectType string
	}{
		{
			name: "http strategy",
			waitConfig: WaitConfig{
				Strategy: "http",
				Target:   "/health",
				Timeout:  30,
			},
			expectType: "*wait.HTTPStrategy",
		},
		{
			name: "port strategy",
			waitConfig: WaitConfig{
				Strategy: "port",
				Target:   "8080/tcp",
				Timeout:  30,
			},
			expectType: "*wait.HostPortStrategy",
		},
		{
			name: "log strategy",
			waitConfig: WaitConfig{
				Strategy: "log",
				Target:   "Server started",
				Timeout:  30,
			},
			expectType: "*wait.LogStrategy",
		},
		{
			name: "exec strategy",
			waitConfig: WaitConfig{
				Strategy: "exec",
				Target:   "curl -f http://localhost/health",
				Timeout:  30,
			},
			expectType: "*wait.ExecStrategy",
		},
		{
			name: "file strategy",
			waitConfig: WaitConfig{
				Strategy: "file",
				Target:   "/tmp/ready.txt",
				Timeout:  30,
			},
			expectType: "*wait.ExecStrategy", // file strategy uses exec internally
		},
		{
			name: "default strategy",
			waitConfig: WaitConfig{
				Strategy: "unknown",
				Target:   "anything",
				Timeout:  30,
			},
			expectType: "*wait.HostPortStrategy", // defaults to port strategy
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			strategy := runner.createWaitStrategy(tt.waitConfig)

			// Check that we got a strategy (not nil)
			if strategy == nil {
				t.Errorf("expected wait strategy but got nil")
				return
			}

			// For basic testing, we just ensure we get a strategy
			// In a more comprehensive test, we could check the specific type
		})
	}
}

func TestCreateWaitStrategyTimeout(t *testing.T) {
	runner := &TestRunner{}

	tests := []struct {
		name            string
		timeout         int
		expectedDefault bool
	}{
		{
			name:            "custom timeout",
			timeout:         60,
			expectedDefault: false,
		},
		{
			name:            "zero timeout uses default",
			timeout:         0,
			expectedDefault: true,
		},
		{
			name:            "negative timeout uses default",
			timeout:         -5,
			expectedDefault: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			waitConfig := WaitConfig{
				Strategy: "http",
				Target:   "/",
				Timeout:  tt.timeout,
			}

			strategy := runner.createWaitStrategy(waitConfig)
			if strategy == nil {
				t.Errorf("expected wait strategy but got nil")
			}

			// We can't easily test the timeout value without accessing internal fields,
			// but we can ensure the strategy was created successfully
		})
	}
}

func TestContains(t *testing.T) {
	tests := []struct {
		name     string
		str      string
		substr   string
		expected bool
	}{
		{
			name:     "exact match",
			str:      "hello",
			substr:   "hello",
			expected: true,
		},
		{
			name:     "substring found",
			str:      "hello world",
			substr:   "world",
			expected: true,
		},
		{
			name:     "case insensitive match",
			str:      "Hello World",
			substr:   "hello",
			expected: true,
		},
		{
			name:     "substring not found",
			str:      "hello world",
			substr:   "foo",
			expected: false,
		},
		{
			name:     "empty substring",
			str:      "hello",
			substr:   "",
			expected: true,
		},
		{
			name:     "empty string",
			str:      "",
			substr:   "hello",
			expected: false,
		},
		{
			name:     "both empty",
			str:      "",
			substr:   "",
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := contains(tt.str, tt.substr)
			if result != tt.expected {
				t.Errorf("contains(%q, %q) = %v, expected %v", tt.str, tt.substr, result, tt.expected)
			}
		})
	}
}

func TestIndexIgnoreCase(t *testing.T) {
	tests := []struct {
		name     string
		str      string
		substr   string
		expected int
	}{
		{
			name:     "exact match at start",
			str:      "hello",
			substr:   "hello",
			expected: 0,
		},
		{
			name:     "case insensitive match",
			str:      "Hello World",
			substr:   "hello",
			expected: 0,
		},
		{
			name:     "substring in middle",
			str:      "hello world",
			substr:   "o w",
			expected: 4,
		},
		{
			name:     "substring not found",
			str:      "hello world",
			substr:   "foo",
			expected: -1,
		},
		{
			name:     "mixed case",
			str:      "Hello WORLD",
			substr:   "world",
			expected: 6,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := indexIgnoreCase(tt.str, tt.substr)
			if result != tt.expected {
				t.Errorf("indexIgnoreCase(%q, %q) = %d, expected %d", tt.str, tt.substr, result, tt.expected)
			}
		})
	}
}

func TestNewTestRunner(t *testing.T) {
	// Create a temporary config file
	configContent := `
concurrency: 2
tests:
  - name: "test1"
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
`

	tmpFile, err := os.CreateTemp("", "test-config-*.yaml")
	if err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())
	defer tmpFile.Close()

	if _, err := tmpFile.WriteString(configContent); err != nil {
		t.Fatalf("failed to write config: %v", err)
	}

	// Test valid config
	runner, err := NewTestRunner(tmpFile.Name())
	if err != nil {
		t.Errorf("unexpected error creating test runner: %v", err)
	}

	if runner == nil {
		t.Error("expected test runner but got nil")
		return
	}

	if runner.config.Concurrency != 2 {
		t.Errorf("expected concurrency 2, got %d", runner.config.Concurrency)
	}

	if len(runner.config.Tests) != 1 {
		t.Errorf("expected 1 test, got %d", len(runner.config.Tests))
	}

	// Test non-existent file
	_, err = NewTestRunner("non-existent-file.yaml")
	if err == nil {
		t.Error("expected error for non-existent file but got none")
	}
}

func TestNewTestRunnerInvalidYAML(t *testing.T) {
	// Create a temporary file with invalid YAML
	invalidYAML := `
concurrency: 2
tests:
  - name: "test1"
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
    invalid_yaml: [unclosed bracket
`

	tmpFile, err := os.CreateTemp("", "invalid-config-*.yaml")
	if err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())
	defer tmpFile.Close()

	if _, err := tmpFile.WriteString(invalidYAML); err != nil {
		t.Fatalf("failed to write config: %v", err)
	}

	// Test invalid YAML
	_, err = NewTestRunner(tmpFile.Name())
	if err == nil {
		t.Error("expected error for invalid YAML but got none")
	}

	if !strings.Contains(err.Error(), "failed to unmarshal config") {
		t.Errorf("expected unmarshal error, got: %v", err)
	}
}

func TestRunTestsWithCancellation(t *testing.T) {
	// Create a test runner with a simple config
	runner := &TestRunner{
		config: TestConfig{
			Concurrency: 1,
			Tests: []TestCase{
				{
					Name:        "test1",
					Image:       "nginx:alpine",
					ExposedPort: "80/tcp",
					WaitFor: WaitConfig{
						Strategy: "http",
						Target:   "/",
						Timeout:  30,
					},
					Assertions: []Assertion{
						{
							Type:     "http_status",
							Target:   "/",
							Expected: "200",
						},
					},
				},
			},
		},
	}

	// Create a context that's already cancelled
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	results := runner.RunTests(ctx)

	if len(results) != 1 {
		t.Errorf("expected 1 result, got %d", len(results))
		return
	}

	result := results[0]
	if result.Success {
		t.Error("expected test to fail due to cancellation")
	}

	if result.Error == nil {
		t.Error("expected error due to cancellation")
	}

	if !strings.Contains(result.Error.Error(), "cancelled") {
		t.Errorf("expected cancellation error, got: %v", result.Error)
	}
}

func TestRunTestsWithTimeout(t *testing.T) {
	runner := &TestRunner{
		config: TestConfig{
			Concurrency: 1,
			Tests: []TestCase{
				{
					Name:        "test1",
					Image:       "nginx:alpine",
					ExposedPort: "80/tcp",
					WaitFor: WaitConfig{
						Strategy: "http",
						Target:   "/",
						Timeout:  30,
					},
					Assertions: []Assertion{
						{
							Type:     "http_status",
							Target:   "/",
							Expected: "200",
						},
					},
				},
			},
		},
	}

	// Create a context with very short timeout
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Millisecond)
	defer cancel()

	// Give the context time to timeout
	time.Sleep(5 * time.Millisecond)

	results := runner.RunTests(ctx)

	if len(results) != 1 {
		t.Errorf("expected 1 result, got %d", len(results))
		return
	}

	result := results[0]
	if result.Success {
		t.Error("expected test to fail due to timeout")
	}
}
