package evidence

import (
	"bytes"
	"errors"
	"fmt"
	"time"

	tmproto "github.com/tendermint/tendermint/proto/tendermint/types"
	"github.com/tendermint/tendermint/light"
	sm "github.com/tendermint/tendermint/state"
	"github.com/tendermint/tendermint/types"
)

// VerifyEvidence verifies the evidence fully by checking:
// - it is sufficiently recent (MaxAge)
// - it is from a key who was a validator at the given height
// - it is internally consistent with state
// - it was properly signed by the alleged equivocator
func VerifyEvidence(evidence types.Evidence, state sm.State, stateDB StateStore, blockStore BlockStore) error {
	var (
		height         = state.LastBlockHeight
		evidenceParams = state.ConsensusParams.Evidence

		ageDuration  = state.LastBlockTime.Sub(evidence.Time())
		ageNumBlocks = height - evidence.Height()

		header *types.Header
	)

	// verify the time of the evidence
	blockMeta := blockStore.LoadBlockMeta(evidence.Height())
	header = &blockMeta.Header
	if header == nil {
		return fmt.Errorf("don't have header at height #%d", evidence.Height())
	}
	if header.Time != evidence.Time() {
		return fmt.Errorf("evidence time (%v) is different to the time of the header we have for the same height (%v)",
			evidence.Time(),
			header.Time,
		)
	}

	// check that the evidence hasn't expired
	if ageDuration > evidenceParams.MaxAgeDuration && ageNumBlocks > evidenceParams.MaxAgeNumBlocks {
		return fmt.Errorf(
			"evidence from height %d (created at: %v) is too old; min height is %d and evidence can not be older than %v",
			evidence.Height(),
			evidence.Time(),
			height-evidenceParams.MaxAgeNumBlocks,
			state.LastBlockTime.Add(evidenceParams.MaxAgeDuration),
		)
	}
	
	// apply the evidence-specific verification logic
	switch evidence.(type) {
	case *types.DuplicateVoteEvidence:
		return VerifyDuplicateVote(evidence.(*types.DuplicateVoteEvidence), state.ChainID, stateDB)
	case *types.LightClientAttackEvidence:
		return VerifyLightClientAttack(evidence.(*types.LightClientAttackEvidence), state, stateDB, blockStore)
	default:
		return fmt.Errorf("unrecognized evidence: %v", evidence)
	}
}

// VerifyLightClientAttack verifies LightClientAttackEvidence against the state of the full node. This involves
// the following checks:
//     - same chain ID
//     - the common header from the full node has at least 1/3 voting power which is also present in 
//       the conflicting header's commit
//     - the nodes trusted header at the same height as the conflicting header has a different hash
func VerifyLightClientAttack(e *types.LightClientAttackEvidence, state sm.State, stateDB StateStore, blockStore BlockStore) error {
	if e.ConflictingBlock.ChainID != state.ChainID {
		return fmt.Errorf("different chainID: evidence: %s, ours: %s", e.ConflictingBlock.ChainID, state.ChainID)
	}
	
	commonHeader, err := getSignedHeader(blockStore, e.Height())
	if err != nil {
		return err
	}
	commonVals, err := stateDB.LoadValidators(e.Height())
	if err != nil {
		return err
	}

	err = light.Verify(commonHeader, commonVals, e.ConflictingBlock.SignedHeader, e.ConflictingBlock.ValidatorSet, 
	state.ConsensusParams.Evidence.MaxAgeDuration, state.LastBlockTime, 0 * time.Second, light.DefaultTrustLevel)
	if err != nil {
		return fmt.Errorf("skipping verification from common to conflicting header failed: %w", err)
	}
	
	trustedHeader, err := getSignedHeader(blockStore, e.ConflictingBlock.Height)
	if err != nil {
		return err 
	}
	
	if bytes.Equal(trustedHeader.Hash(), e.ConflictingBlock.Hash()) {
		return fmt.Errorf("trusted header hash matches the evidence conflicting header (%X = %X)", 
		trustedHeader.Hash(), e.ConflictingBlock.Hash())
	}
	
	switch e.AttackType {
	case tmproto.LightClientAttackType_LUNATIC:
		if !light.IsInvalidHeader(trustedHeader.Header, e.ConflictingBlock.Header) {
			return errors.New("light client attack is not lunatic")
		}
	case tmproto.LightClientAttackType_EQUIVOCATION:
		if trustedHeader.Commit.Round != e.ConflictingBlock.Commit.Round {
			return errors.New("light client attack is not equivocation")
		}
	case tmproto.LightClientAttackType_AMNESIA:
		if trustedHeader.Commit.Round == e.ConflictingBlock.Commit.Round {
			return errors.New("light client attack is not amnesia")
		}
	default:
		return  fmt.Errorf("Unrecognized light client attack type #%d", e.AttackType)
	}
	
	return nil
}

// VerifyDuplicateVote verifies DuplicateVoteEvidence against the state of full node. This involves the
// following checks:
//      - the validator is in the validator set at the height of the evidence
//      - the height, round, type and validator address of the votes must be the same
//      - the block ID's must be different
//      - The signatures must both be valid
func VerifyDuplicateVote(e *types.DuplicateVoteEvidence, chainID string,  stateDB StateStore) error {
	valSet, err := stateDB.LoadValidators(e.Height())
	if err != nil {
		return fmt.Errorf("verifying duplicate vote evidence: %w", err)
	}
	_, val := valSet.GetByAddress(e.Addresses()[0])
	if val == nil {
		return fmt.Errorf("address %X was not a validator at height %d", e.Addresses()[0], e.Height())
	}
	pubKey := val.PubKey

	// H/R/S must be the same
	if e.VoteA.Height != e.VoteB.Height ||
		e.VoteA.Round != e.VoteB.Round ||
		e.VoteA.Type != e.VoteB.Type {
		return fmt.Errorf("h/r/s does not match: %d/%d/%v vs %d/%d/%v",
			e.VoteA.Height, e.VoteA.Round, e.VoteA.Type,
			e.VoteB.Height, e.VoteB.Round, e.VoteB.Type)
	}

	// Address must be the same
	if !bytes.Equal(e.VoteA.ValidatorAddress, e.VoteB.ValidatorAddress) {
		return fmt.Errorf("validator addresses do not match: %X vs %X",
			e.VoteA.ValidatorAddress,
			e.VoteB.ValidatorAddress,
		)
	}

	// BlockIDs must be different
	if e.VoteA.BlockID.Equals(e.VoteB.BlockID) {
		return fmt.Errorf(
			"block IDs are the same (%v) - not a real duplicate vote",
			e.VoteA.BlockID,
		)
	}

	// pubkey must match address (this should already be true, sanity check)
	addr := e.VoteA.ValidatorAddress
	if !bytes.Equal(pubKey.Address(), addr) {
		return fmt.Errorf("address (%X) doesn't match pubkey (%v - %X)",
			addr, pubKey, pubKey.Address())
	}
	va := e.VoteA.ToProto()
	vb := e.VoteB.ToProto()
	// Signatures must be valid
	if !pubKey.VerifySignature(types.VoteSignBytes(chainID, va), e.VoteA.Signature) {
		return fmt.Errorf("verifying VoteA: %w", types.ErrVoteInvalidSignature)
	}
	if !pubKey.VerifySignature(types.VoteSignBytes(chainID, vb), e.VoteB.Signature) {
		return fmt.Errorf("verifying VoteB: %w", types.ErrVoteInvalidSignature)
	}

	return nil
}

func getSignedHeader(blockStore BlockStore, height int64) (*types.SignedHeader, error) {
	blockMeta := blockStore.LoadBlockMeta(height)
	if blockMeta == nil {
		return nil, fmt.Errorf("don't have header at height #%d", height)
	}
	commit := blockStore.LoadBlockCommit(height)
	if commit == nil {
		return nil, fmt.Errorf("don't have commit at height #%d", height)
	}
	return &types.SignedHeader{
		Header: &blockMeta.Header,
		Commit: commit,
	}, nil
}
