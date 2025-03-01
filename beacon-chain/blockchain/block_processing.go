package blockchain

import (
	"bytes"
	"context"
	"errors"
	"fmt"

	"github.com/prysmaticlabs/go-ssz"
	b "github.com/prysmaticlabs/prysm/beacon-chain/core/blocks"
	"github.com/prysmaticlabs/prysm/beacon-chain/core/helpers"
	"github.com/prysmaticlabs/prysm/beacon-chain/core/state"
	"github.com/prysmaticlabs/prysm/beacon-chain/core/validators"
	pb "github.com/prysmaticlabs/prysm/proto/beacon/p2p/v1"
	ethpb "github.com/prysmaticlabs/prysm/proto/eth/v1alpha1"
	"github.com/prysmaticlabs/prysm/shared/bytesutil"
	"github.com/prysmaticlabs/prysm/shared/event"
	"github.com/sirupsen/logrus"
	"go.opencensus.io/trace"
)

// BlockReceiver interface defines the methods in the blockchain service which
// directly receives a new block from other services and applies the full processing pipeline.
type BlockReceiver interface {
	CanonicalBlockFeed() *event.Feed
	ReceiveBlock(ctx context.Context, block *ethpb.BeaconBlock) (*pb.BeaconState, error)
	IsCanonical(slot uint64, hash []byte) bool
	UpdateCanonicalRoots(block *ethpb.BeaconBlock, root [32]byte)
}

// BlockProcessor defines a common interface for methods useful for directly applying state transitions
// to beacon blocks and generating a new beacon state from the Ethereum 2.0 core primitives.
type BlockProcessor interface {
	VerifyBlockValidity(ctx context.Context, block *ethpb.BeaconBlock, beaconState *pb.BeaconState) error
	AdvanceState(ctx context.Context, beaconState *pb.BeaconState, block *ethpb.BeaconBlock) (*pb.BeaconState, error)
	CleanupBlockOperations(ctx context.Context, block *ethpb.BeaconBlock) error
}

// BlockFailedProcessingErr represents a block failing a state transition function.
type BlockFailedProcessingErr struct {
	err error
}

func (b *BlockFailedProcessingErr) Error() string {
	return fmt.Sprintf("block failed processing: %v", b.err)
}

// ReceiveBlock is a function that defines the operations that are preformed on
// any block that is received from p2p layer or rpc. It performs the following actions: It checks the block to see
// 1. Verify a block passes pre-processing conditions
// 2. Save and broadcast the block via p2p to other peers
// 3. Apply the block state transition function and account for skip slots.
// 4. Process and cleanup any block operations, such as attestations and deposits, which would need to be
//    either included or flushed from the beacon node's runtime.
func (c *ChainService) ReceiveBlock(ctx context.Context, block *ethpb.BeaconBlock) (*pb.BeaconState, error) {
	c.receiveBlockLock.Lock()
	defer c.receiveBlockLock.Unlock()
	ctx, span := trace.StartSpan(ctx, "beacon-chain.blockchain.ReceiveBlock")
	defer span.End()
	parentRoot := bytesutil.ToBytes32(block.ParentRoot)
	parent, err := c.beaconDB.Block(parentRoot)
	if err != nil {
		return nil, fmt.Errorf("failed to get parent block: %v", err)
	}
	if parent == nil {
		return nil, errors.New("parent does not exist in DB")
	}
	beaconState, err := c.beaconDB.HistoricalStateFromSlot(ctx, parent.Slot, parentRoot)
	if err != nil {
		return nil, fmt.Errorf("could not retrieve beacon state: %v", err)
	}

	blockRoot, err := ssz.SigningRoot(block)
	if err != nil {
		return nil, fmt.Errorf("could not hash beacon block")
	}
	// We first verify the block's basic validity conditions.
	if err := c.VerifyBlockValidity(ctx, block, beaconState); err != nil {
		return beaconState, fmt.Errorf("block with slot %d is not ready for processing: %v", block.Slot, err)
	}

	// We save the block to the DB and broadcast it to our peers.
	if err := c.SaveAndBroadcastBlock(ctx, block); err != nil {
		return beaconState, fmt.Errorf(
			"could not save and broadcast beacon block with slot %d: %v",
			block.Slot, err,
		)
	}

	log.WithField("slot", block.Slot).Info("Executing state transition")

	// We then apply the block state transition accordingly to obtain the resulting beacon state.
	beaconState, err = c.AdvanceState(ctx, beaconState, block)
	if err != nil {
		switch err.(type) {
		case *BlockFailedProcessingErr:
			// If the block fails processing, we mark it as blacklisted and delete it from our DB.
			c.beaconDB.MarkEvilBlockHash(blockRoot)
			if err := c.beaconDB.DeleteBlock(block); err != nil {
				return nil, fmt.Errorf("could not delete bad block from db: %v", err)
			}
			return beaconState, err
		default:
			return beaconState, fmt.Errorf("could not apply block state transition: %v", err)
		}
	}

	log.WithFields(logrus.Fields{
		"slot":  block.Slot,
		"epoch": helpers.SlotToEpoch(block.Slot),
	}).Info("State transition complete")

	// Check state root
	stateRoot, err := ssz.HashTreeRoot(beaconState)
	if err != nil {
		return nil, fmt.Errorf("could not hash beacon state: %v", err)
	}
	if !bytes.Equal(block.StateRoot, stateRoot[:]) {
		return nil, fmt.Errorf("beacon state root is not equal to block state root: %#x != %#x", stateRoot, block.StateRoot)
	}

	// We process the block's contained deposits, attestations, and other operations
	// and that may need to be stored or deleted from the beacon node's persistent storage.
	if err := c.CleanupBlockOperations(ctx, block); err != nil {
		return beaconState, fmt.Errorf("could not process block deposits, attestations, and other operations: %v", err)
	}

	log.WithFields(logrus.Fields{
		"slot":         block.Slot,
		"attestations": len(block.Body.Attestations),
		"deposits":     len(block.Body.Deposits),
	}).Info("Finished processing beacon block")

	return beaconState, nil
}

// VerifyBlockValidity cross-checks the block against the pre-processing conditions from
// Ethereum 2.0, namely:
//   The parent block with root block.parent_root has been processed and accepted.
//   The node has processed its state up to slot, block.slot - 1.
//   The Ethereum 1.0 block pointed to by the state.processed_pow_receipt_root has been processed and accepted.
//   The node's local clock time is greater than or equal to state.genesis_time + block.slot * SECONDS_PER_SLOT.
func (c *ChainService) VerifyBlockValidity(
	ctx context.Context,
	block *ethpb.BeaconBlock,
	beaconState *pb.BeaconState,
) error {
	if block.Slot == 0 {
		return fmt.Errorf("cannot process a genesis block: received block with slot %d",
			block.Slot)
	}
	powBlockFetcher := c.web3Service.Client().BlockByHash
	if err := b.IsValidBlock(ctx, beaconState, block,
		c.beaconDB.HasBlock, powBlockFetcher, c.genesisTime); err != nil {
		return fmt.Errorf("block does not fulfill pre-processing conditions %v", err)
	}
	return nil
}

// SaveAndBroadcastBlock stores the block in persistent storage and then broadcasts it to
// peers via p2p. Blocks which have already been saved are not processed again via p2p, which is why
// the order of operations is important in this function to prevent infinite p2p loops.
func (c *ChainService) SaveAndBroadcastBlock(ctx context.Context, block *ethpb.BeaconBlock) error {
	blockRoot, err := ssz.SigningRoot(block)
	if err != nil {
		return fmt.Errorf("could not tree hash incoming block: %v", err)
	}
	if err := c.beaconDB.SaveBlock(block); err != nil {
		return fmt.Errorf("failed to save block: %v", err)
	}
	if err := c.beaconDB.SaveAttestationTarget(ctx, &pb.AttestationTarget{
		Slot:            block.Slot,
		BeaconBlockRoot: blockRoot[:],
		ParentRoot:      block.ParentRoot,
	}); err != nil {
		return fmt.Errorf("failed to save attestation target: %v", err)
	}
	// Announce the new block to the network.
	c.p2p.Broadcast(ctx, &pb.BeaconBlockAnnounce{
		Hash:       blockRoot[:],
		SlotNumber: block.Slot,
	})
	return nil
}

// CleanupBlockOperations processes and cleans up any block operations relevant to the beacon node
// such as attestations, exits, and deposits. We update the latest seen attestation by validator
// in the local node's runtime, cleanup and remove pending deposits which have been included in the block
// from our node's local cache, and process validator exits and more.
func (c *ChainService) CleanupBlockOperations(ctx context.Context, block *ethpb.BeaconBlock) error {
	// Forward processed block to operation pool to remove individual operation from DB.
	if c.opsPoolService.IncomingProcessedBlockFeed().Send(block) == 0 {
		log.Error("Sent processed block to no subscribers")
	}

	if err := c.attsService.BatchUpdateLatestAttestation(ctx, block.Body.Attestations); err != nil {
		return fmt.Errorf("failed to update latest attestation for store: %v", err)
	}

	// Remove pending deposits from the deposit queue.
	for _, dep := range block.Body.Deposits {
		c.beaconDB.RemovePendingDeposit(ctx, dep)
	}
	return nil
}

// AdvanceState executes the Ethereum 2.0 core state transition for the beacon chain and
// updates important checkpoints and local persistent data during epoch transitions. It serves as a wrapper
// around the more low-level, core state transition function primitive.
func (c *ChainService) AdvanceState(
	ctx context.Context,
	beaconState *pb.BeaconState,
	block *ethpb.BeaconBlock,
) (*pb.BeaconState, error) {
	finalizedEpoch := beaconState.FinalizedCheckpoint.Epoch
	newState, err := state.ExecuteStateTransition(
		ctx,
		beaconState,
		block,
		&state.TransitionConfig{
			VerifySignatures: false, // We disable signature verification for now.
		},
	)
	if err != nil {
		return beaconState, &BlockFailedProcessingErr{err}
	}
	// Prune the block cache and helper caches on every new finalized epoch.
	if newState.FinalizedCheckpoint.Epoch > finalizedEpoch {
		helpers.ClearAllCaches()
		c.beaconDB.ClearBlockCache()
	}

	log.WithField(
		"slotsSinceGenesis", newState.Slot,
	).Info("Slot transition successfully processed")

	if block != nil {
		log.WithField(
			"slotsSinceGenesis", newState.Slot,
		).Info("Block transition successfully processed")

		blockRoot, err := ssz.SigningRoot(block)
		if err != nil {
			return nil, err
		}
		// Save Historical States.
		if err := c.beaconDB.SaveHistoricalState(ctx, beaconState, blockRoot); err != nil {
			return nil, fmt.Errorf("could not save historical state: %v", err)
		}
	}

	if helpers.IsEpochStart(newState.Slot) {
		// Save activated validators of this epoch to public key -> index DB.
		if err := c.saveValidatorIdx(newState); err != nil {
			return newState, fmt.Errorf("could not save validator index: %v", err)
		}
		// Delete exited validators of this epoch to public key -> index DB.
		if err := c.deleteValidatorIdx(newState); err != nil {
			return newState, fmt.Errorf("could not delete validator index: %v", err)
		}
		// Update FFG checkpoints in DB.
		if err := c.updateFFGCheckPts(ctx, newState); err != nil {
			return newState, fmt.Errorf("could not update FFG checkpts: %v", err)
		}
		logEpochData(newState)
	}
	return newState, nil
}

// saveValidatorIdx saves the validators public key to index mapping in DB, these
// validators were activated from current epoch. After it saves, current epoch key
// is deleted from ActivatedValidators mapping.
func (c *ChainService) saveValidatorIdx(state *pb.BeaconState) error {
	nextEpoch := helpers.CurrentEpoch(state) + 1
	activatedValidators := validators.ActivatedValFromEpoch(nextEpoch)
	var idxNotInState []uint64
	for _, idx := range activatedValidators {
		// If for some reason the activated validator indices is not in state,
		// we skip them and save them to process for next epoch.
		if int(idx) >= len(state.Validators) {
			idxNotInState = append(idxNotInState, idx)
			continue
		}
		pubKey := state.Validators[idx].PublicKey
		if err := c.beaconDB.SaveValidatorIndex(pubKey, int(idx)); err != nil {
			return fmt.Errorf("could not save validator index: %v", err)
		}
	}
	// Since we are processing next epoch, save the can't processed validator indices
	// to the epoch after that.
	validators.InsertActivatedIndices(nextEpoch+1, idxNotInState)
	validators.DeleteActivatedVal(helpers.CurrentEpoch(state))
	return nil
}

// deleteValidatorIdx deletes the validators public key to index mapping in DB, the
// validators were exited from current epoch. After it deletes, current epoch key
// is deleted from ExitedValidators mapping.
func (c *ChainService) deleteValidatorIdx(state *pb.BeaconState) error {
	exitedValidators := validators.ExitedValFromEpoch(helpers.CurrentEpoch(state) + 1)
	for _, idx := range exitedValidators {
		pubKey := state.Validators[idx].PublicKey
		if err := c.beaconDB.DeleteValidatorIndex(pubKey); err != nil {
			return fmt.Errorf("could not delete validator index: %v", err)
		}
	}
	validators.DeleteExitedVal(helpers.CurrentEpoch(state))
	return nil
}

// logs epoch related data in each epoch transition
func logEpochData(beaconState *pb.BeaconState) {

	log.WithField("currentEpochAttestations", len(beaconState.CurrentEpochAttestations)).Info("Number of current epoch attestations")
	log.WithField("prevEpochAttestations", len(beaconState.PreviousEpochAttestations)).Info("Number of previous epoch attestations")
	log.WithField(
		"previousJustifiedEpoch", beaconState.PreviousJustifiedCheckpoint.Epoch,
	).Info("Previous justified epoch")
	log.WithField(
		"justifiedEpoch", beaconState.CurrentJustifiedCheckpoint.Epoch,
	).Info("Justified epoch")
	log.WithField(
		"finalizedEpoch", beaconState.FinalizedCheckpoint.Epoch,
	).Info("Finalized epoch")
	log.WithField(
		"Deposit Index", beaconState.Eth1DepositIndex,
	).Info("ETH1 Deposit Index")
	log.WithField(
		"numValidators", len(beaconState.Validators),
	).Info("Validator registry length")

	log.WithField(
		"SlotsSinceGenesis", beaconState.Slot,
	).Info("Epoch transition successfully processed")
}
