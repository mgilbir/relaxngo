// Package validator provides RELAX NG schema validation functionality.
package validator

import (
	"fmt"
	"io"
	"path/filepath"
	"sync"

	"github.com/mgilbir/relaxngo/rng"
)

// CachedValidator maintains a parsed schema and provides efficient validation
// across multiple documents. Thread-safe.
type CachedValidator struct {
	grammar *rng.Grammar
	defines map[string]*rng.Define
	options ValidationOptions
	mu      sync.RWMutex
}

// NewCachedValidatorFromGrammar creates a cached validator from an already-parsed grammar.
// This is useful when the grammar is parsed separately and needs to be reused.
func NewCachedValidatorFromGrammar(grammar *rng.Grammar, options ValidationOptions) *CachedValidator {
	defines := make(map[string]*rng.Define)
	for i := range grammar.Defines {
		defines[grammar.Defines[i].Name] = &grammar.Defines[i]
	}

	return &CachedValidator{
		grammar: grammar,
		defines: defines,
		options: options,
	}
}

// NewCachedValidator creates a cached validator by parsing a schema file.
// The schema is parsed once and cached for efficient reuse.
// basePath is the directory to resolve relative includes from.
func NewCachedValidator(schemaPath string, basePath string, options ValidationOptions) (*CachedValidator, error) {
	if basePath == "" {
		basePath = filepath.Dir(schemaPath)
	}

	grammar, err := rng.ParseSchemaFile(schemaPath, basePath)
	if err != nil {
		return nil, fmt.Errorf("failed to parse schema: %w", err)
	}

	return NewCachedValidatorFromGrammar(grammar, options), nil
}

// Validate validates an XML document against the cached schema.
// This method is thread-safe and can be called concurrently.
func (c *CachedValidator) Validate(r io.Reader) ([]ValidationError, error) {
	c.mu.RLock()
	grammar := c.grammar
	defines := c.defines
	options := c.options
	c.mu.RUnlock()

	v := &Validator{
		grammar: grammar,
		options: options,
		defines: defines,
	}

	return v.Validate(r)
}

// GetGrammar returns the underlying parsed grammar.
// This is useful for code generation or inspection.
func (c *CachedValidator) GetGrammar() *rng.Grammar {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.grammar
}

// UpdateOptions updates the validation options for future validations.
// This is thread-safe.
func (c *CachedValidator) UpdateOptions(opts ValidationOptions) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.options = opts
}

// GetOptions returns the current validation options.
func (c *CachedValidator) GetOptions() ValidationOptions {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.options
}
