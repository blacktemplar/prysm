// Code generated by yaml_to_go. DO NOT EDIT.
// source: genesis_initialization_minimal.yaml

package spectest

import ethpb "github.com/prysmaticlabs/prysm/proto/eth/v1alpha1"

type GenesisInitializationTest struct {
	Title         string   `json:"title"`
	Summary       string   `json:"summary"`
	ForksTimeline string   `json:"forks_timeline"`
	Forks         []string `json:"forks"`
	Config        string   `json:"config"`
	Runner        string   `json:"runner"`
	Handler       string   `json:"handler"`
	TestCases     []struct {
		Description   string           `json:"description"`
		Eth1BlockHash []byte           `json:"eth1_block_hash"`
		Eth1Timestamp uint64           `json:"eth1_timestamp"`
		Deposits      []*ethpb.Deposit `json:"deposits"`
		State         *pb.BeaconState  `json:"state"`
	} `json:"test_cases"`
}
