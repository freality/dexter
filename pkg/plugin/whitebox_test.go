package plugin

import (
	"context"
	"fmt"
	"net"
	"strings"
	"testing"
	"time"

	goplugin "github.com/hashicorp/go-plugin"

	"github.com/freality/dexter/pkg/plugin/pb"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"
)

// ---------------- test helpers ----------------

func whiteboxPair(t *testing.T, a Adapter) (Adapter, func()) {
	t.Helper()
	listener := bufconn.Listen(1024 * 1024)
	srv := grpc.NewServer()
	pb.RegisterAdapterServer(srv, &grpcServer{impl: a})
	go srv.Serve(listener)

	conn, err := grpc.Dial(
		"passthrough:///bufconn",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
			return listener.DialContext(ctx)
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithBlock(),
	)
	if err != nil {
		t.Fatal(err)
	}
	return &grpcClient{client: pb.NewAdapterClient(conn)}, func() {
		conn.Close()
		srv.Stop()
	}
}

type recorder struct {
	Logs  []string
	Pcts  []int
	Order []string
}

func (r *recorder) Log(msg string) {
	r.Logs = append(r.Logs, msg)
	r.Order = append(r.Order, "log")
}

func (r *recorder) Progress(pct int) {
	r.Pcts = append(r.Pcts, pct)
	r.Order = append(r.Order, "progress")
}

type countAdapter struct {
	applyCount   int
	destroyCount int
	logMsg       string
	pct          int
	returnErr    error
	blockCh      chan struct{}
}

func (a *countAdapter) Apply(ctx context.Context, req Request, r Reporter) (map[string]any, error) {
	a.applyCount++
	if a.blockCh != nil {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	if a.logMsg != "" {
		r.Log(a.logMsg)
	}
	if a.pct > 0 {
		r.Progress(a.pct)
	}
	return map[string]any{"applied": true}, a.returnErr
}

func (a *countAdapter) Destroy(ctx context.Context, req Request, r Reporter) error {
	a.destroyCount++
	if a.blockCh != nil {
		<-ctx.Done()
		return ctx.Err()
	}
	return a.returnErr
}

// mockPluginClient implements the subset of  clientInterface needed by Open.
type mockPluginClient struct {
	dispenseErr error
}

func (m *mockPluginClient) Client() (goplugin.ClientProtocol, error) {
	return &mockClientProtocol{dispenseErr: m.dispenseErr}, nil
}

func (m *mockPluginClient) Kill() {}

type mockClientProtocol struct {
	dispenseErr error
}

func (m *mockClientProtocol) Dispense(name string) (any, error) {
	if m.dispenseErr != nil {
		return nil, m.dispenseErr
	}
	// Return a simple adapter stub backed by countAdapter.
	return &mockAdapter{}, nil
}

func (m *mockClientProtocol) Ping() error  { return nil }
func (m *mockClientProtocol) Close() error { return nil }

type mockAdapter struct{}

func (m *mockAdapter) Apply(ctx context.Context, req Request, r Reporter) (map[string]any, error) {
	return map[string]any{"done": true}, nil
}

func (m *mockAdapter) Destroy(ctx context.Context, req Request, r Reporter) error {
	return nil
}

// ---------------- Serve / Open path tests ----------------

// ---------------- watchOrphan ----------------

// TestWatchOrphanExitsOnReparent verifies watchOrphan calls osExit once the
// current ppid no longer matches the one it was started with.
func TestWatchOrphanExitsOnReparent(t *testing.T) {
	origInterval := orphanCheckInterval
	orphanCheckInterval = time.Millisecond
	defer func() { orphanCheckInterval = origInterval }()

	origExit := osExit
	exited := make(chan struct{})
	osExit = func(int) { close(exited) }
	defer func() { osExit = origExit }()

	origGetppid := osGetppid
	// Report a changed ppid on every call, forcing the "reparented" branch
	// on the first tick.
	osGetppid = func() int { return 1 }
	defer func() { osGetppid = origGetppid }()

	done := make(chan struct{})
	go func() { watchOrphan(0, nil); close(done) }()

	select {
	case <-exited:
	case <-time.After(2 * time.Second):
		t.Fatal("watchOrphan did not call osExit after a ppid change")
	}
	// watchOrphan must return after calling osExit (osExit doesn't, in the
	// stub, unlike the real os.Exit), so the goroutine doesn't keep running
	// with the seams restored out from under it.
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("watchOrphan did not return after calling osExit")
	}
}

// TestWatchOrphanKeepsPollingWhileParentAlive verifies watchOrphan does NOT
// exit while the parent is unchanged (the common case: the host is still
// running).
func TestWatchOrphanKeepsPollingWhileParentAlive(t *testing.T) {
	origInterval := orphanCheckInterval
	orphanCheckInterval = time.Millisecond
	defer func() { orphanCheckInterval = origInterval }()

	exited := make(chan struct{})
	origExit := osExit
	osExit = func(int) { close(exited) }
	defer func() { osExit = origExit }()

	stop := make(chan struct{})
	done := make(chan struct{})
	go func() { watchOrphan(osGetppid(), stop); close(done) }()

	select {
	case <-exited:
		t.Fatal("watchOrphan called osExit while the parent was unchanged")
	case <-time.After(50 * time.Millisecond):
	}

	// Stop the watch and confirm it actually ends the goroutine, covering
	// the stop-channel branch (Serve's own shutdown path).
	close(stop)
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("watchOrphan did not return after stop was closed")
	}
}

func TestServeCallsPluginServe(t *testing.T) {
	called := false
	pluginServe = func(cfg *goplugin.ServeConfig) {
		called = true
		if cfg.HandshakeConfig.ProtocolVersion != 1 {
			t.Errorf("ProtocolVersion = %d", cfg.HandshakeConfig.ProtocolVersion)
		}
		if cfg.GRPCServer == nil {
			t.Error("GRPCServer is nil")
		}
	}
	defer func() { pluginServe = goplugin.Serve }()

	Serve(&countAdapter{})
	if !called {
		t.Error("pluginServe was not called")
	}
}

func TestOpenSuccessPath(t *testing.T) {
	mockClient := &mockPluginClient{}
	pluginNewClient = func(cfg *goplugin.ClientConfig) clientInterface {
		if cfg.HandshakeConfig.ProtocolVersion != 1 {
			t.Errorf("ProtocolVersion = %d", cfg.HandshakeConfig.ProtocolVersion)
		}
		return mockClient
	}
	defer func() {
		pluginNewClient = func(cfg *goplugin.ClientConfig) clientInterface { return goplugin.NewClient(cfg) }
	}()

	var logBuf strings.Builder
	adapter, kill, err := Open("/path/to/adapter", &logBuf)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if adapter == nil {
		t.Error("adapter is nil")
	}
	if kill == nil {
		t.Error("kill is nil")
	}
	_ = kill
}

func TestOpenDispenseFailure(t *testing.T) {
	mockClient := &mockPluginClient{dispenseErr: fmt.Errorf("unknown plugin")}
	pluginNewClient = func(cfg *goplugin.ClientConfig) clientInterface { return mockClient }
	defer func() {
		pluginNewClient = func(cfg *goplugin.ClientConfig) clientInterface { return goplugin.NewClient(cfg) }
	}()

	var logBuf strings.Builder
	_, kill, err := Open("/path/to/adapter", &logBuf)
	if err == nil {
		t.Fatal("expected Dispense error")
	}
	_ = kill
}

// ---------------- Direct gRPC round-trip tests ----------------

func TestWhiteboxApplyCreate(t *testing.T) {
	impl := &countAdapter{}
	client, cleanup := whiteboxPair(t, impl)
	defer cleanup()

	result, err := client.Apply(context.Background(), Request{Name: "rsc", Type: "test", Properties: map[string]any{"key": "val"}}, &recorder{})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if result["applied"] != true {
		t.Errorf("result[applied] = %v", result)
	}
}

func TestWhiteboxApplyUpdate(t *testing.T) {
	impl := &countAdapter{}
	client, cleanup := whiteboxPair(t, impl)
	defer cleanup()

	_, err := client.Apply(context.Background(), Request{
		Name: "rsc", Type: "test",
		Properties: map[string]any{"key": "v2"},
		Prior:      map[string]any{"key": "v1"},
	}, &recorder{})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
}

func TestWhiteboxApplyLogAndProgress(t *testing.T) {
	impl := &countAdapter{logMsg: "hello", pct: 75}
	client, cleanup := whiteboxPair(t, impl)
	defer cleanup()

	rep := &recorder{}
	_, err := client.Apply(context.Background(), Request{Name: "rsc", Type: "test"}, rep)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if len(rep.Order) != 2 || rep.Order[0] != "log" || rep.Order[1] != "progress" {
		t.Errorf("order = %v", rep.Order)
	}
	if len(rep.Logs) != 1 || rep.Logs[0] != "hello" {
		t.Errorf("logs = %v", rep.Logs)
	}
	if len(rep.Pcts) != 1 || rep.Pcts[0] != 75 {
		t.Errorf("pcts = %v", rep.Pcts)
	}
}

func TestWhiteboxApplyError(t *testing.T) {
	impl := &countAdapter{returnErr: fmt.Errorf("boom")}
	client, cleanup := whiteboxPair(t, impl)
	defer cleanup()

	_, err := client.Apply(context.Background(), Request{Name: "rsc", Type: "test"}, &recorder{})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestWhiteboxDestroyOK(t *testing.T) {
	impl := &countAdapter{}
	client, cleanup := whiteboxPair(t, impl)
	defer cleanup()

	err := client.Destroy(context.Background(), Request{
		Name: "rsc", Type: "test",
		Properties: map[string]any{"key": "val"},
		Outputs:    map[string]any{"out": "val"},
	}, &recorder{})
	if err != nil {
		t.Fatalf("Destroy: %v", err)
	}
}

func TestWhiteboxDestroyError(t *testing.T) {
	impl := &countAdapter{returnErr: fmt.Errorf("gone")}
	client, cleanup := whiteboxPair(t, impl)
	defer cleanup()

	err := client.Destroy(context.Background(), Request{Name: "rsc", Type: "test"}, &recorder{})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestWhiteboxDestroyNilOutputs(t *testing.T) {
	impl := &countAdapter{}
	client, cleanup := whiteboxPair(t, impl)
	defer cleanup()

	err := client.Destroy(context.Background(), Request{
		Name: "rsc", Type: "test",
		Properties: map[string]any{"key": "val"},
	}, &recorder{})
	if err != nil {
		t.Fatalf("Destroy: %v", err)
	}
}

func TestWhiteboxContextCancel(t *testing.T) {
	impl := &countAdapter{blockCh: make(chan struct{})}
	client, cleanup := whiteboxPair(t, impl)
	defer cleanup()

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() {
		_, err := client.Apply(ctx, Request{Name: "rsc", Type: "test"}, &recorder{})
		errCh <- err
	}()

	time.Sleep(50 * time.Millisecond)
	cancel()

	select {
	case err := <-errCh:
		if err == nil {
			t.Fatal("expected error after cancellation")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for cancelled Apply")
	}
}

func TestWhiteboxNilProperties(t *testing.T) {
	impl := &countAdapter{}
	client, cleanup := whiteboxPair(t, impl)
	defer cleanup()

	_, err := client.Apply(context.Background(), Request{Name: "rsc", Type: "test"}, &recorder{})
	if err != nil {
		t.Fatalf("Apply with nil Properties: %v", err)
	}
}

func TestMustMarshalNil(t *testing.T) {
	if s := mustMarshal(nil); s != "" {
		t.Errorf("mustMarshal(nil) = %q, want empty", s)
	}
}

func TestMustUnmarshalMapEmpty(t *testing.T) {
	if m := mustUnmarshalMap(""); m != nil {
		t.Errorf("mustUnmarshalMap empty = %v, want nil", m)
	}
}

func TestPanicMarshal(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic")
		}
	}()
	mustMarshal(map[string]any{"key": make(chan int)})
}

func TestPanicUnmarshal(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic")
		}
	}()
	mustUnmarshalMap("{bad json}")
}

func TestAdapterPluginGRPCServer(t *testing.T) {
	p := &adapterPlugin{impl: &countAdapter{}}
	s := grpc.NewServer()
	err := p.GRPCServer(nil, s)
	if err != nil {
		t.Fatalf("GRPCServer: %v", err)
	}
	if _, ok := s.GetServiceInfo()[pb.Adapter_ServiceDesc.ServiceName]; !ok {
		t.Errorf("Adapter service not registered")
	}
}

func TestAdapterPluginGRPCClient(t *testing.T) {
	p := &adapterPlugin{}
	listener := bufconn.Listen(1024 * 1024)
	srv := grpc.NewServer()
	pb.RegisterAdapterServer(srv, &grpcServer{impl: &countAdapter{}})
	go srv.Serve(listener)
	defer srv.Stop()

	conn, err := grpc.Dial(
		"passthrough:///bufconn",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) { return listener.DialContext(ctx) }),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithBlock(),
	)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	raw, err := p.GRPCClient(context.Background(), nil, conn)
	if err != nil {
		t.Fatalf("GRPCClient: %v", err)
	}
	if _, ok := raw.(Adapter); !ok {
		t.Errorf("GRPCClient returned %T, want Adapter", raw)
	}
}

func TestClientApplyStreamError(t *testing.T) {
	listener := bufconn.Listen(1024 * 1024)
	srv := grpc.NewServer()
	pb.RegisterAdapterServer(srv, &grpcServer{impl: &countAdapter{}})
	go srv.Serve(listener)
	srv.Stop()

	conn, err := grpc.Dial(
		"passthrough:///bufconn",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) { return listener.DialContext(ctx) }),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err == nil {
		defer conn.Close()
		client := &grpcClient{client: pb.NewAdapterClient(conn)}
		_, err = client.Apply(context.Background(), Request{Name: "rsc", Type: "test"}, &recorder{})
		if err == nil {
			t.Fatal("expected error from Apply with dead connection")
		}
	}
}

func TestClientDestroyStreamError(t *testing.T) {
	listener := bufconn.Listen(1024 * 1024)
	srv := grpc.NewServer()
	pb.RegisterAdapterServer(srv, &grpcServer{impl: &countAdapter{}})
	go srv.Serve(listener)
	srv.Stop()

	conn, err := grpc.Dial(
		"passthrough:///bufconn",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) { return listener.DialContext(ctx) }),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err == nil {
		defer conn.Close()
		client := &grpcClient{client: pb.NewAdapterClient(conn)}
		err = client.Destroy(context.Background(), Request{Name: "rsc", Type: "test"}, &recorder{})
		if err == nil {
			t.Fatal("expected error from Destroy with dead connection")
		}
	}
}

func TestOpenNonExistentPath(t *testing.T) {
	var logBuf strings.Builder
	_, kill, err := Open("/nonexistent/dex-adapter-test", &logBuf)
	if err == nil {
		t.Fatal("expected error for non-existent binary")
	}
	_ = kill
}

func TestCollectResultRecvError(t *testing.T) {
	listener := bufconn.Listen(1024 * 1024)
	srv := grpc.NewServer()
	pb.RegisterAdapterServer(srv, &recvErrServer{})
	go srv.Serve(listener)
	defer srv.Stop()

	conn, err := grpc.Dial(
		"passthrough:///bufconn",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
			return listener.DialContext(ctx)
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithBlock(),
	)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	stream, err := pb.NewAdapterClient(conn).Apply(context.Background(), &pb.ApplyRequest{ResourceId: "rsc", Type: "test"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = CollectResult("rsc", stream, &recorder{})
	if err == nil || !strings.Contains(err.Error(), "simulated recv error") {
		t.Fatalf("unexpected error: %v", err)
	}
}

type recvErrServer struct {
	pb.UnimplementedAdapterServer
}

func (s *recvErrServer) Apply(req *pb.ApplyRequest, stream grpc.ServerStreamingServer[pb.ApplyResponse]) error {
	return fmt.Errorf("simulated recv error")
}

func (s *recvErrServer) Destroy(req *pb.DestroyRequest, stream grpc.ServerStreamingServer[pb.ApplyResponse]) error {
	return fmt.Errorf("simulated recv error")
}

func TestCollectResultEOF(t *testing.T) {
	listener := bufconn.Listen(1024 * 1024)
	srv := grpc.NewServer()
	pb.RegisterAdapterServer(srv, &eofServer{})
	go srv.Serve(listener)
	defer srv.Stop()

	conn, err := grpc.Dial(
		"passthrough:///bufconn",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
			return listener.DialContext(ctx)
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithBlock(),
	)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	stream, err := pb.NewAdapterClient(conn).Apply(context.Background(), &pb.ApplyRequest{ResourceId: "rsc", Type: "test"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = CollectResult("rsc", stream, &recorder{})
	if err == nil || !strings.Contains(err.Error(), "stream ended without result") {
		t.Fatalf("unexpected error: %v", err)
	}
}

type eofServer struct {
	pb.UnimplementedAdapterServer
}

func (s *eofServer) Apply(req *pb.ApplyRequest, stream grpc.ServerStreamingServer[pb.ApplyResponse]) error {
	return nil
}

func (s *eofServer) Destroy(req *pb.DestroyRequest, stream grpc.ServerStreamingServer[pb.ApplyResponse]) error {
	return nil
}
