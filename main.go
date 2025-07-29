package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/docker/go-connections/nat"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
	"gopkg.in/yaml.v3"
)

// TestConfig represents the overall configuration structure
type TestConfig struct {
	Tests       []TestCase `yaml:"tests"`
	Concurrency int        `yaml:"concurrency,omitempty"` // Maximum concurrent tests (default: 3)
}

// TestCase represents a single test case configuration
type TestCase struct {
	Name        string            `yaml:"name"`
	Image       string            `yaml:"image"`
	ExposedPort string            `yaml:"exposed_port"`
	WaitFor     WaitConfig        `yaml:"wait_for"`
	Environment map[string]string `yaml:"environment,omitempty"`
	Commands    []string          `yaml:"commands,omitempty"`
	Assertions  []Assertion       `yaml:"assertions"`
}

// WaitConfig defines what to wait for before running assertions
type WaitConfig struct {
	Strategy string `yaml:"strategy"` // "http", "port", "log", "exec", "file"
	Target   string `yaml:"target"`   // endpoint, port, log message, command, or file path
	Timeout  int    `yaml:"timeout"`  // timeout in seconds
}

// Assertion represents a test assertion
type Assertion struct {
	Type     string `yaml:"type"`     // "http_status", "http_body_contains", "port_open"
	Target   string `yaml:"target"`   // URL, port, etc.
	Expected string `yaml:"expected"` // expected value
}

// TestResult represents the result of a test case
type TestResult struct {
	Name     string
	Success  bool
	Error    error
	Duration time.Duration
}

// TestRunner handles the execution of test cases
type TestRunner struct {
	config TestConfig
}

// NewTestRunner creates a new test runner with the given config
func NewTestRunner(configPath string) (*TestRunner, error) {
	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	var config TestConfig
	if err := yaml.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("failed to unmarshal config: %w", err)
	}

	return &TestRunner{config: config}, nil
}

// validateTestCase validates a test case configuration
func (tr *TestRunner) validateTestCase(testCase TestCase) error {
	if testCase.Name == "" {
		return fmt.Errorf("test name cannot be empty")
	}
	if testCase.Image == "" {
		return fmt.Errorf("test image cannot be empty")
	}
	if testCase.ExposedPort == "" {
		return fmt.Errorf("exposed port cannot be empty")
	}
	if testCase.WaitFor.Strategy == "" {
		return fmt.Errorf("wait strategy cannot be empty")
	}
	if testCase.WaitFor.Target == "" {
		return fmt.Errorf("wait target cannot be empty")
	}
	if len(testCase.Assertions) == 0 {
		return fmt.Errorf("at least one assertion is required")
	}

	// Validate wait strategy
	validStrategies := []string{"http", "port", "log", "exec", "file"}
	validStrategy := false
	for _, strategy := range validStrategies {
		if testCase.WaitFor.Strategy == strategy {
			validStrategy = true
			break
		}
	}
	if !validStrategy {
		return fmt.Errorf("invalid wait strategy '%s', must be one of: %s", testCase.WaitFor.Strategy, strings.Join(validStrategies, ", "))
	}

	// Validate assertions
	for i, assertion := range testCase.Assertions {
		if assertion.Type == "" {
			return fmt.Errorf("assertion %d: type cannot be empty", i+1)
		}
		if assertion.Target == "" {
			return fmt.Errorf("assertion %d: target cannot be empty", i+1)
		}
		if assertion.Expected == "" {
			return fmt.Errorf("assertion %d: expected value cannot be empty", i+1)
		}

		validTypes := []string{"http_status", "http_body_contains", "port_open"}
		validType := false
		for _, t := range validTypes {
			if assertion.Type == t {
				validType = true
				break
			}
		}
		if !validType {
			return fmt.Errorf("assertion %d: invalid type '%s', must be one of: %s", i+1, assertion.Type, strings.Join(validTypes, ", "))
		}
	}

	return nil
}

// RunTests executes all test cases in parallel with concurrency limit
func (tr *TestRunner) RunTests(ctx context.Context) []TestResult {
	// Set default concurrency if not specified
	concurrency := tr.config.Concurrency
	if concurrency <= 0 {
		concurrency = 3 // default to 3 concurrent tests
	}

	// Create semaphore channel to limit concurrency
	semaphore := make(chan struct{}, concurrency)

	var wg sync.WaitGroup
	results := make([]TestResult, len(tr.config.Tests))

	log.Printf("Running tests with concurrency limit: %d", concurrency)

	for i, testCase := range tr.config.Tests {
		wg.Add(1)
		go func(index int, tc TestCase) {
			defer wg.Done()

			// Check if context is cancelled before starting
			select {
			case <-ctx.Done():
				results[index] = TestResult{
					Name:     tc.Name,
					Success:  false,
					Error:    fmt.Errorf("test cancelled: %w", ctx.Err()),
					Duration: 0,
				}
				return
			default:
			}

			// Acquire semaphore (block if at concurrency limit)
			select {
			case semaphore <- struct{}{}:
				defer func() { <-semaphore }() // Release semaphore when done
			case <-ctx.Done():
				results[index] = TestResult{
					Name:     tc.Name,
					Success:  false,
					Error:    fmt.Errorf("test cancelled while waiting for semaphore: %w", ctx.Err()),
					Duration: 0,
				}
				return
			}

			results[index] = tr.runSingleTest(ctx, tc)
		}(i, testCase)
	}

	wg.Wait()
	return results
}

// runSingleTest executes a single test case
func (tr *TestRunner) runSingleTest(ctx context.Context, testCase TestCase) TestResult {
	start := time.Now()

	log.Printf("[%s] Starting test with image: %s", testCase.Name, testCase.Image)

	// Validate test case configuration
	if err := tr.validateTestCase(testCase); err != nil {
		return TestResult{
			Name:     testCase.Name,
			Success:  false,
			Error:    fmt.Errorf("test configuration invalid: %w", err),
			Duration: time.Since(start),
		}
	}

	// Create container request
	req := testcontainers.ContainerRequest{
		Image:        testCase.Image,
		ExposedPorts: []string{testCase.ExposedPort},
		Env:          testCase.Environment,
		Cmd:          testCase.Commands,
		WaitingFor:   tr.createWaitStrategy(testCase.WaitFor),
	}

	// Start container
	log.Printf("[%s] Creating container with image: %s", testCase.Name, testCase.Image)
	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
	if err != nil {
		log.Printf("[%s] Failed to start container: %v", testCase.Name, err)
		return TestResult{
			Name:     testCase.Name,
			Success:  false,
			Error:    fmt.Errorf("failed to start container '%s' with image '%s': %w", testCase.Name, testCase.Image, err),
			Duration: time.Since(start),
		}
	}

	log.Printf("[%s] Container started, running assertions", testCase.Name)

	// Ensure container cleanup with timeout
	defer func() {
		log.Printf("[%s] Cleaning up container", testCase.Name)

		// Create a separate context for cleanup with timeout
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cleanupCancel()

		// Try graceful termination first
		if err := container.Terminate(cleanupCtx); err != nil {
			log.Printf("[%s] Failed to terminate container gracefully: %v", testCase.Name, err)
		} else {
			log.Printf("[%s] Container terminated successfully", testCase.Name)
		}
	}()

	// Run assertions
	if err := tr.runAssertions(ctx, container, testCase.Assertions); err != nil {
		log.Printf("[%s] Test failed: %v", testCase.Name, err)
		return TestResult{
			Name:     testCase.Name,
			Success:  false,
			Error:    err,
			Duration: time.Since(start),
		}
	}

	log.Printf("[%s] Test completed successfully in %v", testCase.Name, time.Since(start).Truncate(time.Millisecond))
	return TestResult{
		Name:     testCase.Name,
		Success:  true,
		Duration: time.Since(start),
	}
}

// createWaitStrategy creates a wait strategy based on the configuration
func (tr *TestRunner) createWaitStrategy(waitConfig WaitConfig) wait.Strategy {
	timeout := time.Duration(waitConfig.Timeout) * time.Second
	if timeout == 0 {
		timeout = 30 * time.Second // default timeout
	}

	switch waitConfig.Strategy {
	case "http":
		return wait.ForHTTP(waitConfig.Target).WithStartupTimeout(timeout)
	case "port":
		return wait.ForListeningPort(nat.Port(waitConfig.Target)).WithStartupTimeout(timeout)
	case "log":
		return wait.ForLog(waitConfig.Target).WithStartupTimeout(timeout)
	case "exec":
		return wait.ForExec([]string{"sh", "-c", waitConfig.Target}).WithStartupTimeout(timeout)
	case "file":
		return wait.ForExec([]string{"test", "-f", waitConfig.Target}).WithStartupTimeout(timeout)
	default:
		return wait.ForListeningPort("80/tcp").WithStartupTimeout(timeout)
	}
}

// runAssertions executes all assertions for a test case
func (tr *TestRunner) runAssertions(ctx context.Context, container testcontainers.Container, assertions []Assertion) error {
	for _, assertion := range assertions {
		if err := tr.runAssertion(ctx, container, assertion); err != nil {
			return fmt.Errorf("assertion failed: %w", err)
		}
	}
	return nil
}

// runAssertion executes a single assertion
func (tr *TestRunner) runAssertion(ctx context.Context, container testcontainers.Container, assertion Assertion) error {
	switch assertion.Type {
	case "http_status":
		return tr.assertHTTPStatus(ctx, container, assertion)
	case "http_body_contains":
		return tr.assertHTTPBodyContains(ctx, container, assertion)
	case "port_open":
		return tr.assertPortOpen(ctx, container, assertion)
	default:
		return fmt.Errorf("unknown assertion type: %s", assertion.Type)
	}
}

// assertHTTPStatus checks if HTTP endpoint returns expected status code
func (tr *TestRunner) assertHTTPStatus(ctx context.Context, container testcontainers.Container, assertion Assertion) error {
	host, err := container.Host(ctx)
	if err != nil {
		return fmt.Errorf("failed to get container host: %w", err)
	}

	port, err := container.MappedPort(ctx, "80/tcp")
	if err != nil {
		return fmt.Errorf("failed to get mapped port: %w", err)
	}

	url := fmt.Sprintf("http://%s:%s%s", host, port.Port(), assertion.Target)

	// Retry HTTP requests with exponential backoff
	var resp *http.Response
	var lastErr error
	maxRetries := 3

	for i := 0; i < maxRetries; i++ {
		// Check context cancellation
		select {
		case <-ctx.Done():
			return fmt.Errorf("HTTP request cancelled: %w", ctx.Err())
		default:
		}

		client := &http.Client{Timeout: 10 * time.Second}
		resp, err = client.Get(url)
		if err == nil {
			defer resp.Body.Close()
			break
		}

		lastErr = err
		if i < maxRetries-1 {
			backoff := time.Duration(i+1) * time.Second
			log.Printf("HTTP request failed (attempt %d/%d), retrying in %v: %v", i+1, maxRetries, backoff, err)
			time.Sleep(backoff)
		}
	}

	if err != nil {
		return fmt.Errorf("failed to make HTTP request after %d attempts: %w", maxRetries, lastErr)
	}

	expectedStatus := assertion.Expected
	actualStatus := fmt.Sprintf("%d", resp.StatusCode)

	if actualStatus != expectedStatus {
		return fmt.Errorf("HTTP status mismatch for %s: expected %s, got %s", url, expectedStatus, actualStatus)
	}

	return nil
}

// assertHTTPBodyContains checks if HTTP response body contains expected text
func (tr *TestRunner) assertHTTPBodyContains(ctx context.Context, container testcontainers.Container, assertion Assertion) error {
	host, err := container.Host(ctx)
	if err != nil {
		return fmt.Errorf("failed to get container host: %w", err)
	}

	port, err := container.MappedPort(ctx, "80/tcp")
	if err != nil {
		return fmt.Errorf("failed to get mapped port: %w", err)
	}

	url := fmt.Sprintf("http://%s:%s%s", host, port.Port(), assertion.Target)

	resp, err := http.Get(url)
	if err != nil {
		return fmt.Errorf("failed to make HTTP request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read response body: %w", err)
	}

	bodyStr := string(body)
	if !contains(bodyStr, assertion.Expected) {
		return fmt.Errorf("response body does not contain expected text: %s", assertion.Expected)
	}

	return nil
}

// assertPortOpen checks if a port is open on the container
func (tr *TestRunner) assertPortOpen(ctx context.Context, container testcontainers.Container, assertion Assertion) error {
	host, err := container.Host(ctx)
	if err != nil {
		return fmt.Errorf("failed to get container host: %w", err)
	}

	port, err := container.MappedPort(ctx, nat.Port(assertion.Target))
	if err != nil {
		return fmt.Errorf("failed to get mapped port: %w", err)
	}

	var address string
	if strings.Contains(host, ":") {
		address = fmt.Sprintf("[%s]:%s", host, port.Port())
	} else {
		address = fmt.Sprintf("%s:%s", host, port.Port())
	}

	// Simple TCP connection test
	conn, err := net.DialTimeout("tcp", address, 5*time.Second)
	if err != nil {
		return fmt.Errorf("port %s is not open: %w", assertion.Target, err)
	}
	defer conn.Close()

	return nil
}

// contains checks if a string contains a substring (case-insensitive)
func contains(str, substr string) bool {
	return len(str) >= len(substr) &&
		(str == substr ||
			len(substr) == 0 ||
			indexIgnoreCase(str, substr) >= 0)
}

// indexIgnoreCase finds the index of substr in str (case-insensitive)
func indexIgnoreCase(str, substr string) int {
	str = strings.ToLower(str)
	substr = strings.ToLower(substr)
	return strings.Index(str, substr)
}

// printResults prints the test results
func printResults(results []TestResult) {
	fmt.Println("\n" + strings.Repeat("=", 50))
	fmt.Println("TEST RESULTS")
	fmt.Println(strings.Repeat("=", 50))

	passed := 0
	total := len(results)

	for _, result := range results {
		status := "PASS"
		if !result.Success {
			status = "FAIL"
		} else {
			passed++
		}

		fmt.Printf("%-30s | %-4s | %v\n", result.Name, status, result.Duration.Truncate(time.Millisecond))

		if result.Error != nil {
			fmt.Printf("   Error: %v\n", result.Error)
		}
	}

	fmt.Println(strings.Repeat("-", 50))
	fmt.Printf("Total: %d, Passed: %d, Failed: %d\n", total, passed, total-passed)

	if passed == total {
		fmt.Println("🎉 All tests passed!")
	} else {
		fmt.Printf("❌ %d test(s) failed\n", total-passed)
	}
}

func main() {
	if len(os.Args) < 2 {
		log.Fatal("Usage: go run main.go <config-file>")
	}

	configPath := os.Args[1]

	// Create test runner
	runner, err := NewTestRunner(configPath)
	if err != nil {
		log.Fatalf("Failed to create test runner: %v", err)
	}

	// Create context with timeout and signal handling
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	// Set up signal handling for graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// Create a context that gets cancelled on signal
	signalCtx, signalCancel := context.WithCancel(ctx)
	defer signalCancel()

	// Handle signals in a separate goroutine
	go func() {
		sig := <-sigChan
		log.Printf("\nReceived signal %v, initiating graceful shutdown...", sig)
		fmt.Printf("\n🛑 Shutting down gracefully... (press Ctrl+C again to force quit)\n")
		signalCancel()

		// Set up a second signal handler for force quit
		go func() {
			sig := <-sigChan
			log.Printf("Received second signal %v, forcing immediate exit", sig)
			fmt.Printf("💥 Force quitting...\n")
			os.Exit(1)
		}()
	}()

	// Show configuration info
	concurrency := runner.config.Concurrency
	if concurrency <= 0 {
		concurrency = 3
	}

	fmt.Printf("Starting test execution with %d test(s), concurrency limit: %d\n",
		len(runner.config.Tests), concurrency)
	fmt.Printf("Press Ctrl+C to gracefully shutdown\n\n")
	start := time.Now()

	results := runner.RunTests(signalCtx)

	totalDuration := time.Since(start)

	// Check if we were interrupted
	if signalCtx.Err() != nil {
		fmt.Printf("\n⚠️  Test execution was interrupted\n")
	}

	// Print results
	printResults(results)
	fmt.Printf("\nTotal execution time: %v\n", totalDuration.Truncate(time.Millisecond))
}
