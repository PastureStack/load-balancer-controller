package rancher

import (
	"testing"

	"github.com/PastureStack/load-balancer-controller/internal/metadata"
)

type drainWeightMetadata struct {
	tMetaFetcher
	state string
}

func (mf drainWeightMetadata) GetService(link string) (*metadata.Service, error) {
	ip := "10.42.0.1"
	if link == "default/two" {
		ip = "10.42.0.2"
	}
	return &metadata.Service{Kind: "service", State: "active", Containers: []metadata.Container{{Name: link, UUID: link, PrimaryIp: ip, State: mf.state}}}, nil
}

func TestStoppingAndRestartedTargetsPreserveDesiredWeightsAcrossRuleMerges(t *testing.T) {
	var oldNames []string
	for _, state := range []string{"running", "stopping", "running", "starting"} {
		c := &LoadBalancerController{MetaFetcher: drainWeightMetadata{state: state}, CertFetcher: tCertFetcher{}, LBProvider: &tProvider{}}
		meta := &LBMetadata{PortRules: []metadata.PortRule{
			{Protocol: "http", Service: "default/one", SourcePort: 80, TargetPort: 8080, Weight: 17},
			{Protocol: "http", Service: "default/two", SourcePort: 80, TargetPort: 8080, Weight: 17},
		}}
		configs, err := c.BuildConfigFromMetadata("lb", "env", "", "any", meta)
		if err != nil {
			t.Fatal(err)
		}
		endpoints := configs[0].FrontendServices[0].BackendServices[0].Endpoints
		if len(endpoints) != 2 {
			t.Fatalf("merged endpoint count=%d", len(endpoints))
		}
		for i, ep := range endpoints {
			want := 17
			if state == "stopping" {
				want = 0
			}
			if ep.Weight != want {
				t.Fatalf("state=%s endpoint=%s weight=%d want=%d", state, ep.Name, ep.Weight, want)
			}
			if oldNames != nil && ep.Name != oldNames[i] {
				t.Fatalf("state transition changed endpoint identity: %s", state)
			}
		}
		oldNames = []string{endpoints[0].Name, endpoints[1].Name}
	}
}
