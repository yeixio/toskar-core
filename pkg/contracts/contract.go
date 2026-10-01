package contracts

import (
	"fmt"
	"strconv"
	"strings"
)

// ContractVersion is the version of the client contract (spec §68): the
// events, run traces, citations, files, and steps the desktop app, mobile
// apps, and other clients read. It is major.minor.
//
//   - Minor rises when fields or event types are added. Clients ignore what
//     they do not know, so an older client keeps working.
//   - Major rises only when something is removed or changes meaning. A
//     client built for another major version is told to update.
//
// tests/contract checks that no field in the contract is removed or renamed.
const ContractVersion = "1.0"

// Headers that carry the contract version.
const (
	// ContractHeader is on every API response.
	ContractHeader = "Yggdrasil-Contract"
	// ClientContractHeader is what a client may send: the contract it was
	// built for.
	ClientContractHeader = "Yggdrasil-Client-Contract"
)

// ContractInfo describes the contract in /api/v1/version.
type ContractInfo struct {
	Version string `json:"version"`
	// Major is the major version clients must match.
	Major int `json:"major"`
}

// CurrentContract describes the contract this build speaks.
func CurrentContract() ContractInfo {
	major, _ := ContractMajor(ContractVersion)
	return ContractInfo{Version: ContractVersion, Major: major}
}

// ContractMajor reads the major version of "1.4" or "1".
func ContractMajor(v string) (int, bool) {
	head, _, _ := strings.Cut(strings.TrimSpace(v), ".")
	n, err := strconv.Atoi(head)
	return n, err == nil && n > 0
}

// CheckClientContract reports whether a client built for version v can talk
// to this build. Any minor version of the same major works; an empty
// version means the client did not say, which is allowed.
func CheckClientContract(v string) error {
	if strings.TrimSpace(v) == "" {
		return nil
	}
	client, ok := ContractMajor(v)
	if !ok {
		return fmt.Errorf("%s must look like 1.0", ClientContractHeader)
	}
	server, _ := ContractMajor(ContractVersion)
	switch {
	case client > server:
		return fmt.Errorf("this app needs Yggdrasil contract %d.x, and this Yggdrasil speaks %s. Update Yggdrasil", client, ContractVersion)
	case client < server:
		return fmt.Errorf("this app was built for Yggdrasil contract %d.x, and this Yggdrasil speaks %s. Update the app", client, ContractVersion)
	}
	return nil
}
