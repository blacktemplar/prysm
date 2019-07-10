package forkchoice

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"github.com/prysmaticlabs/prysm/beacon-chain/core/blocks"
	"github.com/prysmaticlabs/prysm/beacon-chain/core/state"

	"github.com/prysmaticlabs/go-ssz"
	"github.com/prysmaticlabs/prysm/beacon-chain/core/helpers"
	"github.com/prysmaticlabs/prysm/beacon-chain/db"
	pb "github.com/prysmaticlabs/prysm/proto/beacon/p2p/v1"
	"github.com/prysmaticlabs/prysm/shared/bytesutil"
	"github.com/prysmaticlabs/prysm/shared/params"
)

type Store struct {
	ctx              context.Context
	cancel           context.CancelFunc
	time             uint64
	justifiedCheckpt *pb.Checkpoint
	finalizedCheckpt *pb.Checkpoint
	db               *db.BeaconDB
}

func NewForkChoiceService(ctx context.Context, db *db.BeaconDB) *Store {
	ctx, cancel := context.WithCancel(ctx)

	return &Store{
		ctx:    ctx,
		cancel: cancel,
		db:     db,
	}
}

// GensisStore to be filled
//
// Spec pseudocode definition:
//   def get_genesis_store(genesis_state: BeaconState) -> Store:
//    genesis_block = BeaconBlock(state_root=hash_tree_root(genesis_state))
//    root = signing_root(genesis_block)
//    justified_checkpoint = Checkpoint(epoch=GENESIS_EPOCH, root=root)
//    finalized_checkpoint = Checkpoint(epoch=GENESIS_EPOCH, root=root)
//    return Store(
//        time=genesis_state.genesis_time,
//        justified_checkpoint=justified_checkpoint,
//        finalized_checkpoint=finalized_checkpoint,
//        blocks={root: genesis_block},
//        block_states={root: genesis_state.copy()},
//        checkpoint_states={justified_checkpoint: genesis_state.copy()},
//    )
func (s *Store) GensisStore(state *pb.BeaconState) error {

	stateRoot, err := ssz.HashTreeRoot(state)
	if err != nil {
		return fmt.Errorf("could not tree hash genesis state: %v", err)
	}

	genesisBlk := &pb.BeaconBlock{StateRoot: stateRoot[:]}

	blkRoot, err := ssz.HashTreeRoot(genesisBlk)
	if err != nil {
		return fmt.Errorf("could not tree hash genesis block: %v", err)
	}

	s.time = state.GenesisTime
	s.justifiedCheckpt = &pb.Checkpoint{Epoch: 0, Root: blkRoot[:]}
	s.finalizedCheckpt = &pb.Checkpoint{Epoch: 0, Root: blkRoot[:]}

	if err := s.db.SaveBlock(genesisBlk); err != nil {
		return fmt.Errorf("could not save genesis block: %v", err)
	}
	if err := s.db.SaveState(s.ctx, state); err != nil {
		return fmt.Errorf("could not save genesis state: %v", err)
	}
	if err := s.db.SaveCheckpointState(s.ctx, s.justifiedCheckpt, state); err != nil {
		return fmt.Errorf("could not save justified checkpt: %v", err)
	}
	if err := s.db.SaveCheckpointState(s.ctx, s.finalizedCheckpt, state); err != nil {
		return fmt.Errorf("could not save finalized checkpt: %v", err)
	}

	return nil
}

// Ancestor to be filled
//
// Spec pseudocode definition:
//   def get_ancestor(store: Store, root: Hash, slot: Slot) -> Hash:
//    block = store.blocks[root]
//    assert block.slot >= slot
//    return root if block.slot == slot else get_ancestor(store, block.parent_root, slot)
func (s *Store) Ancestor(root []byte, slot uint64) ([]byte, error) {
	b, err := s.db.Block(bytesutil.ToBytes32(root))
	if err != nil {
		return nil, fmt.Errorf("could not get ancestor block: %v", err)
	}

	if b.Slot < slot {
		return nil, fmt.Errorf("ancestor slot %d reacched below wanted slot %d", b.Slot, slot)
	}

	if b.Slot == slot {
		return root, nil
	}

	return s.Ancestor(b.ParentRoot, slot)
}

// LatestAttestingBalance to be filled
//
// Spec pseudocode definition:
//   def get_latest_attesting_balance(store: Store, root: Hash) -> Gwei:
//    state = store.checkpoint_states[store.justified_checkpoint]
//    active_indices = get_active_validator_indices(state, get_current_epoch(state))
//    return Gwei(sum(
//        state.validators[i].effective_balance for i in active_indices
//        if (i in store.latest_messages
//            and get_ancestor(store, store.latest_messages[i].root, store.blocks[root].slot) == root)
//    ))
func (s *Store) LatestAttestingBalance(root []byte) (uint64, error) {
	lastJustifiedState, err := s.db.CheckpointState(s.ctx, s.justifiedCheckpt)
	if err != nil {
		return 0, fmt.Errorf("could not get checkpoint state: %v", err)
	}
	lastJustifiedEpoch := helpers.CurrentEpoch(lastJustifiedState)
	activeIndices, err := helpers.ActiveValidatorIndices(lastJustifiedState, lastJustifiedEpoch)
	if err != nil {
		return 0, fmt.Errorf("could not get active indices for last checkpoint state: %v", err)
	}

	wantedBlk, err := s.db.Block(bytesutil.ToBytes32(root))
	if err != nil {
		return 0, fmt.Errorf("could not get slot for an ancestor block: %v", err)
	}
	balances := uint64(0)

	for _, i := range activeIndices {
		if s.db.HasLatestMessage(i) {
			msg, err := s.db.LatestMessage(i)
			if err != nil {
				return 0, fmt.Errorf("could not get validator %d's latest msg: %v", i, err)
			}
			wantedRoot, err := s.Ancestor(msg.Root, wantedBlk.Slot)
			if err != nil {
				return 0, fmt.Errorf("could not get ancestor root for slot %d: %v", wantedBlk.Slot, err)
			}
			if bytes.Equal(wantedRoot, root) {
				balances += lastJustifiedState.Validators[i].EffectiveBalance
			}
		}
	}
	return balances, nil
}

// Head to be filled
//
// Spec pseudocode definition:
//   def get_head(store: Store) -> Hash:
//    # Execute the LMD-GHOST fork choice
//    head = store.justified_checkpoint.root
//    justified_slot = compute_start_slot_of_epoch(store.justified_checkpoint.epoch)
//    while True:
//        children = [
//            root for root in store.blocks.keys()
//            if store.blocks[root].parent_root == head and store.blocks[root].slot > justified_slot
//        ]
//        if len(children) == 0:
//            return head
//        # Sort by latest attesting balance with ties broken lexicographically
//        head = max(children, key=lambda root: (get_latest_attesting_balance(store, root), root))
func (s *Store) Head() ([]byte, error) {
	head := s.justifiedCheckpt.Root
	justifiedSlot := helpers.StartSlot(s.justifiedCheckpt.Epoch)

	for {
		// TODO: To be implemented, stubbing children and roots
		children := [][]byte{{}}
		roots := [][]byte{{}}

		for _, r := range roots {
			b, err := s.db.Block(bytesutil.ToBytes32(r))
			if err != nil {
				return nil, fmt.Errorf("could not get block: %v", err)
			}
			if bytes.Equal(b.ParentRoot, head) && b.Slot > justifiedSlot {
				children = append(children, r)
			}
		}

		if len(children) == 0 {
			return head, nil
		}

		head := children[0]
		highest, err := s.LatestAttestingBalance(head)
		if err != nil {
			return nil, fmt.Errorf("could not get latest balance: %v", err)
		}
		for _, child := range children[1:] {
			balance, err := s.LatestAttestingBalance(child)
			if err != nil {
				return nil, fmt.Errorf("could not get latest balance: %v", err)
			}
			if balance > highest {
				head = child
			}
		}
	}
}

// OnTick to be filled
//
// Spec pseudocode definition:
//   def on_tick(store: Store, time: uint64) -> None:
//    store.time = time
func (s *Store) OnTick(t uint64) {
	s.time = t
}

// OnBlock to be filled
//
// Spec pseudocode definition:
//   def on_block(store: Store, block: BeaconBlock) -> None:
//    # Make a copy of the state to avoid mutability issues
//    assert block.parent_root in store.block_states
//    pre_state = store.block_states[block.parent_root].copy()
//    # Blocks cannot be in the future. If they are, their consideration must be delayed until the are in the past.
//    assert store.time >= pre_state.genesis_time + block.slot * SECONDS_PER_SLOT
//    # Add new block to the store
//    store.blocks[signing_root(block)] = block
//    # Check block is a descendant of the finalized block
//    assert (
//        get_ancestor(store, signing_root(block), store.blocks[store.finalized_checkpoint.root].slot) ==
//        store.finalized_checkpoint.root
//    )
//    # Check that block is later than the finalized epoch slot
//    assert block.slot > compute_start_slot_of_epoch(store.finalized_checkpoint.epoch)
//    # Check the block is valid and compute the post-state
//    state = state_transition(pre_state, block)
//    # Add new state for this block to the store
//    store.block_states[signing_root(block)] = state
//
//    # Update justified checkpoint
//    if state.current_justified_checkpoint.epoch > store.justified_checkpoint.epoch:
//        store.justified_checkpoint = state.current_justified_checkpoint
//
//    # Update finalized checkpoint
//    if state.finalized_checkpoint.epoch > store.finalized_checkpoint.epoch:
//        store.finalized_checkpoint = state.finalized_checkpoint
func (s *Store) OnBlock(b *pb.BeaconBlock) error {
	// TODO: Implement HistoryStateFromBlkRoot
	preState, err := s.db.HistoricalStateFromSlot(s.ctx, b.Slot, bytesutil.ToBytes32(b.ParentRoot))
	if err != nil {
		return fmt.Errorf("could not get pre state for slot %d: %v", b.Slot, err)
	}
	if preState == nil {
		return fmt.Errorf("pre state of slot %d does not exist: %v", b.Slot, err)
	}

	// Blocks cannot be in the future. If they are, their consideration must be delayed until the are in the past.
	slotTime := preState.GenesisTime + b.Slot * params.BeaconConfig().SecondsPerSlot
	if slotTime > s.time {
		return fmt.Errorf("could not process block from the future, %d > %d", slotTime, s.time)
	}

	// TODO: Why would you save the block here?
	if err := s.db.SaveBlock(b); err != nil {
		return fmt.Errorf("could not save block from slot %d: %v", b.Slot, err)
	}

	// Verify block is a descendent of a finalized block.
	finalizedBlk, err := s.db.Block(bytesutil.ToBytes32(s.finalizedCheckpt.Root))
	if err != nil {
		return fmt.Errorf("could not get finalized block: %v", err)
	}
	root, err := ssz.SigningRoot(b)
	if err != nil {
		return fmt.Errorf("could not get sign root of block %d: %v", b.Slot, err)
	}
	bFinalizedRoot, err := s.Ancestor(root[:], finalizedBlk.Slot)
	if !bytes.Equal(bFinalizedRoot, s.finalizedCheckpt.Root) {
		return fmt.Errorf("block from slot %d is not a descendent of the current finalized block", b.Slot)
	}

	// Verify block is later than the finalized epoch slot.
	finalizedSlot := helpers.StartSlot(s.finalizedCheckpt.Epoch)
	if finalizedSlot >= b.Slot {
		return fmt.Errorf("block is equal or earlier than finalized block, %d < %d", b.Slot, finalizedSlot)
	}

	// Apply new state transition for the block to the store.
	postState, err := state.ExecuteStateTransition(
		s.ctx,
		preState,
		b,
		state.DefaultConfig(),
	)
	if err != nil {
		return fmt.Errorf("could not execute state transition: %v", err)
	}

	// TODO: Need to save state based on block root as key, not state root
	if err := s.db.SaveState(s.ctx, postState); err != nil {
		return fmt.Errorf("could not save state: %v", err)
	}

	// Update justified check point.
	if postState.CurrentJustifiedCheckpoint.Epoch > s.justifiedCheckpt.Epoch {
		s.justifiedCheckpt.Epoch = postState.CurrentJustifiedCheckpoint.Epoch
	}

	// Update finalized check point.
	if postState.FinalizedCheckpoint.Epoch > s.finalizedCheckpt.Epoch {
		s.finalizedCheckpt.Epoch = postState.FinalizedCheckpoint.Epoch
	}

	return nil
}

// OnAttestation to be filled
//
// Spec pseudocode definition:
//   def on_attestation(store: Store, attestation: Attestation) -> None:
//    target = attestation.data.target
//
//    # Cannot calculate the current shuffling if have not seen the target
//    assert target.root in store.blocks
//
//    # Attestations cannot be from future epochs. If they are, delay consideration until the epoch arrives
//    base_state = store.block_states[target.root].copy()
//    assert store.time >= base_state.genesis_time + compute_start_slot_of_epoch(target.epoch) * SECONDS_PER_SLOT
//
//    # Store target checkpoint state if not yet seen
//    if target not in store.checkpoint_states:
//        process_slots(base_state, compute_start_slot_of_epoch(target.epoch))
//        store.checkpoint_states[target] = base_state
//    target_state = store.checkpoint_states[target]
//
//    # Attestations can only affect the fork choice of subsequent slots.
//    # Delay consideration in the fork choice until their slot is in the past.
//    attestation_slot = get_attestation_data_slot(target_state, attestation.data)
//    assert store.time >= (attestation_slot + 1) * SECONDS_PER_SLOT
//
//    # Get state at the `target` to validate attestation and calculate the committees
//    indexed_attestation = get_indexed_attestation(target_state, attestation)
//    assert is_valid_indexed_attestation(target_state, indexed_attestation)
//
//    # Update latest messages
//    for i in indexed_attestation.custody_bit_0_indices + indexed_attestation.custody_bit_1_indices:
//        if i not in store.latest_messages or target.epoch > store.latest_messages[i].epoch:
//            store.latest_messages[i] = LatestMessage(epoch=target.epoch, root=attestation.data.beacon_block_root)
func (s *Store) OnAttestation(a *pb.Attestation) error {
	tgt := a.Data.Target

	// Verify beacon node has seen the target block before.
	if !s.db.HasBlock(bytesutil.ToBytes32(tgt.Root)) {
		return fmt.Errorf("target root %#x does not exist in db", bytesutil.Trunc(tgt.Root))
	}

	// Verify Attestations cannot be from future epochs.
	// If they are, delay consideration until the epoch arrives
	// TODO: Implement HistoryStateFromBlkRoot
	tgtSlot := helpers.StartSlot(tgt.Epoch)
	baseState, err := s.db.HistoricalStateFromSlot(s.ctx, tgtSlot, bytesutil.ToBytes32(tgt.Root))
	if err != nil {
		return fmt.Errorf("could not get pre state for slot %d: %v", tgtSlot, err)
	}
	if baseState == nil {
		return fmt.Errorf("pre state of slot %d does not exist: %v", tgtSlot, err)
	}
	slotTime := baseState.GenesisTime + tgtSlot * params.BeaconConfig().SecondsPerSlot
	if slotTime > s.time {
		return fmt.Errorf("could not process attestation from the future, %d > %d", slotTime, s.time)
	}


	// Store target checkpoint state if not yet seen.
	exists, err := s.db.HasCheckpoint(tgt)
	if err != nil {
		return fmt.Errorf("could not get check point state: %v", err)
	}
	if !exists {
		baseState, err = state.ProcessSlots(s.ctx, baseState, tgtSlot);
		if err != nil {
			return fmt.Errorf("could not process slots up to %d", tgtSlot, err)
		}
		if err := s.db.SaveCheckpointState(s.ctx, tgt, baseState); err != nil {
			return fmt.Errorf("could not save check point state: %v", err)
		}
	}

	// Verify attestations can only affect the fork choice of subsequent slots.
	// Delay consideration in the fork choice until their slot is in the past.
	aSlot, err := helpers.AttestationDataSlot(baseState, a.Data)
	if err != nil {
		return fmt.Errorf("could not get attestation slot: %v", err)
	}
	slotTime = baseState.GenesisTime + (aSlot + 1) * params.BeaconConfig().SecondsPerSlot
	if slotTime > s.time {
		return fmt.Errorf("could not process attestation for fork choice, %d > %d", slotTime, s.time)
	}

	// Use the target state to to validate attestation and calculate the committees.
	// TODO: Implement get_indexed_attestation
	indexedAtt := &pb.IndexedAttestation{}
	if err := blocks.VerifyIndexedAttestation(indexedAtt, true); err != nil {
		 return errors.New("could not verify indexed attestation")
	}

	// Update every validator's latest message.
	for _, i := range append(indexedAtt.CustodyBit_0Indices, indexedAtt.CustodyBit_1Indices...) {
		s.db.HasLatestMessage(i)
		msg, err := s.db.LatestMessage(i)
		if err != nil {
			return fmt.Errorf("could not get latest msg for validator %d: %v", i, err)
		}
		if s.db.HasLatestMessage(i) || tgt.Epoch > msg.Epoch {
			if err := s.db.SaveLatestMessage(s.ctx, i, &pb.LatestMessage{
				Epoch: tgt.Epoch,
				Root: a.Data.BeaconBlockRoot,
			}); err != nil {
				return fmt.Errorf("could not save latest msg for validator %d: %v", i, err)
			}
		}
	}
	return nil
}
