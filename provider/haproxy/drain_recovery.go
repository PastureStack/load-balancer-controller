package haproxy

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/PastureStack/load-balancer-controller/config"
	utils "github.com/PastureStack/load-balancer-controller/utils"
	log "github.com/sirupsen/logrus"
)

// Only a weight that this controller changed can be restored. In particular,
// an unowned zero weight, MAINT/DRAIN admin state or operator's nonzero weight
// is not evidence of a stale controller drain.
type drainWeight struct {
	weight        int
	initialWeight int
	pid           string
	retry         bool
}

type desiredEndpoint struct {
	weight        int
	disabled      bool
	temporaryZero bool
}

type serverState struct {
	backend       string
	name          string
	weight        int
	initialWeight int
	admin         int
	operational   int
}

// HAProxy's forced/inherited/configuration MAINT and forced/inherited DRAIN,
// plus an FQDN changed through the runtime API, belong to the operator/config.
// RMAINT (0x20) is a transient DNS failure: wait without discarding ownership.
const operatorAdminState = 0x1f | 0x40

func (dm *drainMgr) setDesired(lbConfig *config.LoadBalancerConfig) {
	desired := make(map[string]desiredEndpoint)
	for _, fe := range lbConfig.FrontendServices {
		for _, be := range fe.BackendServices {
			for _, ep := range be.Endpoints {
				state := desiredEndpoint{weight: 1, temporaryZero: ep.Stopping}
				fields := strings.Fields(ep.Config)
				for i, field := range fields {
					if field == "disabled" {
						state.disabled = true
					}
					if field == "weight" && i+1 < len(fields) {
						if weight, err := strconv.Atoi(fields[i+1]); err == nil {
							state.weight = weight
						}
					}
				}
				desired[be.UUID+"/"+ep.Name] = state
			}
		}
	}
	dm.mu.Lock()
	dm.desired = desired
	dm.mu.Unlock()
}

func (dm *drainMgr) readServerStates() (map[string]serverState, error) {
	lines, err := dm.runCommand("show servers state")
	if err != nil {
		return nil, err
	}
	states := make(map[string]serverState)
	var header []string
	for _, line := range lines {
		if strings.HasPrefix(line, "# ") {
			header = strings.Fields(strings.TrimPrefix(line, "# "))
			continue
		}
		if header == nil || strings.TrimSpace(line) == "" {
			continue
		}
		values := strings.Fields(line)
		if len(values) != len(header) {
			return nil, errors.New("invalid HAProxy server state column count")
		}
		fields := make(map[string]string, len(header))
		for i, key := range header {
			fields[key] = values[i]
		}
		weight, weightErr := strconv.Atoi(fields["srv_uweight"])
		initialWeight, initialErr := strconv.Atoi(fields["srv_iweight"])
		admin, adminErr := strconv.Atoi(fields["srv_admin_state"])
		operational, opErr := strconv.Atoi(fields["srv_op_state"])
		if fields["be_name"] == "" || fields["srv_name"] == "" || weightErr != nil || initialErr != nil || adminErr != nil || opErr != nil || weight < 0 || weight > 256 || initialWeight < 0 || initialWeight > 256 || admin < 0 || operational < 0 || operational > 3 {
			return nil, errors.New("invalid HAProxy server state fields")
		}
		key := fields["be_name"] + "/" + fields["srv_name"]
		states[key] = serverState{backend: key, name: fields["srv_name"], weight: weight, initialWeight: initialWeight, admin: admin, operational: operational}
	}
	if header == nil {
		return nil, errors.New("HAProxy server state header is missing")
	}
	for _, required := range []string{"be_name", "srv_name", "srv_uweight", "srv_iweight", "srv_admin_state", "srv_op_state"} {
		found := false
		for _, field := range header {
			found = found || field == required
		}
		if !found {
			return nil, fmt.Errorf("HAProxy server state header is missing %s", required)
		}
	}
	return states, nil
}

func (dm *drainMgr) setWeight(backend string, weight int) error {
	lines, err := dm.runCommand(fmt.Sprintf("set server %s weight %d", backend, weight))
	if err != nil {
		return err
	}
	for _, line := range lines {
		if strings.TrimSpace(line) != "" {
			return errors.New("HAProxy rejected weight change: " + utils.SanitizeLogText(line))
		}
	}
	return nil
}

func (dm *drainMgr) setWeightVerified(backend string, weight int) error {
	if err := dm.setWeight(backend, weight); err != nil {
		return err
	}
	states, err := dm.readServerStates()
	if err != nil {
		return err
	}
	state, exists := states[backend]
	if !exists || state.weight != weight {
		return fmt.Errorf("weight verification failed: backend=%s wantWeight=%d", backend, weight)
	}
	return nil
}

// Caller holds mu. Save provenance before issuing a command: a socket timeout
// can occur after HAProxy applied the zero, and must not lose its recovery key.
func (dm *drainMgr) drainEndpoint(name, pid string, states map[string]serverState) error {
	if dm.weights == nil {
		dm.weights = make(map[string]map[string]drainWeight)
	}
	if dm.weights[name] == nil {
		dm.weights[name] = make(map[string]drainWeight)
	}
	var failures []error
	for key, state := range states {
		if state.name != name || state.admin != 0 || state.weight == 0 {
			continue
		}
		if _, exists := dm.weights[name][key]; !exists {
			dm.weights[name][key] = drainWeight{weight: state.weight, initialWeight: state.initialWeight, pid: pid}
		} else {
			owned := dm.weights[name][key]
			owned.pid = pid
			dm.weights[name][key] = owned
		}
		if err := dm.setWeightVerified(key, 0); err != nil {
			failures = append(failures, fmt.Errorf("backend=%s: %w", key, err))
		}
	}
	return errors.Join(failures...)
}

func (lbp *Provider) reconcileDrains() error {
	if lbp.recoveryBlocked {
		return nil
	}
	dm := lbp.drainMgr
	dm.mu.Lock()
	defer dm.mu.Unlock()
	if len(dm.weights) == 0 || dm.desired == nil {
		return nil
	}
	pid, err := lbp.cfg.readPid()
	if err != nil {
		return fmt.Errorf("read HAProxy pid for drain recovery: %w", err)
	}
	states, err := dm.readServerStates()
	if err != nil {
		return fmt.Errorf("read HAProxy state for drain recovery: %w", err)
	}
	var failures []error
	for name, weights := range dm.weights {
		_, active := dm.drainList[name]
		if active {
			// A reload replaces the process and resets runtime weights. Reapply
			// active drains without losing the weight captured before draining.
			for key, original := range weights {
				state, exists := states[key]
				desired, present := dm.desired[key]
				if !exists || !present || desired.disabled || desired.weight == 0 && !desired.temporaryZero || state.admin&operatorAdminState != 0 {
					delete(weights, key)
					continue
				}
				if state.admin != 0 {
					continue
				}
				if original.pid == pid && !original.retry {
					if state.weight != 0 {
						delete(weights, key)
					} // Observed operator override after our verified zero.
					continue
				}
				if original.pid != pid {
					if desired.temporaryZero && desired.weight == 0 && state.initialWeight == 0 && state.weight == 0 {
						// This replacement process was initialized from our stopping
						// configuration, not an unexplained runtime zero.
						original.pid = pid
						original.retry = false
						weights[key] = original
						continue
					}
					if state.weight == 0 {
						continue
					} // An unowned zero in the replacement process.
					if state.weight != state.initialWeight {
						log.Infof("Preserved runtime weight override after reload: endpoint=%s backend=%s weight=%d initialWeight=%d", name, key, state.weight, state.initialWeight)
						delete(weights, key)
						continue
					}
					if state.initialWeight != desired.weight {
						continue
					} // Cannot prove this is the applied desired config.
				}
				// Only the original owned backends follow a reload; never adopt
				// another backend's intentional zero/disabled runtime state.
				original.pid = pid
				original.retry = true
				weights[key] = original
				if state.weight != 0 {
					if err := dm.setWeightVerified(key, 0); err != nil {
						failures = append(failures, fmt.Errorf("reapply active drain backend=%s: %w", key, err))
						continue
					}
				}
				original.retry = false
				weights[key] = original
			}
			continue
		}
		for key, original := range weights {
			desired, present := dm.desired[key]
			state, exists := states[key]
			if !present || !exists || desired.disabled || desired.weight == 0 && !desired.temporaryZero || state.admin&operatorAdminState != 0 || state.weight != 0 {
				delete(weights, key)
				continue
			}
			if desired.temporaryZero && desired.weight == 0 {
				// DrainComplete and stopping metadata can arrive in either order.
				// Keep the provenance while this lifecycle zero is desired. If a
				// reload initialized that zero, bind only that proven generation.
				if original.pid != pid && state.initialWeight == 0 {
					original.pid = pid
					weights[key] = original
				}
				continue
			}
			if original.pid != pid {
				// No proof this controller zeroed this replacement process. A
				// zero there can be an operator action; do not infer ownership.
				log.Warnf("Drain recovery ownership expired: endpoint=%s backend=%s oldPID=%s currentPID=%s", name, key, original.pid, pid)
				delete(weights, key)
				continue
			}
			if state.admin != 0 || state.operational != 2 {
				continue
			} // Wait for actual health recovery.
			restoreWeight := original.weight
			if desired.weight != original.initialWeight {
				// A later desired-config edit supersedes the old runtime weight.
				// Unchanged config still recovers an operator's original custom
				// runtime weight, rather than resetting it to the config default.
				restoreWeight = desired.weight
			}
			if err := dm.setWeightVerified(key, restoreWeight); err != nil {
				failures = append(failures, fmt.Errorf("restore endpoint=%s backend=%s weight=%d: %w", name, key, restoreWeight, err))
				continue
			}
			// Delete the only proof of drain ownership after verified readback.
			delete(weights, key)
			log.Infof("Recovered controller drain weight: endpoint=%s backend=%s weight=%d", name, key, restoreWeight)
		}
		if len(weights) == 0 {
			delete(dm.weights, name)
		}
	}
	return errors.Join(failures...)
}
