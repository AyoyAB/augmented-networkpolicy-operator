package dns

import "context"

// MockResolver is a test double for the Resolver interface.
type MockResolver struct {
	Results map[string][]string
	Err     error
}

// Resolve returns pre-configured results for the given hostname.
func (m *MockResolver) Resolve(_ context.Context, hostname string) ([]string, error) {
	if m.Err != nil {
		return nil, m.Err
	}
	if result, ok := m.Results[hostname]; ok {
		return result, nil
	}
	return nil, nil
}
