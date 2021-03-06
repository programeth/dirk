// Copyright © 2020 Attestant Limited.
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package standard

import (
	context "context"
	"fmt"
	"time"

	"github.com/attestantio/dirk/core"
	"github.com/attestantio/dirk/rules"
	"github.com/attestantio/dirk/services/checker"
	"github.com/attestantio/dirk/services/ruler"
)

// Checkpoint is a copy of the Ethereum 2 Checkpoint struct with SSZ size information.
type Checkpoint struct {
	Epoch uint64
	Root  []byte `ssz-size:"32"`
}

// BeaconAttestation is a copy of the Ethereum 2 BeaconAttestation struct with SSZ size information.
type BeaconAttestation struct {
	Slot            uint64
	CommitteeIndex  uint64
	BeaconBlockRoot []byte `ssz-size:"32"`
	Source          *Checkpoint
	Target          *Checkpoint
}

// SignBeaconAttestation signs a attestation for a beacon block.
func (s *Service) SignBeaconAttestation(
	ctx context.Context,
	credentials *checker.Credentials,
	accountName string,
	pubKey []byte,
	data *rules.SignBeaconAttestationData,
) (
	core.Result,
	[]byte,
) {
	started := time.Now()

	if credentials == nil {
		log.Error().Msg("No credentials supplied")
		return core.ResultFailed, nil
	}

	log := log.With().
		Str("request_id", credentials.RequestID).
		Str("client", credentials.Client).
		Str("action", "SignBeaconAttestation").
		Logger()
	log.Trace().Msg("Signing")

	// Check input.
	if data == nil {
		log.Warn().Str("result", "denied").Msg("Request missing data")
		s.monitor.SignCompleted(started, "attestation", core.ResultDenied)
		return core.ResultDenied, nil
	}
	if data.BeaconBlockRoot == nil {
		log.Warn().Str("result", "denied").Msg("Request missing beacon block root")
		s.monitor.SignCompleted(started, "attestation", core.ResultDenied)
		return core.ResultDenied, nil
	}
	if data.Domain == nil {
		log.Warn().Str("result", "denied").Msg("Request missing domain")
		s.monitor.SignCompleted(started, "attestation", core.ResultDenied)
		return core.ResultDenied, nil
	}
	if data.Source == nil {
		log.Warn().Str("result", "denied").Msg("Request missing source")
		s.monitor.SignCompleted(started, "attestation", core.ResultDenied)
		return core.ResultDenied, nil
	}
	if data.Source.Root == nil {
		log.Warn().Str("result", "denied").Msg("Request missing source root")
		s.monitor.SignCompleted(started, "attestation", core.ResultDenied)
		return core.ResultDenied, nil
	}
	if data.Target == nil {
		log.Warn().Str("result", "denied").Msg("Request missing target")
		s.monitor.SignCompleted(started, "attestation", core.ResultDenied)
		return core.ResultDenied, nil
	}
	if data.Target.Root == nil {
		log.Warn().Str("result", "denied").Msg("Request missing target root")
		s.monitor.SignCompleted(started, "attestation", core.ResultDenied)
		return core.ResultDenied, nil
	}

	wallet, account, checkRes := s.preCheck(ctx, credentials, accountName, pubKey, ruler.ActionSignBeaconAttestation)
	if checkRes != core.ResultSucceeded {
		s.monitor.SignCompleted(started, "attestation", checkRes)
		return checkRes, nil
	}
	accountName = fmt.Sprintf("%s/%s", wallet.Name(), account.Name())
	log = log.With().Str("account", accountName).Logger()

	// Confirm approval via rules.
	result := s.ruler.RunRules(ctx, credentials, ruler.ActionSignBeaconAttestation, wallet.Name(), account.Name(), account.PublicKey().Marshal(), data)
	switch result {
	case rules.DENIED:
		log.Debug().Str("result", "denied").Msg("Denied by rules")
		s.monitor.SignCompleted(started, "attestation", core.ResultDenied)
		return core.ResultDenied, nil
	case rules.FAILED:
		log.Error().Str("result", "failed").Msg("Rules check failed")
		s.monitor.SignCompleted(started, "attestation", core.ResultFailed)
		return core.ResultFailed, nil
	}

	// Create a local copy of the data; we need ssz size information to calculate the correct root.
	attestation := &BeaconAttestation{
		Slot:            data.Slot,
		CommitteeIndex:  data.CommitteeIndex,
		BeaconBlockRoot: data.BeaconBlockRoot,
		Source: &Checkpoint{
			Epoch: data.Source.Epoch,
			Root:  data.Source.Root,
		},
		Target: &Checkpoint{
			Epoch: data.Target.Epoch,
			Root:  data.Target.Root,
		},
	}

	// Sign it.
	signingRoot, err := generateSigningRootFromData(ctx, attestation, data.Domain)
	if err != nil {
		log.Error().Err(err).Str("result", "failed").Msg("Failed to generate signing root")
		s.monitor.SignCompleted(started, "attestation", core.ResultFailed)
		return core.ResultFailed, nil
	}
	signature, err := signRoot(ctx, account, signingRoot[:])
	if err != nil {
		log.Error().Err(err).Str("result", "failed").Msg("Failed to sign")
		s.monitor.SignCompleted(started, "attestation", core.ResultFailed)
		return core.ResultFailed, nil
	}

	log.Trace().Str("result", "succeeded").Msg("Success")
	s.monitor.SignCompleted(started, "attestation", core.ResultSucceeded)
	return core.ResultSucceeded, signature
}
