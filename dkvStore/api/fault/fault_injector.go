package fault

import (
	"context"
	"math/rand/v2"
	"sync"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type Config struct {
	LatencyMs   int
	DropRate    float64
	Partitioned bool
}

type Injector struct {
	mu        sync.RWMutex
	global    Config
	perTarget map[string]Config // outbound gRPC dial targets only (see UnaryClientInterceptor)
}

func NewInjector() *Injector {
	return &Injector{perTarget: make(map[string]Config)}
}

// Set updates fault behavior for all RPCs (incoming server + outgoing client defaults).
func (f *Injector) Set(cfg Config) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.global = cfg
}

// SetForTarget sets outbound client faults only for RPCs to the given gRPC dial target
// (must match the address passed to grpc.NewClient and grpc.ClientConn.Target()).
// Empty target is treated as Set(cfg).
func (f *Injector) SetForTarget(target string, cfg Config) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if target == "" {
		f.global = cfg
		return
	}
	if f.perTarget == nil {
		f.perTarget = make(map[string]Config)
	}
	f.perTarget[target] = cfg
}

// ClearTarget removes a per-target outbound override. Empty target clears global + all targets.
func (f *Injector) ClearTarget(target string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if target == "" {
		f.global = Config{}
		f.perTarget = make(map[string]Config)
		return
	}
	delete(f.perTarget, target)
}

func (f *Injector) Reset() {
	f.ClearTarget("")
}

func (f *Injector) Get() Config {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return f.global
}

// Snapshot is returned by GET /fault for debugging.
type Snapshot struct {
	Global  Config            `json:"global"`
	Targets map[string]Config `json:"targets,omitempty"`
}

func (f *Injector) Snapshot() Snapshot {
	f.mu.RLock()
	defer f.mu.RUnlock()
	cp := make(map[string]Config, len(f.perTarget))
	for k, v := range f.perTarget {
		cp[k] = v
	}
	return Snapshot{Global: f.global, Targets: cp}
}

func (f *Injector) configForClient(target string) Config {
	f.mu.RLock()
	defer f.mu.RUnlock()
	if c, ok := f.perTarget[target]; ok {
		return c
	}
	return f.global
}

func (f *Injector) UnaryClientInterceptor() grpc.UnaryClientInterceptor {
	return func(
		ctx context.Context,
		method string,
		req, reply any,
		cc *grpc.ClientConn,
		invoker grpc.UnaryInvoker,
		opts ...grpc.CallOption,
	) error {
		cfg := f.configForClient(cc.Target())

		if cfg.Partitioned {
			return status.Errorf(codes.Unavailable, "fault: node partitioned")
		}
		if cfg.DropRate > 0 && rand.Float64() < cfg.DropRate {
			return status.Errorf(codes.DeadlineExceeded, "fault: packet dropped")
		}
		if cfg.LatencyMs > 0 {
			select {
			case <-time.After(time.Duration(cfg.LatencyMs) * time.Millisecond):
			case <-ctx.Done():
				return status.FromContextError(ctx.Err()).Err()
			}
		}

		return invoker(ctx, method, req, reply, cc, opts...)
	}
}

// UnaryServerInterceptor injects faults on incoming RPCs.
func (f *Injector) UnaryServerInterceptor() grpc.UnaryServerInterceptor {
	return func(
		ctx context.Context,
		req any,
		info *grpc.UnaryServerInfo,
		handler grpc.UnaryHandler,
	) (any, error) {
		cfg := f.Get()

		if cfg.Partitioned {
			return nil, status.Errorf(codes.Unavailable, "fault: node partitioned")
		}
		if cfg.DropRate > 0 && rand.Float64() < cfg.DropRate {
			return nil, status.Errorf(codes.DeadlineExceeded, "fault: packet dropped")
		}
		if cfg.LatencyMs > 0 {
			select {
			case <-time.After(time.Duration(cfg.LatencyMs) * time.Millisecond):
			case <-ctx.Done():
				return nil, status.FromContextError(ctx.Err()).Err()
			}
		}

		return handler(ctx, req)
	}
}
