# Agent Instructions

## Examples and Privacy

- **Never add real repository code, proprietary architecture maps, or internal project data to `examples/`**:
  - `examples/` must only contain fictional, generic mock examples (such as the existing `examples/shop.smap` e-commerce example).
  - Strictly prohibit leaking private or proprietary repository information, node names, architecture structures, or code snippets into committed examples or tests.
  - When writing regression tests or demonstrating new features, always construct abstract or fictional mock models instead of copying real project files.

## Testing and Verification

- Run `go test -count=1 ./...` and `go vet ./...` before committing changes.
- When modifying `internal/smap/template.html`, update the baseline length and SHA256 in `internal/smap/tasks_test.go` (`TestViewerWithoutTasksKeepsTheExactBaseline`).
