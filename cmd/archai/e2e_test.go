package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestE2E_CLI_Generate tests the CLI end-to-end with real package generation.
func TestE2E_CLI_Generate(t *testing.T) {
	// Build the CLI binary first
	tmpBin := filepath.Join(t.TempDir(), "archai")
	cmd := exec.Command("go", "build", "-o", tmpBin, ".")
	if err := cmd.Run(); err != nil {
		t.Fatalf("Failed to build archai binary: %v", err)
	}

	// Create a test package
	testDir := t.TempDir()

	goMod := `module test.example/e2etest

go 1.21
`
	err := os.WriteFile(filepath.Join(testDir, "go.mod"), []byte(goMod), 0644)
	if err != nil {
		t.Fatalf("Failed to create go.mod: %v", err)
	}

	testCode := `package e2etest

import "context"

// Service is a test service.
// archspec:service
type Service interface {
	Process(ctx context.Context, data string) error
}

// Config holds configuration.
// archspec:value
type Config struct {
	Name string
}

// NewService creates a service.
// archspec:factory
func NewService(cfg Config) Service {
	return nil
}
`
	err = os.WriteFile(filepath.Join(testDir, "service.go"), []byte(testCode), 0644)
	if err != nil {
		t.Fatalf("Failed to create service.go: %v", err)
	}

	// Run: archai diagram generate .
	cmd = exec.Command(tmpBin, "diagram", "generate", ".")
	cmd.Dir = testDir
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("CLI command failed: %v\nOutput: %s", err, output)
	}

	// Verify output messages
	outputStr := string(output)
	if !strings.Contains(outputStr, "Created") || !strings.Contains(outputStr, ".arch/pub.d2") {
		t.Errorf("Expected success message with pub.d2 path\nGot: %s", outputStr)
	}
	if !strings.Contains(outputStr, ".arch/internal.d2") {
		t.Errorf("Expected success message with internal.d2 path\nGot: %s", outputStr)
	}

	// Verify .arch directory was created
	archDir := filepath.Join(testDir, ".arch")
	if _, err := os.Stat(archDir); os.IsNotExist(err) {
		t.Fatalf(".arch directory was not created")
	}

	// Verify pub.d2 exists and has correct content
	pubFile := filepath.Join(archDir, "pub.d2")
	pubContent, err := os.ReadFile(pubFile)
	if err != nil {
		t.Fatalf("Failed to read pub.d2: %v", err)
	}

	pubStr := string(pubContent)
	expectedInPub := []string{
		"# e2etest package",
		"# Legend",
		"Service: {",
		"Config: {",
		`stereotype: "<<interface>>"`,
		`stereotype: "<<struct>>"`,
		"+Process",
		"+Name string",
		"NewService: {",
		`stereotype: "<<factory>>"`,
	}

	for _, expected := range expectedInPub {
		if !strings.Contains(pubStr, expected) {
			t.Errorf("pub.d2 missing expected content: %q\n\nGot:\n%s", expected, pubStr)
		}
	}

	// Verify internal.d2 exists
	internalFile := filepath.Join(archDir, "internal.d2")
	if _, err := os.Stat(internalFile); os.IsNotExist(err) {
		t.Fatalf("internal.d2 was not created")
	}
}

// TestE2E_CLI_Generate_PublicOnly tests --pub flag.
func TestE2E_CLI_Generate_PublicOnly(t *testing.T) {
	tmpBin := filepath.Join(t.TempDir(), "archai")
	cmd := exec.Command("go", "build", "-o", tmpBin, ".")
	if err := cmd.Run(); err != nil {
		t.Fatalf("Failed to build archai binary: %v", err)
	}

	testDir := t.TempDir()

	goMod := `module test.example/pubtest

go 1.21
`
	err := os.WriteFile(filepath.Join(testDir, "go.mod"), []byte(goMod), 0644)
	if err != nil {
		t.Fatalf("Failed to create go.mod: %v", err)
	}

	testCode := `package pubtest

// Service is a test service.
type Service interface {
	Do() error
}
`
	err = os.WriteFile(filepath.Join(testDir, "service.go"), []byte(testCode), 0644)
	if err != nil {
		t.Fatalf("Failed to create service.go: %v", err)
	}

	// Run with --pub flag
	cmd = exec.Command(tmpBin, "diagram", "generate", ".", "--pub")
	cmd.Dir = testDir
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("CLI command failed: %v\nOutput: %s", err, output)
	}

	// Verify only pub.d2 was created
	archDir := filepath.Join(testDir, ".arch")
	pubFile := filepath.Join(archDir, "pub.d2")
	internalFile := filepath.Join(archDir, "internal.d2")

	if _, err := os.Stat(pubFile); os.IsNotExist(err) {
		t.Fatalf("pub.d2 was not created")
	}

	if _, err := os.Stat(internalFile); err == nil {
		t.Fatalf("internal.d2 should not have been created with --pub flag")
	}
}

// TestE2E_CLI_Generate_InternalOnly tests --internal flag.
func TestE2E_CLI_Generate_InternalOnly(t *testing.T) {
	tmpBin := filepath.Join(t.TempDir(), "archai")
	cmd := exec.Command("go", "build", "-o", tmpBin, ".")
	if err := cmd.Run(); err != nil {
		t.Fatalf("Failed to build archai binary: %v", err)
	}

	testDir := t.TempDir()

	goMod := `module test.example/internaltest

go 1.21
`
	err := os.WriteFile(filepath.Join(testDir, "go.mod"), []byte(goMod), 0644)
	if err != nil {
		t.Fatalf("Failed to create go.mod: %v", err)
	}

	testCode := `package internaltest

// Service is a test service.
type Service interface {
	Do() error
}
`
	err = os.WriteFile(filepath.Join(testDir, "service.go"), []byte(testCode), 0644)
	if err != nil {
		t.Fatalf("Failed to create service.go: %v", err)
	}

	// Run with --internal flag
	cmd = exec.Command(tmpBin, "diagram", "generate", ".", "--internal")
	cmd.Dir = testDir
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("CLI command failed: %v\nOutput: %s", err, output)
	}

	// Verify only internal.d2 was created
	archDir := filepath.Join(testDir, ".arch")
	pubFile := filepath.Join(archDir, "pub.d2")
	internalFile := filepath.Join(archDir, "internal.d2")

	if _, err := os.Stat(internalFile); os.IsNotExist(err) {
		t.Fatalf("internal.d2 was not created")
	}

	if _, err := os.Stat(pubFile); err == nil {
		t.Fatalf("pub.d2 should not have been created with --internal flag")
	}
}

// TestE2E_CLI_Generate_MultiplePackages tests generating diagrams for multiple packages.
func TestE2E_CLI_Generate_MultiplePackages(t *testing.T) {
	tmpBin := filepath.Join(t.TempDir(), "archai")
	cmd := exec.Command("go", "build", "-o", tmpBin, ".")
	if err := cmd.Run(); err != nil {
		t.Fatalf("Failed to build archai binary: %v", err)
	}

	testDir := t.TempDir()

	goMod := `module test.example/multitest

go 1.21
`
	err := os.WriteFile(filepath.Join(testDir, "go.mod"), []byte(goMod), 0644)
	if err != nil {
		t.Fatalf("Failed to create go.mod: %v", err)
	}

	// Create pkg1
	pkg1Dir := filepath.Join(testDir, "pkg1")
	err = os.Mkdir(pkg1Dir, 0755)
	if err != nil {
		t.Fatalf("Failed to create pkg1: %v", err)
	}

	pkg1Code := `package pkg1

// Service1 is a service.
type Service1 interface {
	Do() error
}
`
	err = os.WriteFile(filepath.Join(pkg1Dir, "service.go"), []byte(pkg1Code), 0644)
	if err != nil {
		t.Fatalf("Failed to create pkg1/service.go: %v", err)
	}

	// Create pkg2
	pkg2Dir := filepath.Join(testDir, "pkg2")
	err = os.Mkdir(pkg2Dir, 0755)
	if err != nil {
		t.Fatalf("Failed to create pkg2: %v", err)
	}

	pkg2Code := `package pkg2

// Service2 is a service.
type Service2 interface {
	Do() error
}
`
	err = os.WriteFile(filepath.Join(pkg2Dir, "service.go"), []byte(pkg2Code), 0644)
	if err != nil {
		t.Fatalf("Failed to create pkg2/service.go: %v", err)
	}

	// Run: archai diagram generate ./pkg1 ./pkg2
	cmd = exec.Command(tmpBin, "diagram", "generate", "./pkg1", "./pkg2")
	cmd.Dir = testDir
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("CLI command failed: %v\nOutput: %s", err, output)
	}

	outputStr := string(output)

	// Verify both packages are mentioned in output
	if !strings.Contains(outputStr, "pkg1/.arch/pub.d2") {
		t.Errorf("Expected pkg1 pub.d2 in output\nGot: %s", outputStr)
	}
	if !strings.Contains(outputStr, "pkg2/.arch/pub.d2") {
		t.Errorf("Expected pkg2 pub.d2 in output\nGot: %s", outputStr)
	}

	// Verify both .arch directories were created
	pkg1ArchDir := filepath.Join(pkg1Dir, ".arch")
	pkg2ArchDir := filepath.Join(pkg2Dir, ".arch")

	if _, err := os.Stat(pkg1ArchDir); os.IsNotExist(err) {
		t.Fatalf("pkg1/.arch directory was not created")
	}

	if _, err := os.Stat(pkg2ArchDir); os.IsNotExist(err) {
		t.Fatalf("pkg2/.arch directory was not created")
	}

	// Verify diagrams exist in both packages
	if _, err := os.Stat(filepath.Join(pkg1ArchDir, "pub.d2")); os.IsNotExist(err) {
		t.Fatalf("pkg1/.arch/pub.d2 was not created")
	}

	if _, err := os.Stat(filepath.Join(pkg2ArchDir, "pub.d2")); os.IsNotExist(err) {
		t.Fatalf("pkg2/.arch/pub.d2 was not created")
	}
}

// TestE2E_CLI_Generate_InvalidPackage tests error handling for invalid packages.
func TestE2E_CLI_Generate_InvalidPackage(t *testing.T) {
	tmpBin := filepath.Join(t.TempDir(), "archai")
	cmd := exec.Command("go", "build", "-o", tmpBin, ".")
	if err := cmd.Run(); err != nil {
		t.Fatalf("Failed to build archai binary: %v", err)
	}

	// Run with nonexistent package
	cmd = exec.Command(tmpBin, "diagram", "generate", "/nonexistent/path")
	output, err := cmd.CombinedOutput()

	// Should fail with non-zero exit code
	if err == nil {
		t.Fatalf("Expected command to fail for invalid package")
	}

	outputStr := string(output)
	if !strings.Contains(outputStr, "Error") && !strings.Contains(outputStr, "error") {
		t.Errorf("Expected error message in output\nGot: %s", outputStr)
	}
}

// TestE2E_CLI_DiagramContentAccuracy tests that generated diagrams contain expected symbols.
func TestE2E_CLI_DiagramContentAccuracy(t *testing.T) {
	tmpBin := filepath.Join(t.TempDir(), "archai")
	cmd := exec.Command("go", "build", "-o", tmpBin, ".")
	if err := cmd.Run(); err != nil {
		t.Fatalf("Failed to build archai binary: %v", err)
	}

	testDir := t.TempDir()

	goMod := `module test.example/accuracy

go 1.21
`
	err := os.WriteFile(filepath.Join(testDir, "go.mod"), []byte(goMod), 0644)
	if err != nil {
		t.Fatalf("Failed to create go.mod: %v", err)
	}

	// Create a comprehensive test file
	testCode := `package accuracy

import "context"

// UserRepository provides data access.
// archspec:repository
type UserRepository interface {
	FindByID(ctx context.Context, id string) (*User, error)
	Save(ctx context.Context, user User) error
}

// User represents a user.
// archspec:aggregate
type User struct {
	ID       string
	Name     string
	Email    string
	password string  // unexported
}

// Role is an enum.
// archspec:enum
type Role string

const (
	RoleAdmin Role = "admin"
	RoleUser  Role = "user"
)

// NewUser creates a user.
// archspec:factory
func NewUser(id, name, email string) *User {
	return &User{ID: id, Name: name, Email: email}
}

// unexported helper
func validateEmail(email string) bool {
	return true
}
`
	err = os.WriteFile(filepath.Join(testDir, "user.go"), []byte(testCode), 0644)
	if err != nil {
		t.Fatalf("Failed to create user.go: %v", err)
	}

	// Generate diagrams
	cmd = exec.Command(tmpBin, "diagram", "generate", ".")
	cmd.Dir = testDir
	if _, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("CLI command failed: %v", err)
	}

	// Read pub.d2
	pubContent, err := os.ReadFile(filepath.Join(testDir, ".arch", "pub.d2"))
	if err != nil {
		t.Fatalf("Failed to read pub.d2: %v", err)
	}

	pubStr := string(pubContent)

	// Verify interface with methods
	if !strings.Contains(pubStr, "UserRepository: {") {
		t.Error("pub.d2 should contain UserRepository interface")
	}
	if !strings.Contains(pubStr, "+FindByID") {
		t.Error("pub.d2 should contain FindByID method")
	}
	if !strings.Contains(pubStr, "+Save") {
		t.Error("pub.d2 should contain Save method")
	}

	// Verify struct with fields
	if !strings.Contains(pubStr, "User: {") {
		t.Error("pub.d2 should contain User struct")
	}
	if !strings.Contains(pubStr, "+ID string") {
		t.Error("pub.d2 should contain ID field")
	}
	if !strings.Contains(pubStr, "+Name string") {
		t.Error("pub.d2 should contain Name field")
	}

	// Verify unexported field is NOT in pub.d2
	if strings.Contains(pubStr, "password") {
		t.Error("pub.d2 should NOT contain unexported password field")
	}

	// Verify enum
	if !strings.Contains(pubStr, "Role: {") {
		t.Error("pub.d2 should contain Role enum")
	}
	if !strings.Contains(pubStr, `stereotype: "<<enum>>"`) {
		t.Error("pub.d2 should mark Role as enum stereotype")
	}

	// Verify factory
	if !strings.Contains(pubStr, "NewUser: {") {
		t.Error("pub.d2 should contain NewUser factory")
	}
	if !strings.Contains(pubStr, `stereotype: "<<factory>>"`) {
		t.Error("pub.d2 should mark factory with factory stereotype")
	}

	// Verify unexported function is NOT in pub.d2
	if strings.Contains(pubStr, "validateEmail") {
		t.Error("pub.d2 should NOT contain unexported validateEmail function")
	}

	// Verify stereotypes are applied (interfaces get <<interface>>, structs get <<struct>>)
	if !strings.Contains(pubStr, `stereotype: "<<interface>>"`) {
		t.Error("pub.d2 should mark interfaces with interface stereotype")
	}
	if !strings.Contains(pubStr, `stereotype: "<<struct>>"`) {
		t.Error("pub.d2 should mark structs with struct stereotype")
	}

	// Read internal.d2
	internalContent, err := os.ReadFile(filepath.Join(testDir, ".arch", "internal.d2"))
	if err != nil {
		t.Fatalf("Failed to read internal.d2: %v", err)
	}

	internalStr := string(internalContent)

	// Verify internal.d2 contains unexported symbols
	if !strings.Contains(internalStr, "-password string") {
		t.Error("internal.d2 should contain unexported password field")
	}
	if !strings.Contains(internalStr, "validateEmail: {") {
		t.Error("internal.d2 should contain unexported validateEmail function")
	}
}

// TestE2E_CLI_GenerateCombined tests the combined mode with --output flag.
func TestE2E_CLI_GenerateCombined(t *testing.T) {
	tmpBin := filepath.Join(t.TempDir(), "archai")
	cmd := exec.Command("go", "build", "-o", tmpBin, ".")
	if err := cmd.Run(); err != nil {
		t.Fatalf("Failed to build archai binary: %v", err)
	}

	testDir := t.TempDir()

	goMod := `module test.example/combined

go 1.21
`
	err := os.WriteFile(filepath.Join(testDir, "go.mod"), []byte(goMod), 0644)
	if err != nil {
		t.Fatalf("Failed to create go.mod: %v", err)
	}

	testCode := `package combined

// Service is a test service.
type Service interface {
	Process() error
}

// Config holds configuration.
type Config struct {
	Name string
}

// NewService creates a service.
func NewService(cfg Config) Service {
	return nil
}
`
	err = os.WriteFile(filepath.Join(testDir, "service.go"), []byte(testCode), 0644)
	if err != nil {
		t.Fatalf("Failed to create service.go: %v", err)
	}

	// Run: archai diagram generate . -o combined.d2
	outputFile := filepath.Join(testDir, "combined.d2")
	cmd = exec.Command(tmpBin, "diagram", "generate", ".", "-o", outputFile)
	cmd.Dir = testDir
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("CLI command failed: %v\nOutput: %s", err, output)
	}

	// Verify output messages
	outputStr := string(output)
	if !strings.Contains(outputStr, "Combined diagram generated") {
		t.Errorf("Expected 'Combined diagram generated' message\nGot: %s", outputStr)
	}
	if !strings.Contains(outputStr, "Packages included: 1") {
		t.Errorf("Expected 'Packages included: 1' message\nGot: %s", outputStr)
	}

	// Verify combined.d2 was created
	if _, err := os.Stat(outputFile); os.IsNotExist(err) {
		t.Fatalf("combined.d2 was not created")
	}

	// Read and verify content
	content, err := os.ReadFile(outputFile)
	if err != nil {
		t.Fatalf("Failed to read combined.d2: %v", err)
	}

	contentStr := string(content)
	expectedParts := []string{
		"# Combined Architecture Diagram",
		"# Legend",
		"legend: {",
		"# Packages",
		"Service: {",
		"Config: {",
		"NewService: {",
		`stereotype: "<<interface>>"`,
		`stereotype: "<<struct>>"`,
		`stereotype: "<<factory>>"`,
	}

	for _, part := range expectedParts {
		if !strings.Contains(contentStr, part) {
			t.Errorf("combined.d2 missing expected content: %q\n\nGot:\n%s", part, contentStr)
		}
	}
}

// TestE2E_CLI_GenerateCombined_MultiplePackages tests combined mode with multiple packages.
func TestE2E_CLI_GenerateCombined_MultiplePackages(t *testing.T) {
	tmpBin := filepath.Join(t.TempDir(), "archai")
	cmd := exec.Command("go", "build", "-o", tmpBin, ".")
	if err := cmd.Run(); err != nil {
		t.Fatalf("Failed to build archai binary: %v", err)
	}

	testDir := t.TempDir()

	goMod := `module test.example/multipkg

go 1.21
`
	err := os.WriteFile(filepath.Join(testDir, "go.mod"), []byte(goMod), 0644)
	if err != nil {
		t.Fatalf("Failed to create go.mod: %v", err)
	}

	// Create pkg1
	pkg1Dir := filepath.Join(testDir, "pkg1")
	err = os.Mkdir(pkg1Dir, 0755)
	if err != nil {
		t.Fatalf("Failed to create pkg1: %v", err)
	}

	pkg1Code := `package pkg1

// Service1 is a service.
type Service1 interface {
	DoA() error
}
`
	err = os.WriteFile(filepath.Join(pkg1Dir, "service.go"), []byte(pkg1Code), 0644)
	if err != nil {
		t.Fatalf("Failed to create pkg1/service.go: %v", err)
	}

	// Create pkg2
	pkg2Dir := filepath.Join(testDir, "pkg2")
	err = os.Mkdir(pkg2Dir, 0755)
	if err != nil {
		t.Fatalf("Failed to create pkg2: %v", err)
	}

	pkg2Code := `package pkg2

// Service2 is a service.
type Service2 interface {
	DoB() error
}
`
	err = os.WriteFile(filepath.Join(pkg2Dir, "service.go"), []byte(pkg2Code), 0644)
	if err != nil {
		t.Fatalf("Failed to create pkg2/service.go: %v", err)
	}

	// Run: archai diagram generate ./pkg1 ./pkg2 -o all.d2
	outputFile := filepath.Join(testDir, "all.d2")
	cmd = exec.Command(tmpBin, "diagram", "generate", "./pkg1", "./pkg2", "-o", outputFile)
	cmd.Dir = testDir
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("CLI command failed: %v\nOutput: %s", err, output)
	}

	// Verify output
	outputStr := string(output)
	if !strings.Contains(outputStr, "Packages included: 2") {
		t.Errorf("Expected 'Packages included: 2' message\nGot: %s", outputStr)
	}

	// Verify both packages are in the diagram
	content, err := os.ReadFile(outputFile)
	if err != nil {
		t.Fatalf("Failed to read all.d2: %v", err)
	}

	contentStr := string(content)
	expectedParts := []string{
		"pkg1: {",
		`label: "pkg1"`,
		"Service1: {",
		"pkg2: {",
		`label: "pkg2"`,
		"Service2: {",
	}

	for _, part := range expectedParts {
		if !strings.Contains(contentStr, part) {
			t.Errorf("all.d2 missing expected content: %q\n\nGot:\n%s", part, contentStr)
		}
	}
}

// TestE2E_CLI_GenerateCombined_PublicOnly tests that combined mode is always public-only.
func TestE2E_CLI_GenerateCombined_PublicOnly(t *testing.T) {
	tmpBin := filepath.Join(t.TempDir(), "archai")
	cmd := exec.Command("go", "build", "-o", tmpBin, ".")
	if err := cmd.Run(); err != nil {
		t.Fatalf("Failed to build archai binary: %v", err)
	}

	testDir := t.TempDir()

	goMod := `module test.example/pubonly

go 1.21
`
	err := os.WriteFile(filepath.Join(testDir, "go.mod"), []byte(goMod), 0644)
	if err != nil {
		t.Fatalf("Failed to create go.mod: %v", err)
	}

	testCode := `package pubonly

// Public is exported.
type Public struct {
	ExportedField string
	unexported    string
}

// private is unexported.
type private struct {
	field string
}
`
	err = os.WriteFile(filepath.Join(testDir, "types.go"), []byte(testCode), 0644)
	if err != nil {
		t.Fatalf("Failed to create types.go: %v", err)
	}

	// Run combined mode
	outputFile := filepath.Join(testDir, "combined.d2")
	cmd = exec.Command(tmpBin, "diagram", "generate", ".", "-o", outputFile)
	cmd.Dir = testDir
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("CLI command failed: %v\nOutput: %s", err, output)
	}

	// Read content
	content, err := os.ReadFile(outputFile)
	if err != nil {
		t.Fatalf("Failed to read combined.d2: %v", err)
	}

	contentStr := string(content)

	// Verify exported symbols are present
	if !strings.Contains(contentStr, "Public: {") {
		t.Error("combined.d2 should contain Public struct")
	}
	if !strings.Contains(contentStr, "+ExportedField") {
		t.Error("combined.d2 should contain ExportedField")
	}

	// Verify unexported symbols are absent
	if strings.Contains(contentStr, "private: {") {
		t.Error("combined.d2 should NOT contain unexported private struct")
	}
	if strings.Contains(contentStr, "unexported") {
		t.Error("combined.d2 should NOT contain unexported field")
	}
}

// TestE2E_CLI_GenerateCombined_IgnoresPubInternalFlags tests that --pub and --internal flags show warning.
func TestE2E_CLI_GenerateCombined_IgnoresPubInternalFlags(t *testing.T) {
	tmpBin := filepath.Join(t.TempDir(), "archai")
	cmd := exec.Command("go", "build", "-o", tmpBin, ".")
	if err := cmd.Run(); err != nil {
		t.Fatalf("Failed to build archai binary: %v", err)
	}

	testDir := t.TempDir()

	goMod := `module test.example/ignoreflag

go 1.21
`
	err := os.WriteFile(filepath.Join(testDir, "go.mod"), []byte(goMod), 0644)
	if err != nil {
		t.Fatalf("Failed to create go.mod: %v", err)
	}

	testCode := `package ignoreflag

type Service interface {
	Do() error
}
`
	err = os.WriteFile(filepath.Join(testDir, "service.go"), []byte(testCode), 0644)
	if err != nil {
		t.Fatalf("Failed to create service.go: %v", err)
	}

	// Run with --pub flag (should be ignored)
	outputFile := filepath.Join(testDir, "combined.d2")
	cmd = exec.Command(tmpBin, "diagram", "generate", ".", "-o", outputFile, "--pub")
	cmd.Dir = testDir
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("CLI command failed: %v\nOutput: %s", err, output)
	}

	outputStr := string(output)
	if !strings.Contains(outputStr, "Note: --pub and --internal flags are ignored") {
		t.Errorf("Expected warning about ignored flags\nGot: %s", outputStr)
	}

	// Verify diagram was still created
	if _, err := os.Stat(outputFile); os.IsNotExist(err) {
		t.Fatalf("combined.d2 was not created")
	}
}

// TestE2E_CLI_GenerateCombined_CreatesDirIfNeeded tests that parent directories are created.
func TestE2E_CLI_GenerateCombined_CreatesDirIfNeeded(t *testing.T) {
	tmpBin := filepath.Join(t.TempDir(), "archai")
	cmd := exec.Command("go", "build", "-o", tmpBin, ".")
	if err := cmd.Run(); err != nil {
		t.Fatalf("Failed to build archai binary: %v", err)
	}

	testDir := t.TempDir()

	goMod := `module test.example/createdir

go 1.21
`
	err := os.WriteFile(filepath.Join(testDir, "go.mod"), []byte(goMod), 0644)
	if err != nil {
		t.Fatalf("Failed to create go.mod: %v", err)
	}

	testCode := `package createdir

type Service interface {
	Do() error
}
`
	err = os.WriteFile(filepath.Join(testDir, "service.go"), []byte(testCode), 0644)
	if err != nil {
		t.Fatalf("Failed to create service.go: %v", err)
	}

	// Output to nested path that doesn't exist
	outputFile := filepath.Join(testDir, "docs", "diagrams", "combined.d2")
	cmd = exec.Command(tmpBin, "diagram", "generate", ".", "-o", outputFile)
	cmd.Dir = testDir
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("CLI command failed: %v\nOutput: %s", err, output)
	}

	// Verify file was created
	if _, err := os.Stat(outputFile); os.IsNotExist(err) {
		t.Fatalf("combined.d2 was not created at nested path")
	}
}

// TestE2E_CLI_GenerateCombined_NoExportedSymbols tests empty package handling.
func TestE2E_CLI_GenerateCombined_NoExportedSymbols(t *testing.T) {
	tmpBin := filepath.Join(t.TempDir(), "archai")
	cmd := exec.Command("go", "build", "-o", tmpBin, ".")
	if err := cmd.Run(); err != nil {
		t.Fatalf("Failed to build archai binary: %v", err)
	}

	testDir := t.TempDir()

	goMod := `module test.example/noexports

go 1.21
`
	err := os.WriteFile(filepath.Join(testDir, "go.mod"), []byte(goMod), 0644)
	if err != nil {
		t.Fatalf("Failed to create go.mod: %v", err)
	}

	// Only unexported symbols
	testCode := `package noexports

type private struct {
	field string
}
`
	err = os.WriteFile(filepath.Join(testDir, "private.go"), []byte(testCode), 0644)
	if err != nil {
		t.Fatalf("Failed to create private.go: %v", err)
	}

	// Run combined mode
	outputFile := filepath.Join(testDir, "combined.d2")
	cmd = exec.Command(tmpBin, "diagram", "generate", ".", "-o", outputFile)
	cmd.Dir = testDir
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("CLI command failed: %v\nOutput: %s", err, output)
	}

	outputStr := string(output)
	if !strings.Contains(outputStr, "No packages with exported symbols found") {
		t.Errorf("Expected message about no exported symbols\nGot: %s", outputStr)
	}
}
