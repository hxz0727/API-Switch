package resilience

import (
	"context"
	"errors"
	"io"
	"sync"

	"github.com/hxz0727/API-Switch/pkg/anthropic"
)

// fakeProvider is a minimal provider.Provider implementation for testing
// resilience logic without making real network calls.
type fakeProvider struct {
	name string

	mu         sync.Mutex
	pingErr    error
	pingCount  int
	sendErr    error
	streamErr  error
	streamBody io.ReadCloser
	models     []string
	modelsErr  error
}

func newFakeProvider(name string) *fakeProvider {
	return &fakeProvider{name: name}
}

func (f *fakeProvider) Name() string { return f.name }

func (f *fakeProvider) Type() string { return "fake" }

func (f *fakeProvider) SendMessage(context.Context, *anthropic.MessagesRequest, string, int) (*anthropic.MessagesResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.sendErr != nil {
		return nil, f.sendErr
	}
	return &anthropic.MessagesResponse{}, nil
}

func (f *fakeProvider) StreamMessage(context.Context, *anthropic.MessagesRequest, string, int) (io.ReadCloser, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.streamErr != nil {
		return nil, f.streamErr
	}
	return f.streamBody, nil
}

func (f *fakeProvider) Ping() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.pingCount++
	return f.pingErr
}

func (f *fakeProvider) ListModels() ([]string, error) {
	return f.models, f.modelsErr
}

func (f *fakeProvider) setPingErr(err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.pingErr = err
}

func (f *fakeProvider) setSendErr(err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sendErr = err
}

func (f *fakeProvider) pingCalls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.pingCount
}

func errTest(msg string) error { return errors.New(msg) }
