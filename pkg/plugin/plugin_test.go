package plugin_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"sync"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"

	"github.com/freality/dexter/pkg/plugin"
	"github.com/freality/dexter/pkg/plugin/pb"
)

// fakeAdapter implements plugin.Adapter with configurable behavior for tests.
type fakeAdapter struct {
	applyErr   error
	destroyErr error
	applyFn    func(ctx context.Context, req plugin.Request, r plugin.Reporter) (map[string]any, error)
	destroyFn  func(ctx context.Context, req plugin.Request, r plugin.Reporter) error

	mu           sync.Mutex
	applyCalls   []plugin.Request
	destroyCalls []plugin.Request
}

func (a *fakeAdapter) Apply(ctx context.Context, req plugin.Request, r plugin.Reporter) (map[string]any, error) {
	a.mu.Lock()
	a.applyCalls = append(a.applyCalls, req)
	a.mu.Unlock()
	if a.applyFn != nil {
		return a.applyFn(ctx, req, r)
	}
	if a.applyErr != nil {
		return nil, a.applyErr
	}
	return nil, nil
}

func (a *fakeAdapter) Destroy(ctx context.Context, req plugin.Request, r plugin.Reporter) error {
	a.mu.Lock()
	a.destroyCalls = append(a.destroyCalls, req)
	a.mu.Unlock()
	if a.destroyFn != nil {
		return a.destroyFn(ctx, req, r)
	}
	if a.destroyErr != nil {
		return a.destroyErr
	}
	return nil
}

// recordingReporter records all Log and Progress calls in order.
type recordingReporter struct {
	mu    sync.Mutex
	logs  []string
	pcts  []int
	order []string
}

func (r *recordingReporter) Log(msg string) {
	r.mu.Lock()
	r.logs = append(r.logs, msg)
	r.order = append(r.order, "log")
	r.mu.Unlock()
}

func (r *recordingReporter) Progress(pct int) {
	r.mu.Lock()
	r.pcts = append(r.pcts, pct)
	r.order = append(r.order, "progress")
	r.mu.Unlock()
}

// serveAdapter starts an in-process gRPC server for the given plugin.Adapter
// and returns a pb.AdapterClient and a cleanup func.
func serveAdapter(t *testing.T, a plugin.Adapter) (pb.AdapterClient, func()) {
	t.Helper()
	listener := bufconn.Listen(1024 * 1024)
	srv := grpc.NewServer()
	pb.RegisterAdapterServer(srv, &grpcAdapter{impl: a})
	go srv.Serve(listener)

	conn, err := grpc.Dial("passthrough:///bufconn",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) { return listener.DialContext(ctx) }),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithBlock(),
	)
	if err != nil {
		t.Fatal(err)
	}
	return pb.NewAdapterClient(conn), func() {
		conn.Close()
		srv.Stop()
	}
}

// grpcAdapter wraps plugin.Adapter as a pb.AdapterServer using the same logic
// as the production grpcServer in plugin.go — tested against the same wire contract.
type grpcAdapter struct {
	pb.UnimplementedAdapterServer
	impl plugin.Adapter
}

func (s *grpcAdapter) Apply(req *pb.ApplyRequest, stream grpc.ServerStreamingServer[pb.ApplyResponse]) error {
	r := plugin.Request{Name: req.ResourceId, Type: req.Type, Properties: unmarshalJSON(req.PropertiesJson)}
	if req.PriorPropertiesJson != "" {
		r.Prior = unmarshalJSON(req.PriorPropertiesJson)
	}
	rep := &streamReporter{stream: stream}

	result, err := s.impl.Apply(stream.Context(), r, rep)
	if err != nil {
		stream.Send(&pb.ApplyResponse{Type: pb.ApplyResponse_ERROR, Message: err.Error()})
		return nil
	}
	var jsonStr string
	if len(result) > 0 {
		jsonStr = marshalJSON(result)
	}
	stream.Send(&pb.ApplyResponse{Type: pb.ApplyResponse_SUCCESS, NewStateJson: jsonStr})
	return nil
}

func (s *grpcAdapter) Destroy(req *pb.DestroyRequest, stream grpc.ServerStreamingServer[pb.ApplyResponse]) error {
	r := plugin.Request{Name: req.ResourceId, Type: req.Type, Properties: unmarshalJSON(req.PropertiesJson)}
	if req.OutputsJson != "" {
		r.Outputs = unmarshalJSON(req.OutputsJson)
	}
	rep := &streamReporter{stream: stream}

	err := s.impl.Destroy(stream.Context(), r, rep)
	if err != nil {
		stream.Send(&pb.ApplyResponse{Type: pb.ApplyResponse_ERROR, Message: err.Error()})
		return nil
	}
	stream.Send(&pb.ApplyResponse{Type: pb.ApplyResponse_SUCCESS})
	return nil
}

type streamReporter struct {
	stream grpc.ServerStreamingServer[pb.ApplyResponse]
}

func (r *streamReporter) Log(msg string) {
	r.stream.Send(&pb.ApplyResponse{Type: pb.ApplyResponse_LOG, Message: msg})
}
func (r *streamReporter) Progress(pct int) {
	r.stream.Send(&pb.ApplyResponse{Type: pb.ApplyResponse_PROGRESS, Message: fmt.Sprintf("%d", pct)})
}

func unmarshalJSON(s string) map[string]any {
	if s == "" {
		return nil
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(s), &m); err != nil {
		panic(err)
	}
	return m
}

func marshalJSON(v map[string]any) string {
	if len(v) == 0 {
		return ""
	}
	b, _ := json.Marshal(v)
	return string(b)
}

// --- Tests ---

func TestCreate(t *testing.T) {
	fake := &fakeAdapter{}
	client, cleanup := serveAdapter(t, fake)
	defer cleanup()

	rep := &recordingReporter{}
	stream, err := client.Apply(context.Background(), &pb.ApplyRequest{
		ResourceId: "test-rsc", Type: "test",
		PropertiesJson: `{"key":"value"}`,
	})
	if err != nil {
		t.Fatalf("Apply RPC: %v", err)
	}

	_, err = plugin.CollectResult("test", stream, rep)
	if err != nil {
		t.Fatalf("CollectResult: %v", err)
	}

	fake.mu.Lock()
	req := fake.applyCalls[0]
	fake.mu.Unlock()

	if req.Name != "test-rsc" {
		t.Errorf("Name = %q", req.Name)
	}
	if req.Type != "test" {
		t.Errorf("Type = %q", req.Type)
	}
	if v, _ := req.Properties["key"].(string); v != "value" {
		t.Errorf("Properties[key] = %v", req.Properties["key"])
	}
	if req.Prior != nil {
		t.Errorf("Prior = %v, want nil", req.Prior)
	}
}

func TestUpdate(t *testing.T) {
	fake := &fakeAdapter{}
	client, cleanup := serveAdapter(t, fake)
	defer cleanup()

	rep := &recordingReporter{}
	stream, err := client.Apply(context.Background(), &pb.ApplyRequest{
		ResourceId: "test-rsc", Type: "test",
		PropertiesJson:      `{"key":"v2"}`,
		PriorPropertiesJson: `{"key":"v1"}`,
	})
	if err != nil {
		t.Fatalf("Apply RPC: %v", err)
	}

	_, err = plugin.CollectResult("test", stream, rep)
	if err != nil {
		t.Fatalf("CollectResult: %v", err)
	}

	fake.mu.Lock()
	req := fake.applyCalls[0]
	fake.mu.Unlock()

	if v, _ := req.Prior["key"].(string); v != "v1" {
		t.Errorf("Prior[key] = %v", req.Prior["key"])
	}
}

func TestDestroy(t *testing.T) {
	fake := &fakeAdapter{}
	client, cleanup := serveAdapter(t, fake)
	defer cleanup()

	rep := &recordingReporter{}
	stream, err := client.Destroy(context.Background(), &pb.DestroyRequest{
		ResourceId: "test-rsc", Type: "test",
		PropertiesJson: `{"key":"value"}`,
		OutputsJson:    `{"ip":"10.0.0.1"}`,
	})
	if err != nil {
		t.Fatalf("Destroy RPC: %v", err)
	}

	_, err = plugin.CollectResult("test", stream, rep)
	if err != nil {
		t.Fatalf("CollectResult: %v", err)
	}

	fake.mu.Lock()
	req := fake.destroyCalls[0]
	fake.mu.Unlock()

	if v, _ := req.Outputs["ip"].(string); v != "10.0.0.1" {
		t.Errorf("Outputs[ip] = %v", req.Outputs["ip"])
	}
}

func TestLogAndProgressOrdering(t *testing.T) {
	fake := &fakeAdapter{
		applyFn: func(_ context.Context, _ plugin.Request, r plugin.Reporter) (map[string]any, error) {
			r.Log("pulling image")
			r.Progress(50)
			return nil, nil
		},
	}
	client, cleanup := serveAdapter(t, fake)
	defer cleanup()

	rep := &recordingReporter{}
	stream, err := client.Apply(context.Background(), &pb.ApplyRequest{
		ResourceId: "rsc", Type: "test",
	})
	if err != nil {
		t.Fatalf("Apply RPC: %v", err)
	}

	_, err = plugin.CollectResult("rsc", stream, rep)
	if err != nil {
		t.Fatalf("CollectResult: %v", err)
	}

	if len(rep.order) != 2 || rep.order[0] != "log" || rep.order[1] != "progress" {
		t.Errorf("order = %v, want [log progress]", rep.order)
	}
	if len(rep.logs) != 1 || rep.logs[0] != "pulling image" {
		t.Errorf("logs = %v", rep.logs)
	}
	if len(rep.pcts) != 1 || rep.pcts[0] != 50 {
		t.Errorf("pcts = %v", rep.pcts)
	}
}

func TestApplyError(t *testing.T) {
	fake := &fakeAdapter{applyErr: fmt.Errorf("connection refused")}
	client, cleanup := serveAdapter(t, fake)
	defer cleanup()

	rep := &recordingReporter{}
	stream, err := client.Apply(context.Background(), &pb.ApplyRequest{
		ResourceId: "rsc", Type: "test",
	})
	if err != nil {
		t.Fatalf("Apply RPC: %v", err)
	}

	_, err = plugin.CollectResult("rsc", stream, rep)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestDestroyError(t *testing.T) {
	fake := &fakeAdapter{destroyErr: fmt.Errorf("container not found")}
	client, cleanup := serveAdapter(t, fake)
	defer cleanup()

	rep := &recordingReporter{}
	stream, err := client.Destroy(context.Background(), &pb.DestroyRequest{
		ResourceId: "rsc", Type: "test",
	})
	if err != nil {
		t.Fatalf("Destroy RPC: %v", err)
	}

	_, err = plugin.CollectResult("rsc", stream, rep)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestNewStateReturned(t *testing.T) {
	fake := &fakeAdapter{
		applyFn: func(_ context.Context, _ plugin.Request, _ plugin.Reporter) (map[string]any, error) {
			return map[string]any{"ip": "10.0.0.1", "port": float64(8080)}, nil
		},
	}
	client, cleanup := serveAdapter(t, fake)
	defer cleanup()

	rep := &recordingReporter{}
	stream, err := client.Apply(context.Background(), &pb.ApplyRequest{
		ResourceId: "rsc", Type: "test",
	})
	if err != nil {
		t.Fatalf("Apply RPC: %v", err)
	}

	result, err := plugin.CollectResult("rsc", stream, rep)
	if err != nil {
		t.Fatalf("CollectResult: %v", err)
	}
	if v, _ := result["ip"].(string); v != "10.0.0.1" {
		t.Errorf("result[ip] = %v", result["ip"])
	}
}

// TestStreamEndedWithoutResult verifies that a misbehaving server that closes
// the stream without SUCCESS/ERROR is detected as a failure.
func TestStreamEndedWithoutResult(t *testing.T) {
	// Register a pb.AdapterServer directly (not through a plugin.Adapter) so
	// we can simulate a server that closes the stream without a terminal.
	listener := bufconn.Listen(1024 * 1024)
	srv := grpc.NewServer()
	pb.RegisterAdapterServer(srv, &misbehavingAdapter{})
	go srv.Serve(listener)

	conn, err := grpc.Dial("passthrough:///bufconn",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) { return listener.DialContext(ctx) }),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithBlock(),
	)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	defer srv.Stop()
	client := pb.NewAdapterClient(conn)

	rep := &recordingReporter{}
	stream, err := client.Apply(context.Background(), &pb.ApplyRequest{
		ResourceId: "rsc", Type: "test",
	})
	if err != nil {
		t.Fatalf("Apply RPC: %v", err)
	}

	_, err = plugin.CollectResult("rsc", stream, rep)
	if err == nil {
		t.Fatal("expected error for stream without terminal result, got nil")
	}
}

// misbehavingAdapter sends a LOG message then returns without SUCCESS/ERROR.
type misbehavingAdapter struct {
	pb.UnimplementedAdapterServer
}

func (a *misbehavingAdapter) Apply(req *pb.ApplyRequest, stream grpc.ServerStreamingServer[pb.ApplyResponse]) error {
	stream.Send(&pb.ApplyResponse{Type: pb.ApplyResponse_LOG, Message: "hello"})
	return nil
}

func (a *misbehavingAdapter) Destroy(req *pb.DestroyRequest, stream grpc.ServerStreamingServer[pb.ApplyResponse]) error {
	stream.Send(&pb.ApplyResponse{Type: pb.ApplyResponse_LOG, Message: "hello"})
	return nil
}

func TestContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	adapterDone := make(chan struct{})

	fake := &fakeAdapter{
		applyFn: func(ctx context.Context, _ plugin.Request, _ plugin.Reporter) (map[string]any, error) {
			<-ctx.Done()
			close(adapterDone)
			return nil, ctx.Err()
		},
	}
	client, cleanup := serveAdapter(t, fake)
	defer cleanup()

	rep := &recordingReporter{}
	stream, err := client.Apply(ctx, &pb.ApplyRequest{
		ResourceId: "rsc", Type: "test",
	})
	if err != nil {
		t.Fatalf("Apply RPC: %v", err)
	}

	// Give the adapter goroutine time to reach the <-ctx.Done().
	time.Sleep(50 * time.Millisecond)

	cancel()

	select {
	case <-adapterDone:
		// Correct — cancellation reached the adapter.
	case <-time.After(5 * time.Second):
		t.Fatal("context cancellation did not reach adapter within 5s")
	}

	// The stream should end with the cancelled error.
	_, _ = plugin.CollectResult("rsc", stream, rep)
}

func TestHandshakeConstants(t *testing.T) {
	if plugin.Handshake.ProtocolVersion != 1 {
		t.Errorf("ProtocolVersion = %d", plugin.Handshake.ProtocolVersion)
	}
	if plugin.Handshake.MagicCookieKey != "DEX_PLUGIN" {
		t.Errorf("MagicCookieKey = %q", plugin.Handshake.MagicCookieKey)
	}
}

// TestEmptyPriorMapsToNil ensures the server passes nil Prior when the proto
// field is empty, distinguishing create from update.
func TestEmptyPriorMapsToNil(t *testing.T) {
	fake := &fakeAdapter{}
	client, cleanup := serveAdapter(t, fake)
	defer cleanup()

	rep := &recordingReporter{}
	stream, err := client.Apply(context.Background(), &pb.ApplyRequest{
		ResourceId: "rsc", Type: "test",
	})
	if err != nil {
		t.Fatalf("Apply RPC: %v", err)
	}

	_, err = plugin.CollectResult("rsc", stream, rep)
	if err != nil {
		t.Fatalf("CollectResult: %v", err)
	}

	fake.mu.Lock()
	req := fake.applyCalls[0]
	fake.mu.Unlock()

	if req.Prior != nil {
		t.Errorf("Prior = %v, want nil for create (empty prior_properties_json)", req.Prior)
	}
}
