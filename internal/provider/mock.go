package provider

import "context"

// MockProvider implements Provider with canned replies and zero API cost.
// Use the Responses queue for simple scripted flows, or set Handler for
// dynamic behavior. Every received Request is recorded in Calls for assertions.
type MockProvider struct {
	model     string
	Responses []Response
	Handler   func(req Request) (Response, error)
	Err       error
	Calls     []Request
}

// NewMock builds a MockProvider that returns the given responses in order.
func NewMock(responses ...Response) *MockProvider {
	return &MockProvider{model: "mock-1", Responses: responses}
}

func (m *MockProvider) Model() string         { return m.model }
func (m *MockProvider) SetModel(model string) { m.model = model }

// StreamMessage runs SendMessage, then replays the resulting text through
// onDelta in small chunks so streaming front-ends can be tested without a
// network provider.
func (m *MockProvider) StreamMessage(ctx context.Context, req Request, onDelta func(string)) (Response, error) {
	resp, err := m.SendMessage(ctx, req)
	if err != nil {
		return Response{}, err
	}
	if onDelta != nil {
		for _, part := range chunkRunes(resp.Text, 8) {
			onDelta(part)
		}
	}
	return resp, nil
}

func chunkRunes(s string, n int) []string {
	r := []rune(s)
	var out []string
	for i := 0; i < len(r); i += n {
		out = append(out, string(r[i:min(i+n, len(r))]))
	}
	return out
}

// SendMessage records the request, then returns (in priority order): a
// configured Err, the Handler's output, the next queued Response, or a bare
// end-of-turn response when the queue is empty.
func (m *MockProvider) SendMessage(_ context.Context, req Request) (Response, error) {
	m.Calls = append(m.Calls, req)
	switch {
	case m.Err != nil:
		return Response{}, m.Err
	case m.Handler != nil:
		return m.Handler(req)
	case len(m.Responses) == 0:
		return Response{StopReason: StopEndTurn}, nil
	default:
		resp := m.Responses[0]
		m.Responses = m.Responses[1:]
		return resp, nil
	}
}
