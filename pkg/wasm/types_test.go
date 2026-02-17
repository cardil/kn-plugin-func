package wasm

import (
	"testing"
)

func TestIsWasiRuntime(t *testing.T) {
	tests := []struct {
		name     string
		runtime  string
		expected bool
	}{
		{"rust-wasi", RuntimeRustWasi, true},
		{"go-wasi", RuntimeGoWasi, true},
		{"python-wasi", RuntimePythonWasi, true},
		{"js-wasi", RuntimeJsWasi, true},
		{"c-wasi", RuntimeCWasi, true},
		{"cpp-wasi", RuntimeCppWasi, true},
		{"dotnet-wasi", RuntimeDotnetWasi, true},
		{"swift-wasi", RuntimeSwiftWasi, true},
		{"node (not wasi)", "node", false},
		{"python (not wasi)", "python", false},
		{"go (not wasi)", "go", false},
		{"rust (not wasi)", "rust", false},
		{"empty string", "", false},
		{"unknown runtime", "unknown-wasi", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := IsWasiRuntime(tt.runtime)
			if result != tt.expected {
				t.Errorf("IsWasiRuntime(%q) = %v, want %v", tt.runtime, result, tt.expected)
			}
		})
	}
}
