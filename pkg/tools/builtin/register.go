package builtin

import "git_sonic/pkg/tools"

// RegisterAll registers all built-in tools with the given registry.
func RegisterAll(registry *tools.Registry) {
	RegisterFileTools(registry)
	RegisterBashTools(registry)
	RegisterGitTools(registry)
	RegisterGitHubTools(registry)
}

// NewRegistryWithBuiltins creates a new registry with all built-in tools registered.
func NewRegistryWithBuiltins() *tools.Registry {
	registry := tools.NewRegistry()
	RegisterAll(registry)
	return registry
}
