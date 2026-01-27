package integration_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kgatilin/archai/internal/adapter/d2"
	"github.com/kgatilin/archai/internal/adapter/golang"
	"github.com/kgatilin/archai/internal/service"
)

func newTestService() *service.Service {
	return service.NewService(golang.NewReader(), d2.NewReader(), d2.NewWriter())
}

// TestIntegration_GoCodeToD2Diagram tests the full flow from Go code to D2 diagram.
func TestIntegration_GoCodeToD2Diagram(t *testing.T) {
	// Create a temporary test package
	tmpDir := t.TempDir()

	// Create go.mod
	goMod := `module test.example/integration

go 1.21
`
	err := os.WriteFile(filepath.Join(tmpDir, "go.mod"), []byte(goMod), 0644)
	if err != nil {
		t.Fatalf("Failed to create go.mod: %v", err)
	}

	// Create a realistic Go source file
	testCode := `package integration

import "context"

// UserService manages user operations.
// archspec:service
type UserService interface {
	// GetUser retrieves a user by ID.
	GetUser(ctx context.Context, id string) (*User, error)

	// CreateUser creates a new user.
	CreateUser(ctx context.Context, user User) error

	// internal validation method
	validateUser(user User) error
}

// User represents a user entity.
// archspec:aggregate
type User struct {
	ID       string
	Name     string
	Email    string
	password string  // unexported field
}

// UserRepository provides data access for users.
// archspec:repository
type UserRepository interface {
	// FindByID finds a user by ID.
	FindByID(ctx context.Context, id string) (*User, error)

	// Save persists a user.
	Save(ctx context.Context, user User) error
}

// NewUserService creates a new UserService implementation.
// archspec:factory
func NewUserService(repo UserRepository) UserService {
	return &userServiceImpl{repo: repo}
}

// userServiceImpl implements UserService.
type userServiceImpl struct {
	repo UserRepository
}

// GetUser implements UserService.GetUser.
func (s *userServiceImpl) GetUser(ctx context.Context, id string) (*User, error) {
	return s.repo.FindByID(ctx, id)
}

// CreateUser implements UserService.CreateUser.
func (s *userServiceImpl) CreateUser(ctx context.Context, user User) error {
	if err := s.validateUser(user); err != nil {
		return err
	}
	return s.repo.Save(ctx, user)
}

func (s *userServiceImpl) validateUser(user User) error {
	return nil
}
`

	err = os.WriteFile(filepath.Join(tmpDir, "user.go"), []byte(testCode), 0644)
	if err != nil {
		t.Fatalf("Failed to create user.go: %v", err)
	}

	// Change to tmpDir
	oldDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("Failed to get working directory: %v", err)
	}
	defer os.Chdir(oldDir)

	err = os.Chdir(tmpDir)
	if err != nil {
		t.Fatalf("Failed to change directory: %v", err)
	}

	// Create service with real adapters
	svc := newTestService()

	// Test Generate operation
	opts := service.GenerateOptions{
		Paths: []string{"."},
	}

	results, err := svc.Generate(context.Background(), opts)
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	if len(results) != 1 {
		t.Fatalf("Generate() returned %d results, want 1", len(results))
	}

	result := results[0]
	if result.Error != nil {
		t.Fatalf("Generate result error = %v", result.Error)
	}

	// Verify pub.d2 was created
	if result.PubFile == "" {
		t.Fatal("PubFile should not be empty")
	}

	pubContent, err := os.ReadFile(result.PubFile)
	if err != nil {
		t.Fatalf("Failed to read pub.d2: %v", err)
	}

	pubStr := string(pubContent)

	// Verify pub.d2 contains expected content
	expectedInPub := []string{
		"# integration package",
		"# Legend",
		"legend:",
		"# Files",
		"user: {",
		"UserService: {",
		"UserRepository: {",
		"User: {",
		`stereotype: "<<interface>>"`,
		`stereotype: "<<struct>>"`,
		"+GetUser",
		"+CreateUser",
		"+FindByID",
		"+Save",
		"+ID string",
		"+Name string",
		"+Email string",
		"# Dependencies",
	}

	for _, expected := range expectedInPub {
		if !strings.Contains(pubStr, expected) {
			t.Errorf("pub.d2 missing expected content: %q\n\nGot:\n%s", expected, pubStr)
		}
	}

	// Verify unexported symbols are NOT in pub.d2
	unexpectedInPub := []string{
		"userServiceImpl",
		"validateUser",
		"password", // unexported field
	}

	for _, unexpected := range unexpectedInPub {
		if strings.Contains(pubStr, unexpected) {
			t.Errorf("pub.d2 contains unexpected content: %q\n\nGot:\n%s", unexpected, pubStr)
		}
	}

	// Verify internal.d2 was created
	if result.InternalFile == "" {
		t.Fatal("InternalFile should not be empty")
	}

	internalContent, err := os.ReadFile(result.InternalFile)
	if err != nil {
		t.Fatalf("Failed to read internal.d2: %v", err)
	}

	internalStr := string(internalContent)

	// Verify internal.d2 contains both exported and unexported symbols
	expectedInInternal := []string{
		"UserService: {",
		"userServiceImpl: {",
		"+GetUser",
		"-validateUser",
		"+Email string",
		"-password string", // unexported field should be in internal
	}

	for _, expected := range expectedInInternal {
		if !strings.Contains(internalStr, expected) {
			t.Errorf("internal.d2 missing expected content: %q\n\nGot:\n%s", expected, internalStr)
		}
	}
}

// TestIntegration_RealProject tests generating diagrams for the actual archai project.
func TestIntegration_RealProject(t *testing.T) {
	// This test generates diagrams for the actual project
	// Skip if running in CI or if user doesn't want to modify project
	if os.Getenv("CI") != "" {
		t.Skip("Skipping real project test in CI")
	}

	svc := newTestService()

	// Generate diagrams for domain package
	opts := service.GenerateOptions{
		Paths: []string{"github.com/kgatilin/archai/internal/domain"},
	}

	results, err := svc.Generate(context.Background(), opts)
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	if len(results) == 0 {
		t.Fatal("Generate() returned no results")
	}

	// Clean up generated files
	defer func() {
		for _, result := range results {
			if result.PubFile != "" {
				os.Remove(result.PubFile)
			}
			if result.InternalFile != "" {
				os.Remove(result.InternalFile)
			}
			// Remove .arch directory if empty
			if result.PubFile != "" {
				archDir := filepath.Dir(result.PubFile)
				os.Remove(archDir)
			}
		}
	}()

	// Verify diagrams were created
	for _, result := range results {
		if result.Error != nil {
			t.Errorf("Result error for %s: %v", result.PackagePath, result.Error)
			continue
		}

		// Verify files exist
		if result.PubFile != "" {
			if _, err := os.Stat(result.PubFile); os.IsNotExist(err) {
				t.Errorf("PubFile does not exist: %s", result.PubFile)
			} else {
				// Verify it's valid D2
				content, _ := os.ReadFile(result.PubFile)
				if !strings.Contains(string(content), "# Legend") {
					t.Errorf("PubFile %s does not contain legend", result.PubFile)
				}
			}
		}

		if result.InternalFile != "" {
			if _, err := os.Stat(result.InternalFile); os.IsNotExist(err) {
				t.Errorf("InternalFile does not exist: %s", result.InternalFile)
			}
		}
	}
}

// TestIntegration_MultiplePackages tests generating diagrams for multiple packages.
func TestIntegration_MultiplePackages(t *testing.T) {
	tmpDir := t.TempDir()

	// Create go.mod
	goMod := `module test.example/multi

go 1.21
`
	err := os.WriteFile(filepath.Join(tmpDir, "go.mod"), []byte(goMod), 0644)
	if err != nil {
		t.Fatalf("Failed to create go.mod: %v", err)
	}

	// Create pkg1
	pkg1Dir := filepath.Join(tmpDir, "pkg1")
	err = os.Mkdir(pkg1Dir, 0755)
	if err != nil {
		t.Fatalf("Failed to create pkg1: %v", err)
	}

	pkg1Code := `package pkg1

// Service1 is a service.
type Service1 interface {
	DoSomething() error
}
`
	err = os.WriteFile(filepath.Join(pkg1Dir, "service.go"), []byte(pkg1Code), 0644)
	if err != nil {
		t.Fatalf("Failed to create pkg1/service.go: %v", err)
	}

	// Create pkg2
	pkg2Dir := filepath.Join(tmpDir, "pkg2")
	err = os.Mkdir(pkg2Dir, 0755)
	if err != nil {
		t.Fatalf("Failed to create pkg2: %v", err)
	}

	pkg2Code := `package pkg2

// Service2 is a service.
type Service2 interface {
	DoOther() error
}
`
	err = os.WriteFile(filepath.Join(pkg2Dir, "service.go"), []byte(pkg2Code), 0644)
	if err != nil {
		t.Fatalf("Failed to create pkg2/service.go: %v", err)
	}

	// Change to tmpDir
	oldDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("Failed to get working directory: %v", err)
	}
	defer os.Chdir(oldDir)

	err = os.Chdir(tmpDir)
	if err != nil {
		t.Fatalf("Failed to change directory: %v", err)
	}

	// Generate diagrams for both packages
	svc := newTestService()

	opts := service.GenerateOptions{
		Paths: []string{"./pkg1", "./pkg2"},
	}

	results, err := svc.Generate(context.Background(), opts)
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	if len(results) != 2 {
		t.Fatalf("Generate() returned %d results, want 2", len(results))
	}

	// Verify both packages generated diagrams
	for _, result := range results {
		if result.Error != nil {
			t.Errorf("Result error for %s: %v", result.PackagePath, result.Error)
			continue
		}

		if result.PubFile == "" {
			t.Errorf("PubFile empty for %s", result.PackagePath)
		}

		if result.InternalFile == "" {
			t.Errorf("InternalFile empty for %s", result.PackagePath)
		}

		// Verify files exist
		if _, err := os.Stat(result.PubFile); os.IsNotExist(err) {
			t.Errorf("PubFile does not exist: %s", result.PubFile)
		}

		if _, err := os.Stat(result.InternalFile); os.IsNotExist(err) {
			t.Errorf("InternalFile does not exist: %s", result.InternalFile)
		}
	}
}
