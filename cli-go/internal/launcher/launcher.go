package launcher

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"strconv"
	"time"

	"github.com/sofarpc/cli-go/internal/ipc"
	"github.com/sofarpc/cli-go/internal/protocol"
)

// Default budgets. `RequestBudget` is for normal calls against an already-running daemon;
// `SpawnBudget` is much longer because it has to absorb JVM cold start.
const (
	DefaultDialTimeout    = 1 * time.Second
	DefaultRequestTimeout = 30 * time.Second
	DefaultSpawnBudget    = 30 * time.Second
	DefaultPollInterval   = 100 * time.Millisecond
	loopbackHost          = "127.0.0.1"
	defaultPickPort       = 0
)

// Config drives Connect. Most fields have sensible defaults via DefaultConfig.
type Config struct {
	BuildVersion   string
	IdleTTLMS      int64
	JarPath        string
	JavaBin        string
	JVMArgs        []string
	Paths          Paths
	DialTimeout    time.Duration
	RequestTimeout time.Duration
	SpawnBudget    time.Duration
	PollInterval   time.Duration
	NoSpawn        bool
}

// DefaultConfig returns a Config wired to the default ~/.sofarpc/daemon/ paths.
func DefaultConfig(buildVersion string) (Config, error) {
	paths, err := DefaultPaths()
	if err != nil {
		return Config{}, err
	}
	return Config{
		BuildVersion:   buildVersion,
		IdleTTLMS:      int64((15 * time.Minute) / time.Millisecond),
		JavaBin:        ResolveJavaBin(),
		Paths:          paths,
		DialTimeout:    DefaultDialTimeout,
		RequestTimeout: DefaultRequestTimeout,
		SpawnBudget:    DefaultSpawnBudget,
		PollInterval:   DefaultPollInterval,
	}, nil
}

// Connection is what callers receive: a ready ipc.Client targeting the warm daemon.
type Connection struct {
	Client *ipc.Client
	State  State
	Health protocol.HealthData
}

// Connect resolves a usable daemon, possibly spawning it. The control flow is exactly the
// one specified in docs/agent-first-architecture-design.md §9.3:
//
//  1. read state.json
//  2. if pid alive AND tcp dial works AND health OK AND buildVersion matches -> reuse
//  3. otherwise: clean stale state, take daemon.lock
//     - if we got the lock -> spawn, then wait
//     - if we did not get the lock -> wait
//  4. final: probe state.json + health within SpawnBudget
func Connect(cfg Config) (*Connection, error) {
	if err := cfg.Paths.EnsureBaseDir(); err != nil {
		return nil, err
	}
	conn, err := tryReuse(cfg)
	if err != nil {
		return nil, err
	}
	if conn != nil {
		return conn, nil
	}
	if cfg.NoSpawn {
		return nil, errors.New("daemon not available and --no-spawn was set")
	}

	lock, gotLock, err := TryLock(cfg.Paths.LockFile)
	if err != nil {
		return nil, err
	}
	if gotLock {
		defer lock.Release()
		conn, err := tryReuse(cfg)
		if err != nil {
			return nil, err
		}
		if conn != nil {
			return conn, nil
		}
		if err := spawnDaemon(cfg); err != nil {
			return nil, err
		}
	}
	return waitForReady(cfg)
}

// tryReuse implements the "fast path": a single attempt at reading state.json and
// connecting. Returns (nil, nil) when the daemon is simply not usable yet (missing state,
// dead pid, unreachable, stale version) so the caller can decide to spawn/wait. Returns
// a non-nil error only when something is wrong that the caller cannot paper over by
// spawning — e.g. an old daemon pid stayed alive past the restart deadline, so deleting
// state.json would orphan it.
func tryReuse(cfg Config) (*Connection, error) {
	state, err := ReadState(cfg.Paths.StateFile)
	if err != nil || state == nil {
		return nil, nil
	}
	if !IsPIDAlive(state.PID) {
		_ = DeleteState(cfg.Paths.StateFile)
		return nil, nil
	}
	client := buildClient(cfg, state.Port)
	health, err := probeHealth(client)
	if err != nil {
		return nil, nil
	}
	if cfg.BuildVersion != "" && health.BuildVersion != cfg.BuildVersion {
		if err := restartForVersionMismatch(cfg, state); err != nil {
			return nil, err
		}
		return nil, nil
	}
	return &Connection{Client: client, State: *state, Health: *health}, nil
}

func waitForReady(cfg Config) (*Connection, error) {
	deadline := time.Now().Add(cfg.SpawnBudget)
	var lastErr error
	for time.Now().Before(deadline) {
		conn, err := tryReuse(cfg)
		if err != nil {
			return nil, err
		}
		if conn != nil {
			return conn, nil
		}
		lastErr = errors.New("daemon not ready yet")
		time.Sleep(cfg.PollInterval)
	}
	if lastErr == nil {
		lastErr = errors.New("daemon failed to become ready within spawn budget")
	}
	return nil, fmt.Errorf("waiting for daemon: %w", lastErr)
}

func spawnDaemon(cfg Config) error {
	jar, err := ResolveJarPath(cfg.JarPath)
	if err != nil {
		return err
	}
	_, err = Spawn(SpawnConfig{
		JavaBin:   cfg.JavaBin,
		JarPath:   jar,
		Port:      defaultPickPort,
		IdleTTLMS: cfg.IdleTTLMS,
		StateFile: cfg.Paths.StateFile,
		LogFile:   cfg.Paths.LogFile,
		JVMArgs:   cfg.JVMArgs,
		BuildVer:  cfg.BuildVersion,
	})
	return err
}

// restartForVersionMismatch asks the running daemon to shut down and waits for its pid
// to actually disappear before deleting state.json. If the pid stays alive past the
// deadline we refuse to delete state — otherwise the next spawn would write a fresh
// state.json over a still-listening orphan, leaving two daemons fighting for the port.
func restartForVersionMismatch(cfg Config, state *State) error {
	stop := buildClient(cfg, state.Port)
	_ = sendShutdown(stop)
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) && IsPIDAlive(state.PID) {
		time.Sleep(50 * time.Millisecond)
	}
	if IsPIDAlive(state.PID) {
		return fmt.Errorf("old daemon pid %d did not exit within 5s after shutdown; refusing to abandon state.json", state.PID)
	}
	_ = DeleteState(cfg.Paths.StateFile)
	return nil
}

func buildClient(cfg Config, port int) *ipc.Client {
	return &ipc.Client{
		Addr:           net.JoinHostPort(loopbackHost, strconv.Itoa(port)),
		DialTimeout:    cfg.DialTimeout,
		RequestTimeout: cfg.RequestTimeout,
	}
}

func probeHealth(client *ipc.Client) (*protocol.HealthData, error) {
	req, err := protocol.NewRequest(protocol.OpHealth, struct{}{})
	if err != nil {
		return nil, err
	}
	resp, err := client.Call(req)
	if err != nil {
		return nil, err
	}
	if !resp.OK {
		return nil, fmt.Errorf("health failed: %s", resp.Code)
	}
	var h protocol.HealthData
	if err := json.Unmarshal(resp.Data, &h); err != nil {
		return nil, fmt.Errorf("decode health: %w", err)
	}
	return &h, nil
}

func sendShutdown(client *ipc.Client) error {
	req, err := protocol.NewRequest(protocol.OpShutdown, protocol.ShutdownPayload{GraceMS: 0})
	if err != nil {
		return err
	}
	_, err = client.Call(req)
	return err
}
