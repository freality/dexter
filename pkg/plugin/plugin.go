// Package plugin provides the Go SDK for building dex adapters.
//
// An adapter author implements the Adapter interface and calls Serve to start
// the plugin binary. The host side (dex engine) calls Open to connect.
// Neither side ever touches the generated protobuf types or JSON directly.
package plugin

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"time"

	"github.com/hashicorp/go-hclog"
	goplugin "github.com/hashicorp/go-plugin"
	"google.golang.org/grpc"

	"github.com/freality/dexter/pkg/plugin/pb"
)

// Handshake is the shared handshake configuration both Serve and Open use.
// ProtocolVersion must match on both sides; the magic cookie guards against
// accidentally launching a non-plugin binary.
var Handshake = goplugin.HandshakeConfig{
	ProtocolVersion:  1,
	MagicCookieKey:   "DEX_PLUGIN",
	MagicCookieValue: "dex-plugin-v1",
}

// Request carries a resource operation's parameters in Go-native types.
type Request struct {
	// Name is the resource identifier from the IR.
	Name string
	// Type is the adapter resource type (e.g. "docker", "http").
	Type string
	// Properties is the desired state.
	Properties map[string]any
	// Prior is the previous desired state (nil for create).
	Prior map[string]any
	// Outputs is the resource outputs from state (only meaningful for Destroy).
	Outputs map[string]any
}

// Reporter receives intermediate progress from an Adapter operation.
type Reporter interface {
	// Log sends a human-readable log message.
	Log(msg string)
	// Progress reports a percentage estimate (0-100).
	Progress(pct int)
}

// Adapter is the interface a dex adapter binary implements.
type Adapter interface {
	// Apply creates or updates a resource. An empty Prior means create.
	// It returns the new state (outputs) for persistence, or an error.
	Apply(ctx context.Context, req Request, r Reporter) (map[string]any, error)

	// Destroy tears down a resource.
	Destroy(ctx context.Context, req Request, r Reporter) error
}

// These variables are replaced by tests to avoid actual subprocess management.
var (
	pluginServe     = goplugin.Serve
	pluginNewClient = func(cfg *goplugin.ClientConfig) clientInterface { return goplugin.NewClient(cfg) }

	// orphanCheckInterval and osExit are seams for watchOrphan's test.
	orphanCheckInterval = time.Second
	osExit              = os.Exit
	osGetppid           = os.Getppid
)

type clientInterface interface {
	Client() (goplugin.ClientProtocol, error)
	Kill()
}

// Serve starts the plugin process. Call this from func main().
func Serve(a Adapter) {
	stop := make(chan struct{})
	go watchOrphan(osGetppid(), stop)
	defer close(stop)
	pluginServe(&goplugin.ServeConfig{
		HandshakeConfig: Handshake,
		Plugins: goplugin.PluginSet{
			"adapter": &adapterPlugin{impl: a},
		},
		GRPCServer: goplugin.DefaultGRPCServer,
	})
}

// watchOrphan exits the process once its parent has changed from startPPID
// (reparented, e.g. to launchd/init on macOS/Linux), which is what happens
// when the host (dex) dies without a chance to run its own cleanup — a
// crash or SIGKILL, as opposed to a signal it can catch and clean up after.
// go-plugin's own graceful-Kill path handles the orderly-exit case; this is
// the fallback for the disorderly one, since there's no cross-platform
// equivalent of Linux's PR_SET_PDEATHSIG available from Go.
// stop ends the watch (closed by Serve once pluginServe returns, i.e. the
// plugin is shutting down on its own and no longer needs watching).
func watchOrphan(startPPID int, stop <-chan struct{}) {
	for {
		select {
		case <-stop:
			return
		case <-time.After(orphanCheckInterval):
		}
		if osGetppid() != startPPID {
			osExit(1)
			return
		}
	}
}

// Open connects to an adapter binary at path and returns an Adapter client.
// The caller must call the returned kill func when done.
func Open(path string, logOut io.Writer) (Adapter, func(), error) {
	client := pluginNewClient(&goplugin.ClientConfig{
		HandshakeConfig: Handshake,
		Plugins: goplugin.PluginSet{
			"adapter": &adapterPlugin{},
		},
		Cmd:              exec.Command(path),
		AllowedProtocols: []goplugin.Protocol{goplugin.ProtocolGRPC},
		Logger: hclog.New(&hclog.LoggerOptions{
			Name:   "plugin",
			Level:  hclog.Warn,
			Output: logOut,
		}),
	})

	rpcClient, err := client.Client()
	if err != nil {
		client.Kill()
		return nil, nil, fmt.Errorf("connecting to plugin %q: %w", path, err)
	}

	raw, err := rpcClient.Dispense("adapter")
	if err != nil {
		client.Kill()
		return nil, nil, fmt.Errorf("dispensing adapter from %q: %w", path, err)
	}

	return raw.(Adapter), client.Kill, nil
}

// adapterPlugin implements goplugin.GRPCPlugin for the Adapter interface.
type adapterPlugin struct {
	goplugin.NetRPCUnsupportedPlugin
	impl Adapter // set on the server side only
}

func (p *adapterPlugin) GRPCServer(_ *goplugin.GRPCBroker, s *grpc.Server) error {
	pb.RegisterAdapterServer(s, &grpcServer{impl: p.impl})
	return nil
}

func (p *adapterPlugin) GRPCClient(_ context.Context, _ *goplugin.GRPCBroker, conn *grpc.ClientConn) (any, error) {
	return &grpcClient{client: pb.NewAdapterClient(conn)}, nil
}

// grpcServer implements pb.AdapterServer by wrapping an Adapter.
type grpcServer struct {
	pb.UnimplementedAdapterServer
	impl Adapter
}

func (s *grpcServer) Apply(req *pb.ApplyRequest, stream grpc.ServerStreamingServer[pb.ApplyResponse]) error {
	r := Request{
		Name:       req.ResourceId,
		Type:       req.Type,
		Properties: mustUnmarshalMap(req.PropertiesJson),
	}
	if req.PriorPropertiesJson != "" {
		r.Prior = mustUnmarshalMap(req.PriorPropertiesJson)
	}

	rep := &streamReporter{stream: stream}
	result, err := s.impl.Apply(stream.Context(), r, rep)
	if err != nil {
		stream.Send(&pb.ApplyResponse{Type: pb.ApplyResponse_ERROR, Message: err.Error()})
		return nil
	}

	json := ""
	if len(result) > 0 {
		json = mustMarshal(result)
	}
	stream.Send(&pb.ApplyResponse{Type: pb.ApplyResponse_SUCCESS, NewStateJson: json})
	return nil
}

func (s *grpcServer) Destroy(req *pb.DestroyRequest, stream grpc.ServerStreamingServer[pb.ApplyResponse]) error {
	r := Request{
		Name:       req.ResourceId,
		Type:       req.Type,
		Properties: mustUnmarshalMap(req.PropertiesJson),
	}
	if req.OutputsJson != "" {
		r.Outputs = mustUnmarshalMap(req.OutputsJson)
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

// streamReporter sends LOG and PROGRESS messages through the gRPC stream.
type streamReporter struct {
	stream grpc.ServerStreamingServer[pb.ApplyResponse]
}

func (r *streamReporter) Log(msg string) {
	r.stream.Send(&pb.ApplyResponse{Type: pb.ApplyResponse_LOG, Message: msg})
}

func (r *streamReporter) Progress(pct int) {
	r.stream.Send(&pb.ApplyResponse{Type: pb.ApplyResponse_PROGRESS, Message: strconv.Itoa(pct)})
}

// grpcClient implements Adapter by calling through gRPC.
type grpcClient struct {
	client pb.AdapterClient
}

func (c *grpcClient) Apply(ctx context.Context, req Request, rep Reporter) (map[string]any, error) {
	stream, err := c.client.Apply(ctx, &pb.ApplyRequest{
		ResourceId:          req.Name,
		Type:                req.Type,
		PropertiesJson:      mustMarshal(req.Properties),
		PriorPropertiesJson: mustMarshal(req.Prior),
	})
	if err != nil {
		return nil, fmt.Errorf("plugin %q: %w", req.Name, err)
	}

	return CollectResult(req.Name, stream, rep)
}

func (c *grpcClient) Destroy(ctx context.Context, req Request, rep Reporter) error {
	stream, err := c.client.Destroy(ctx, &pb.DestroyRequest{
		ResourceId:     req.Name,
		Type:           req.Type,
		PropertiesJson: mustMarshal(req.Properties),
		OutputsJson:    mustMarshal(req.Outputs),
	})
	if err != nil {
		return fmt.Errorf("plugin %q: %w", req.Name, err)
	}

	_, err = CollectResult(req.Name, stream, rep)
	return err
}

// CollectResult reads a server-streaming response, dispatching LOG/PROGRESS
// to the Reporter and returning the terminal SUCCESS/ERROR result.
// It is exported so adapter authors can use it in their own test helpers.
func CollectResult(name string, stream grpc.ServerStreamingClient[pb.ApplyResponse], rep Reporter) (map[string]any, error) {
	var result map[string]any
	var terminalErr error
	haveTerminal := false

	for {
		resp, err := stream.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("collect %q: recv: %w", name, err)
		}

		switch resp.Type {
		case pb.ApplyResponse_LOG:
			if rep != nil {
				rep.Log(resp.Message)
			}
		case pb.ApplyResponse_PROGRESS:
			if rep != nil {
				pct, _ := strconv.Atoi(resp.Message)
				rep.Progress(pct)
			}
		case pb.ApplyResponse_SUCCESS:
			haveTerminal = true
			if resp.NewStateJson != "" {
				result = mustUnmarshalMap(resp.NewStateJson)
			}
		case pb.ApplyResponse_ERROR:
			haveTerminal = true
			terminalErr = fmt.Errorf("plugin %q: %s", name, resp.Message)
		}
	}

	if !haveTerminal {
		return nil, fmt.Errorf("plugin %q: stream ended without result", name)
	}

	return result, terminalErr
}

// ponytail: json.Unmarshal into map[string]any, upgrade to protojson or
// structpb if schema-driven deserialization ever matters.
func mustUnmarshalMap(s string) map[string]any {
	if s == "" {
		return nil
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(s), &m); err != nil {
		// This indicates a bug in the adapter or serialization layer,
		// not a user error — panic is appropriate.
		panic(fmt.Sprintf("plugin: failed to unmarshal JSON map: %v", err))
	}
	return m
}

func mustMarshal(v any) string {
	if v == nil {
		return ""
	}
	b, err := json.Marshal(v)
	if err != nil {
		panic(fmt.Sprintf("plugin: failed to marshal JSON: %v", err))
	}
	return string(b)
}
