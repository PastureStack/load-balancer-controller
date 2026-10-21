package haproxy

import (
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/PastureStack/load-balancer-controller/config"
)

type drainRuntime struct {
	states         map[string]serverState
	commands       []string
	fail           map[string]int
	reject         map[string]string
	ignoreWrites   bool
	sessions       int
	initialWeights map[string]int
}

func (r *drainRuntime) command(cmd string) ([]string, error) {
	r.commands = append(r.commands, cmd)
	if r.fail[cmd] > 0 {
		r.fail[cmd]--
		return nil, errors.New("socket timed out")
	}
	if response, ok := r.reject[cmd]; ok {
		return []string{response}, nil
	}
	switch cmd {
	case "show stat":
		rows := []string{"# pxname,svname,scur"}
		for _, state := range r.states {
			rows = append(rows, fmt.Sprintf("%s,%s,%d", strings.Split(state.backend, "/")[0], state.name, r.sessions))
		}
		return rows, nil
	case "show servers state":
		rows := []string{"1", "# be_name srv_name srv_uweight srv_iweight srv_admin_state srv_op_state"}
		var keys []string
		for key := range r.states {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			state := r.states[key]
			initial, ok := r.initialWeights[key]
			if !ok {
				initial = 1
			}
			rows = append(rows, fmt.Sprintf("%s %s %d %d %d %d", strings.Split(key, "/")[0], state.name, state.weight, initial, state.admin, state.operational))
		}
		return rows, nil
	}
	fields := strings.Fields(cmd)
	if len(fields) == 5 && fields[0] == "set" && fields[1] == "server" && fields[3] == "weight" {
		state, ok := r.states[fields[2]]
		if !ok {
			return []string{"No such server"}, nil
		}
		weight, err := strconv.Atoi(fields[4])
		if err != nil {
			return nil, err
		}
		if !r.ignoreWrites {
			state.weight = weight
			r.states[fields[2]] = state
		}
		return nil, nil
	}
	return nil, fmt.Errorf("unexpected command %q", cmd)
}

func recoveryFixture(t *testing.T) (*Provider, *drainRuntime, *config.Endpoint, *config.LoadBalancerConfig) {
	t.Helper()
	r := &drainRuntime{states: map[string]serverState{
		"be-one/target": {backend: "be-one/target", name: "target", weight: 7, operational: 2},
		"be-two/target": {backend: "be-two/target", name: "target", weight: 23, operational: 2},
	}, fail: make(map[string]int), reject: make(map[string]string), initialWeights: make(map[string]int)}
	dir := t.TempDir()
	pid := filepath.Join(dir, "haproxy.pid")
	if err := os.WriteFile(pid, []byte("123\n"), 0600); err != nil {
		t.Fatal(err)
	}
	p := &Provider{cfg: &haproxyConfig{PidFile: pid, CertDir: filepath.Join(dir, "certs"), Config: filepath.Join(dir, "haproxy.cfg"), Template: "test_data/haproxy_template.cfg", reloadCommand: func() error { return nil }}, drainMgr: &drainMgr{drainList: make(map[string]string), mu: &sync.RWMutex{}, command: r.command, olderProcesses: func() (bool, error) { return false, nil }}}
	ep := &config.Endpoint{Name: "target", IP: "10.42.0.2", Port: 80, Weight: 1, Config: "check"}
	cfg := &config.LoadBalancerConfig{FrontendServices: config.FrontendServices{&config.FrontendService{Protocol: config.HTTPProto, BackendServices: config.BackendServices{&config.BackendService{UUID: "be-one", Endpoints: config.Endpoints{ep}}, &config.BackendService{UUID: "be-two", Endpoints: config.Endpoints{ep}}}}}}
	p.drainMgr.setDesired(cfg)
	return p, r, ep, cfg
}

func runtimeWeights(r *drainRuntime) map[string]int {
	weights := make(map[string]int)
	for key, state := range r.states {
		weights[key] = state.weight
	}
	return weights
}

func TestDrainWithoutSessionsStillRecoversBothBackendWeights(t *testing.T) {
	p, r, ep, _ := recoveryFixture(t)
	if p.DrainEndpoint(ep) {
		t.Fatal("zero sessions must not delay the event reply")
	}
	if !reflect.DeepEqual(runtimeWeights(r), map[string]int{"be-one/target": 0, "be-two/target": 0}) {
		t.Fatal(runtimeWeights(r))
	}
	if err := p.reconcileDrains(); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(runtimeWeights(r), map[string]int{"be-one/target": 7, "be-two/target": 23}) {
		t.Fatal(runtimeWeights(r))
	}
	if len(p.drainMgr.weights) != 0 {
		t.Fatal("verified recovery must clear ownership")
	}
}

func TestRepeatedDrainAndTimeoutPreserveOriginalWeights(t *testing.T) {
	p, r, ep, _ := recoveryFixture(t)
	r.sessions = 2
	if !p.DrainEndpoint(ep) || !p.DrainEndpoint(ep) {
		t.Fatal("active duplicate drain not retained")
	}
	if err := p.reconcileDrains(); err != nil {
		t.Fatal(err)
	}
	if r.states["be-one/target"].weight != 0 {
		t.Fatal("active drain recovered early")
	}
	p.RemoveEndpointFromDrain(ep)
	p.RemoveEndpointFromDrain(ep)
	if err := p.reconcileDrains(); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(runtimeWeights(r), map[string]int{"be-one/target": 7, "be-two/target": 23}) {
		t.Fatal(runtimeWeights(r))
	}
}

func TestDrainCompletionWhileStoppingRetainsOwnershipUntilSameTargetRuns(t *testing.T) {
	p, r, ep, cfg := recoveryFixture(t)
	r.sessions = 1
	if !p.DrainEndpoint(ep) {
		t.Fatal("expected active drain")
	}
	// Metadata observes stopping before the drain-complete reply. These are
	// separate ordered events, not a synthetic final-state fixture.
	ep.Stopping = true
	ep.Config = "check weight 0"
	p.drainMgr.setDesired(cfg)
	p.RemoveEndpointFromDrain(ep)
	for i := 0; i < 2; i++ {
		if err := p.reconcileDrains(); err != nil {
			t.Fatal(err)
		}
		if len(p.drainMgr.weights[ep.Name]) != 2 {
			t.Fatal("stopping sync discarded original weights")
		}
		if !reflect.DeepEqual(runtimeWeights(r), map[string]int{"be-one/target": 0, "be-two/target": 0}) {
			t.Fatal("stopping endpoint recovered early")
		}
	}
	// The same target comes back without a process/config reload to rescue it.
	ep.Stopping = false
	ep.Config = "check"
	p.drainMgr.setDesired(cfg)
	if err := p.reconcileDrains(); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(runtimeWeights(r), map[string]int{"be-one/target": 7, "be-two/target": 23}) {
		t.Fatal(runtimeWeights(r))
	}
}

func TestActiveDrainReloadDoesNotOverwriteNewProcessManualWeight(t *testing.T) {
	p, r, ep, _ := recoveryFixture(t)
	r.sessions = 1
	if !p.DrainEndpoint(ep) {
		t.Fatal("expected active drain")
	}
	if err := os.WriteFile(p.cfg.PidFile, []byte("456\n"), 0600); err != nil {
		t.Fatal(err)
	}
	// Reload initializes both at configured weight 1. An operator then changes
	// only one backend before the controller observes this new process.
	for key, state := range r.states {
		state.weight = 1
		r.states[key] = state
	}
	state := r.states["be-one/target"]
	state.weight = 31
	r.states[state.backend] = state
	if err := p.reconcileDrains(); err != nil {
		t.Fatal(err)
	}
	if r.states["be-one/target"].weight != 31 {
		t.Fatal("reload reconciliation overwrote manual weight")
	}
	if r.states["be-two/target"].weight != 0 {
		t.Fatal("reload default did not resume active drain")
	}
	p.RemoveEndpointFromDrain(ep)
	if err := p.reconcileDrains(); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(runtimeWeights(r), map[string]int{"be-one/target": 31, "be-two/target": 23}) {
		t.Fatal(runtimeWeights(r))
	}
}

func TestCompletedDrainStoppingReloadRetainsOriginalWeightsUntilRunning(t *testing.T) {
	p, r, ep, cfg := recoveryFixture(t)
	r.sessions = 1
	p.DrainEndpoint(ep)
	p.RemoveEndpointFromDrain(ep) // Completion can precede stopping metadata.
	ep.Stopping = true
	ep.Config = "check weight 0"
	p.drainMgr.setDesired(cfg)
	if err := os.WriteFile(p.cfg.PidFile, []byte("456\n"), 0600); err != nil {
		t.Fatal(err)
	}
	for key := range r.states {
		r.initialWeights[key] = 0
	} // Reload of the controller's stopping config.
	if err := p.reconcileDrains(); err != nil {
		t.Fatal(err)
	}
	for _, original := range p.drainMgr.weights[ep.Name] {
		if original.pid != "456" {
			t.Fatal("proven stopping config generation was not retained")
		}
	}
	ep.Stopping = false
	ep.Config = "check"
	p.drainMgr.setDesired(cfg)
	// No subsequent reload: the runtime is still zero in the same PID.
	if err := p.reconcileDrains(); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(runtimeWeights(r), map[string]int{"be-one/target": 7, "be-two/target": 23}) {
		t.Fatal(runtimeWeights(r))
	}
}

func completedStoppingReloadFixture(t *testing.T, completeFirst bool) (*Provider, *drainRuntime, *config.Endpoint, *config.LoadBalancerConfig) {
	t.Helper()
	p, r, ep, cfg := recoveryFixture(t)
	r.sessions = 1
	p.DrainEndpoint(ep)
	if completeFirst {
		p.RemoveEndpointFromDrain(ep)
	}
	ep.Stopping = true
	ep.Config = "check weight 0"
	p.drainMgr.setDesired(cfg)
	if err := os.WriteFile(p.cfg.PidFile, []byte("456\n"), 0600); err != nil {
		t.Fatal(err)
	}
	for key := range r.states {
		r.initialWeights[key] = 0
	}
	if err := p.reconcileDrains(); err != nil {
		t.Fatal(err)
	}
	if len(p.drainMgr.weights[ep.Name]) != 2 {
		t.Fatal("completed drain lost owned weights during stopping reload")
	}
	if !completeFirst {
		p.RemoveEndpointFromDrain(ep)
	}
	return p, r, ep, cfg
}

func runningReload(t *testing.T, p *Provider, r *drainRuntime, ep *config.Endpoint, cfg *config.LoadBalancerConfig) {
	t.Helper()
	ep.Stopping = false
	ep.Config = "check"
	p.drainMgr.setDesired(cfg)
	if err := os.WriteFile(p.cfg.PidFile, []byte("789\n"), 0600); err != nil {
		t.Fatal(err)
	}
	for key, state := range r.states {
		state.weight = 1
		r.states[key] = state
		r.initialWeights[key] = 1
	}
}

func TestCompletedDrainRestoresOwnedWeightsAfterRunningReload(t *testing.T) {
	for _, completeFirst := range []bool{true, false} {
		t.Run(fmt.Sprintf("completeBeforeStopping=%t", completeFirst), func(t *testing.T) {
			p, r, ep, cfg := completedStoppingReloadFixture(t, completeFirst)
			runningReload(t, p, r, ep, cfg)
			if err := p.reconcileDrains(); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(runtimeWeights(r), map[string]int{"be-one/target": 7, "be-two/target": 23}) {
				t.Fatal("running reload discarded controller-owned original weights:", runtimeWeights(r))
			}
			if len(p.drainMgr.weights) != 0 {
				t.Fatal("verified restoration must clear ownership")
			}
		})
	}
}

func TestCompletedRunningReloadPreservesManualAndDesiredAuthority(t *testing.T) {
	for _, test := range []struct {
		name             string
		alter            func(*testing.T, *Provider, *drainRuntime, *config.Endpoint, *config.LoadBalancerConfig)
		wantOne, wantTwo int
	}{
		{"manual zero", func(_ *testing.T, _ *Provider, r *drainRuntime, _ *config.Endpoint, _ *config.LoadBalancerConfig) {
			s := r.states["be-one/target"]
			s.weight = 0
			r.states["be-one/target"] = s
		}, 0, 23},
		{"manual MAINT", func(_ *testing.T, _ *Provider, r *drainRuntime, _ *config.Endpoint, _ *config.LoadBalancerConfig) {
			s := r.states["be-one/target"]
			s.admin = 1
			r.states["be-one/target"] = s
		}, 1, 23},
		{"manual DRAIN", func(_ *testing.T, _ *Provider, r *drainRuntime, _ *config.Endpoint, _ *config.LoadBalancerConfig) {
			s := r.states["be-one/target"]
			s.admin = 8
			r.states["be-one/target"] = s
		}, 1, 23},
		{"manual custom weight", func(_ *testing.T, _ *Provider, r *drainRuntime, _ *config.Endpoint, _ *config.LoadBalancerConfig) {
			s := r.states["be-one/target"]
			s.weight = 31
			r.states["be-one/target"] = s
		}, 31, 23},
		{"desired zero", func(_ *testing.T, p *Provider, r *drainRuntime, ep *config.Endpoint, cfg *config.LoadBalancerConfig) {
			ep.Config = "check weight 0"
			p.drainMgr.setDesired(cfg)
			for key, s := range r.states {
				s.weight = 0
				r.states[key] = s
				r.initialWeights[key] = 0
			}
		}, 0, 0},
		{"desired disabled", func(_ *testing.T, p *Provider, _ *drainRuntime, ep *config.Endpoint, cfg *config.LoadBalancerConfig) {
			ep.Config = "check disabled"
			p.drainMgr.setDesired(cfg)
		}, 1, 1},
		{"desired changed", func(_ *testing.T, p *Provider, r *drainRuntime, ep *config.Endpoint, cfg *config.LoadBalancerConfig) {
			ep.Config = "check weight 37"
			p.drainMgr.setDesired(cfg)
			for key, s := range r.states {
				s.weight = 37
				r.states[key] = s
				r.initialWeights[key] = 37
			}
		}, 37, 37},
		{"unapplied desired generation", func(_ *testing.T, _ *Provider, r *drainRuntime, _ *config.Endpoint, _ *config.LoadBalancerConfig) {
			s := r.states["be-one/target"]
			s.weight = 2
			r.states["be-one/target"] = s
			r.initialWeights["be-one/target"] = 2
		}, 2, 23},
		{"same process manual nonzero", func(t *testing.T, p *Provider, r *drainRuntime, _ *config.Endpoint, _ *config.LoadBalancerConfig) {
			if err := os.WriteFile(p.cfg.PidFile, []byte("456\n"), 0600); err != nil {
				t.Fatal(err)
			}
			// No running reload: an operator raised the stopping process's
			// zero to 1. Even a desired default is not initialization proof.
			for key := range r.states {
				r.initialWeights[key] = 0
			}
		}, 1, 1},
		{"unknown generation", func(_ *testing.T, p *Provider, _ *drainRuntime, ep *config.Endpoint, _ *config.LoadBalancerConfig) {
			for key, owned := range p.drainMgr.weights[ep.Name] {
				owned.stoppingReload = false
				p.drainMgr.weights[ep.Name][key] = owned
			}
		}, 1, 1},
		{"no ownership", func(_ *testing.T, p *Provider, _ *drainRuntime, ep *config.Endpoint, _ *config.LoadBalancerConfig) {
			delete(p.drainMgr.weights, ep.Name)
		}, 1, 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			p, r, ep, cfg := completedStoppingReloadFixture(t, true)
			runningReload(t, p, r, ep, cfg)
			test.alter(t, p, r, ep, cfg)
			before := r.states["be-one/target"]
			if err := p.reconcileDrains(); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(runtimeWeights(r), map[string]int{"be-one/target": test.wantOne, "be-two/target": test.wantTwo}) {
				t.Fatal(runtimeWeights(r))
			}
			if r.states["be-one/target"].admin != before.admin {
				t.Fatal("manual admin state changed")
			}
			if len(p.drainMgr.weights) != 0 {
				t.Fatal("completed restoration/control retained stale ownership")
			}
		})
	}
}

func TestCompletedRunningReloadWaitsForHealth(t *testing.T) {
	for _, block := range []string{"health", "DNS"} {
		t.Run(block, func(t *testing.T) {
			p, r, ep, cfg := completedStoppingReloadFixture(t, true)
			runningReload(t, p, r, ep, cfg)
			for key, state := range r.states {
				if block == "health" {
					state.operational = 0
				} else {
					state.admin = 0x20
				}
				r.states[key] = state
			}
			if err := p.reconcileDrains(); err != nil {
				t.Fatal(err)
			}
			if len(p.drainMgr.weights[ep.Name]) != 2 || !reflect.DeepEqual(runtimeWeights(r), map[string]int{"be-one/target": 1, "be-two/target": 1}) {
				t.Fatal("unready replacement recovered or lost ownership")
			}
			for key, state := range r.states {
				state.admin = 0
				state.operational = 2
				r.states[key] = state
			}
			if err := p.reconcileDrains(); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(runtimeWeights(r), map[string]int{"be-one/target": 7, "be-two/target": 23}) {
				t.Fatal(runtimeWeights(r))
			}
		})
	}
}

func TestCompletedRunningReloadRetainsOwnershipUntilVerified(t *testing.T) {
	for _, failure := range []string{"command", "readback", "ignored write"} {
		t.Run(failure, func(t *testing.T) {
			p, r, ep, cfg := completedStoppingReloadFixture(t, true)
			runningReload(t, p, r, ep, cfg)
			switch failure {
			case "command":
				r.fail["set server be-one/target weight 7"] = 1
			case "ignored write":
				r.ignoreWrites = true
			case "readback":
				p.drainMgr.command = func(cmd string) ([]string, error) {
					out, err := r.command(cmd)
					if cmd == "set server be-one/target weight 7" && err == nil {
						r.fail["show servers state"] = 1
					}
					return out, err
				}
			}
			if err := p.reconcileDrains(); err == nil {
				t.Fatal("missing restoration failure")
			}
			if len(p.drainMgr.weights[ep.Name]) == 0 {
				t.Fatal("unverified restore discarded ownership")
			}
			r.ignoreWrites = false
			if err := p.reconcileDrains(); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(runtimeWeights(r), map[string]int{"be-one/target": 7, "be-two/target": 23}) {
				t.Fatal(runtimeWeights(r))
			}
			if len(p.drainMgr.weights) != 0 {
				t.Fatal("verified restore retained stale ownership")
			}
		})
	}
}

func TestActiveDrainReloadDoesNotAdoptNewProcessManualZero(t *testing.T) {
	p, r, ep, _ := recoveryFixture(t)
	r.sessions = 1
	p.DrainEndpoint(ep)
	if err := os.WriteFile(p.cfg.PidFile, []byte("456\n"), 0600); err != nil {
		t.Fatal(err)
	}
	// The replacement has configured initial weight 1, but its current zero is
	// not from any command issued by this controller in the new generation.
	if err := p.reconcileDrains(); err != nil {
		t.Fatal(err)
	}
	p.RemoveEndpointFromDrain(ep)
	if err := p.reconcileDrains(); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(runtimeWeights(r), map[string]int{"be-one/target": 0, "be-two/target": 0}) {
		t.Fatal("adopted unowned replacement-process zeros")
	}
}

func TestActiveDrainReloadHonorsChangedDesiredCustomWeight(t *testing.T) {
	p, r, ep, cfg := recoveryFixture(t)
	r.sessions = 1
	p.DrainEndpoint(ep)
	// A desired-config edit is a different source of authority from the
	// runtime weight captured before draining. Do not undo the user's edit.
	ep.Config = "check weight 37"
	p.drainMgr.setDesired(cfg)
	if err := os.WriteFile(p.cfg.PidFile, []byte("456\n"), 0600); err != nil {
		t.Fatal(err)
	}
	for key, state := range r.states {
		state.weight = 37
		r.states[key] = state
		r.initialWeights[key] = 37
	}
	if err := p.reconcileDrains(); err != nil {
		t.Fatal(err)
	}
	p.RemoveEndpointFromDrain(ep)
	if err := p.reconcileDrains(); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(runtimeWeights(r), map[string]int{"be-one/target": 37, "be-two/target": 37}) {
		t.Fatal("recovery undid the desired custom weight:", runtimeWeights(r))
	}
}

func TestSameEndpointAndUnchangedConfigRecoverWithoutReload(t *testing.T) {
	p, r, ep, cfg := recoveryFixture(t)
	var previous string
	reloads := 0
	p.cfg.reloadCommand = func() error {
		body, err := os.ReadFile(p.cfg.Config)
		if err != nil {
			return err
		}
		if string(body) != previous {
			reloads++
			previous = string(body)
		}
		return nil
	}
	if err := p.applyHaproxyConfig(cfg); err != nil {
		t.Fatal(err)
	}
	p.DrainEndpoint(ep)
	if err := p.applyHaproxyConfig(cfg); err != nil {
		t.Fatal(err)
	}
	if reloads != 1 {
		t.Fatalf("unchanged config caused %d reloads", reloads)
	}
	if !reflect.DeepEqual(runtimeWeights(r), map[string]int{"be-one/target": 7, "be-two/target": 23}) {
		t.Fatal(runtimeWeights(r))
	}
}

func TestDrainRecoveryWaitsForHealthAndRetriesSocketFailure(t *testing.T) {
	p, r, ep, _ := recoveryFixture(t)
	p.DrainEndpoint(ep)
	state := r.states["be-one/target"]
	state.operational = 0
	r.states[state.backend] = state
	r.fail["set server be-two/target weight 23"] = 1
	if err := p.reconcileDrains(); err == nil {
		t.Fatal("missing restore failure")
	}
	if len(p.drainMgr.weights[ep.Name]) != 2 {
		t.Fatal("failure lost recovery provenance")
	}
	state.operational = 2
	r.states[state.backend] = state
	if err := p.reconcileDrains(); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(runtimeWeights(r), map[string]int{"be-one/target": 7, "be-two/target": 23}) {
		t.Fatal(runtimeWeights(r))
	}
}

func TestDrainCommandFailureAndRejectedReplyKeepRecoveryOwnership(t *testing.T) {
	for _, failure := range []string{"timeout", "rejected"} {
		t.Run(failure, func(t *testing.T) {
			p, r, ep, _ := recoveryFixture(t)
			if failure == "timeout" {
				r.fail["set server be-one/target weight 0"] = 1
			} else {
				r.reject["set server be-one/target weight 0"] = "Permission denied"
			}
			if p.DrainEndpoint(ep) {
				t.Fatal("partial drain reported success")
			}
			if len(p.drainMgr.weights[ep.Name]) != 2 {
				t.Fatal("partial failure lost ownership")
			}
			if err := p.reconcileDrains(); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(runtimeWeights(r), map[string]int{"be-one/target": 7, "be-two/target": 23}) {
				t.Fatal(runtimeWeights(r))
			}
		})
	}
}

func TestDrainRecoveryRequiresVerifiedWeightBeforeForgetting(t *testing.T) {
	p, r, ep, _ := recoveryFixture(t)
	p.DrainEndpoint(ep)
	r.ignoreWrites = true
	if err := p.reconcileDrains(); err == nil {
		t.Fatal("unapplied restore acknowledged as successful")
	}
	if len(p.drainMgr.weights[ep.Name]) != 2 {
		t.Fatal("unverified restore deleted ownership")
	}
	r.ignoreWrites = false
	if err := p.reconcileDrains(); err != nil {
		t.Fatal(err)
	}
}

func TestDrainRecoveryPreservesManualStateAndDesiredZero(t *testing.T) {
	for _, mode := range []string{"preexisting-zero", "manual-MAINT", "manual-DRAIN", "manual-custom-weight", "desired-zero", "desired-disabled", "removed"} {
		t.Run(mode, func(t *testing.T) {
			p, r, ep, cfg := recoveryFixture(t)
			if mode == "preexisting-zero" {
				state := r.states["be-one/target"]
				state.weight = 0
				r.states[state.backend] = state
			}
			p.DrainEndpoint(ep)
			state := r.states["be-one/target"]
			switch mode {
			case "manual-MAINT":
				state.admin = 1
			case "manual-DRAIN":
				state.admin = 8
			case "manual-custom-weight":
				state.weight = 31
			case "desired-zero":
				ep.Config = "check weight 0"
				p.drainMgr.setDesired(cfg)
			case "desired-disabled":
				ep.Config = "check disabled"
				p.drainMgr.setDesired(cfg)
			case "removed":
				p.drainMgr.setDesired(&config.LoadBalancerConfig{})
			}
			r.states[state.backend] = state
			if err := p.reconcileDrains(); err != nil {
				t.Fatal(err)
			}
			if got := r.states[state.backend]; got.weight != state.weight || got.admin != state.admin {
				t.Fatalf("operator state changed: before=%+v after=%+v", state, got)
			}
		})
	}
}

func TestActiveDrainSurvivesReloadAndRecoversOriginalCustomWeights(t *testing.T) {
	p, r, ep, _ := recoveryFixture(t)
	r.sessions = 1
	p.DrainEndpoint(ep)
	if err := os.WriteFile(p.cfg.PidFile, []byte("456\n"), 0600); err != nil {
		t.Fatal(err)
	}
	for key, state := range r.states {
		state.weight = 1
		r.states[key] = state
	}
	r.fail["set server be-two/target weight 0"] = 1
	if err := p.reconcileDrains(); err == nil {
		t.Fatal("missing reapply failure")
	}
	if err := p.reconcileDrains(); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(runtimeWeights(r), map[string]int{"be-one/target": 0, "be-two/target": 0}) {
		t.Fatal(runtimeWeights(r))
	}
	p.RemoveEndpointFromDrain(ep)
	if err := p.reconcileDrains(); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(runtimeWeights(r), map[string]int{"be-one/target": 7, "be-two/target": 23}) {
		t.Fatal(runtimeWeights(r))
	}
}

func TestActiveDrainDoesNotOverwriteManualRuntimeWeight(t *testing.T) {
	p, r, ep, _ := recoveryFixture(t)
	r.sessions = 1
	p.DrainEndpoint(ep)
	state := r.states["be-one/target"]
	state.weight = 31
	r.states[state.backend] = state
	if err := p.reconcileDrains(); err != nil {
		t.Fatal(err)
	}
	if r.states[state.backend].weight != 31 {
		t.Fatal("active reconciliation overwrote operator weight")
	}
	p.RemoveEndpointFromDrain(ep)
	if err := p.reconcileDrains(); err != nil {
		t.Fatal(err)
	}
	if r.states[state.backend].weight != 31 {
		t.Fatal("recovery overwrote operator weight")
	}
}

func TestDrainRecoveryRetainsOwnershipDuringTransientResolutionFailure(t *testing.T) {
	p, r, ep, _ := recoveryFixture(t)
	p.DrainEndpoint(ep)
	state := r.states["be-one/target"]
	state.admin = 0x20
	state.operational = 0
	r.states[state.backend] = state
	if err := p.reconcileDrains(); err != nil {
		t.Fatal(err)
	}
	if _, owned := p.drainMgr.weights[ep.Name][state.backend]; !owned {
		t.Fatal("transient DNS maintenance lost recovery ownership")
	}
	state.admin = 0
	state.operational = 2
	r.states[state.backend] = state
	if err := p.reconcileDrains(); err != nil {
		t.Fatal(err)
	}
	if r.states[state.backend].weight != 7 {
		t.Fatal("DNS recovery retained stale zero")
	}
}

func TestRecoveryDoesNotAdoptReplacementProcessOrFailedReload(t *testing.T) {
	p, r, ep, cfg := recoveryFixture(t)
	p.DrainEndpoint(ep)
	p.cfg.reloadCommand = func() error { return errors.New("reload failed") }
	if err := p.applyHaproxyConfig(cfg); err == nil {
		t.Fatal("missing reload error")
	}
	if r.states["be-one/target"].weight != 0 || len(p.drainMgr.weights[ep.Name]) != 2 {
		t.Fatal("failed reload restored or forgot state")
	}
	if err := p.reconcileDrains(); err != nil {
		t.Fatal(err)
	}
	if r.states["be-one/target"].weight != 0 || len(p.drainMgr.weights[ep.Name]) != 2 {
		t.Fatal("timer used stale desired state after failed reload")
	}
	p.recoveryBlocked = false // Separately verify ownership after a process replacement.
	if err := os.WriteFile(p.cfg.PidFile, []byte("456\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := p.reconcileDrains(); err != nil {
		t.Fatal(err)
	}
	if r.states["be-one/target"].weight != 0 {
		t.Fatal("adopted replacement process's unowned zero")
	}
}

func TestRuntimeCommandHasReadDeadline(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix socket fixture runs in Linux validation")
	}
	socket := filepath.Join(t.TempDir(), "ha.sock")
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	done := make(chan struct{})
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		<-done
	}()
	defer close(done)
	dm := &drainMgr{haproxySock: socket}
	start := time.Now()
	if _, err := dm.runCommand("show servers state"); err == nil {
		t.Fatal("hung socket did not time out")
	}
	if elapsed := time.Since(start); elapsed < time.Second || elapsed > 4*time.Second {
		t.Fatalf("unexpected deadline %s", elapsed)
	}
}

func TestMalformedRuntimeResponsesFailClosed(t *testing.T) {
	for _, response := range [][]string{{"error"}, {"1", "# unknown"}, {"1", "# be_name srv_name srv_uweight srv_admin_state srv_op_state", "be target bad 0 2"}, {"1", "# be_name srv_name srv_uweight srv_admin_state srv_op_state", "be target 1 0 2 extra"}} {
		dm := &drainMgr{command: func(string) ([]string, error) { return response, nil }}
		if _, err := dm.readServerStates(); err == nil {
			t.Fatalf("accepted malformed state %q", response)
		}
	}
	dm := &drainMgr{command: func(string) ([]string, error) { return []string{"# pxname,svname,scur", "be,target,0,unexpected"}, nil }}
	if _, err := dm.ReadStats(); err == nil {
		t.Fatal("accepted malformed stats")
	}
}
