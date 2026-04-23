// Package target provides storage and management of "target" snapshots:
// frozen copies of a project's per-package architecture specs (.arch/*.yaml)
// plus the overlay (archai.yaml) at a specific point in time.
//
// A target is identified by a human-readable id and lives under
// .arch/targets/<id>/. A single active target is tracked via
// .arch/targets/CURRENT (a one-line file containing the target id).
//
// This package implements the storage layout and management operations
// (lock/list/show/use/delete) for M4a. It does not implement diffing or
// validation; those are handled by later milestones.
package target

// TargetMeta describes a single locked target.
//
// The YAML representation lives at .arch/targets/<id>/meta.yaml.
// Fields:
//   - ID: target identifier, matches the directory name under .arch/targets/.
//   - BaseCommit: git commit hash captured at lock time (output of
//     `git rev-parse HEAD`). Empty if the project is not a git repo.
//   - CreatedAt: RFC3339 timestamp of when Lock was called.
//   - Description: optional free-form description passed via --description.
type TargetMeta struct {
	ID          string `yaml:"id"`
	BaseCommit  string `yaml:"base_commit"`
	CreatedAt   string `yaml:"created_at"`
	Description string `yaml:"description,omitempty"`
}
