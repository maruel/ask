// Copyright 2026 Marc-Antoine Ruel. All rights reserved.
// Use of this source code is governed under the Apache License, Version 2.0
// that can be found in the LICENSE file.

// Tests for batch provider ownership and cleanup errors.
package main

import (
	"context"
	"errors"
	"testing"

	"github.com/maruel/genai"
	"github.com/maruel/genai/providers"
)

func TestLoadProviderGenAsync(t *testing.T) {
	t.Run("error", func(t *testing.T) {
		errClose := errors.New("close failed")
		c := &fakeProvider{errClose: errClose}
		const name = "test-batch-cleanup"
		providers.All[name] = providers.Config{
			Factory: func(context.Context, ...genai.ProviderOption) (genai.Provider, error) {
				return c, nil
			},
		}
		t.Cleanup(func() { delete(providers.All, name) })
		p, err := loadProviderGenAsync(t.Context(), name)
		if p != nil || err == nil {
			t.Fatalf("got (%v, %v), want unsupported provider error", p, err)
		}
		if !c.closed {
			t.Error("rejected provider still holds resources")
		}
		if !errors.Is(err, errClose) {
			t.Errorf("got %v, want close error", err)
		}
	})
}

type fakeProvider struct {
	genai.Provider
	closed   bool
	errClose error
}

func (c *fakeProvider) Capabilities() genai.ProviderCapabilities {
	return genai.ProviderCapabilities{}
}

func (c *fakeProvider) Close() error {
	c.closed = true
	return c.errClose
}
