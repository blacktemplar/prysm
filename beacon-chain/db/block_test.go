package db

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/gogo/protobuf/proto"
	"github.com/prysmaticlabs/go-ssz"
	ethpb "github.com/prysmaticlabs/prysm/proto/eth/v1alpha1"
	"github.com/prysmaticlabs/prysm/shared/params"
	"github.com/prysmaticlabs/prysm/shared/testutil"
)

func TestNilDB_OK(t *testing.T) {
	db := setupDB(t)
	defer teardownDB(t, db)

	block := &ethpb.BeaconBlock{}
	h, _ := ssz.SigningRoot(block)

	hasBlock := db.HasBlock(h)
	if hasBlock {
		t.Fatal("HashBlock should return false")
	}

	bPrime, err := db.Block(h)
	if err != nil {
		t.Fatalf("failed to get block: %v", err)
	}
	if bPrime != nil {
		t.Fatalf("get should return nil for a non existent key")
	}
}

func TestSaveBlock_OK(t *testing.T) {
	db := setupDB(t)
	defer teardownDB(t, db)

	block1 := &ethpb.BeaconBlock{}
	h1, _ := ssz.SigningRoot(block1)

	err := db.SaveBlock(block1)
	if err != nil {
		t.Fatalf("save block failed: %v", err)
	}

	b1Prime, err := db.Block(h1)
	if err != nil {
		t.Fatalf("failed to get block: %v", err)
	}
	h1Prime, _ := ssz.SigningRoot(b1Prime)

	if b1Prime == nil || h1 != h1Prime {
		t.Fatalf("get should return b1: %x", h1)
	}

	block2 := &ethpb.BeaconBlock{
		Slot: 0,
	}
	h2, _ := ssz.SigningRoot(block2)

	err = db.SaveBlock(block2)
	if err != nil {
		t.Fatalf("save block failed: %v", err)
	}

	b2Prime, err := db.Block(h2)
	if err != nil {
		t.Fatalf("failed to get block: %v", err)
	}
	h2Prime, _ := ssz.SigningRoot(b2Prime)
	if b2Prime == nil || h2 != h2Prime {
		t.Fatalf("get should return b2: %x", h2)
	}
}

func TestSaveBlock_NilBlkInCache(t *testing.T) {
	db := setupDB(t)
	defer teardownDB(t, db)

	block := &ethpb.BeaconBlock{Slot: 999}
	h1, _ := ssz.SigningRoot(block)

	// Save a nil block to with block root.
	db.blocks[h1] = nil

	if err := db.SaveBlock(block); err != nil {
		t.Fatalf("save block failed: %v", err)
	}

	savedBlock, err := db.Block(h1)
	if err != nil {
		t.Fatal(err)
	}
	if !proto.Equal(block, savedBlock) {
		t.Error("Could not save block in DB")
	}

	// Verify we have the correct cached block
	if !proto.Equal(db.blocks[h1], savedBlock) {
		t.Error("Could not save block in cache")
	}
}

func TestSaveBlockInCache_OK(t *testing.T) {
	db := setupDB(t)
	defer teardownDB(t, db)

	block := &ethpb.BeaconBlock{Slot: 999}
	h, _ := ssz.SigningRoot(block)

	err := db.SaveBlock(block)
	if err != nil {
		t.Fatalf("save block failed: %v", err)
	}

	if !proto.Equal(block, db.blocks[h]) {
		t.Error("Could not save block in cache")
	}

	savedBlock, err := db.Block(h)
	if err != nil {
		t.Fatal(err)
	}
	if !proto.Equal(block, savedBlock) {
		t.Error("Could not save block in cache")
	}
}

func TestDeleteBlock_OK(t *testing.T) {
	db := setupDB(t)
	defer teardownDB(t, db)

	block := &ethpb.BeaconBlock{Slot: 0}
	h, _ := ssz.SigningRoot(block)

	err := db.SaveBlock(block)
	if err != nil {
		t.Fatalf("save block failed: %v", err)
	}

	savedBlock, err := db.Block(h)
	if err != nil {
		t.Fatal(err)
	}
	if !proto.Equal(block, savedBlock) {
		t.Fatal(err)
	}
	if err := db.DeleteBlock(block); err != nil {
		t.Fatal(err)
	}
	savedBlock, err = db.Block(h)
	if err != nil {
		t.Fatal(err)
	}
	if savedBlock != nil {
		t.Errorf("Expected block to have been deleted, received: %v", savedBlock)
	}
}

func TestDeleteBlockInCache_OK(t *testing.T) {
	db := setupDB(t)
	defer teardownDB(t, db)

	block := &ethpb.BeaconBlock{Slot: 0}
	h, _ := ssz.SigningRoot(block)

	err := db.SaveBlock(block)
	if err != nil {
		t.Fatalf("save block failed: %v", err)
	}

	if err := db.DeleteBlock(block); err != nil {
		t.Fatal(err)
	}

	if _, exists := db.blocks[h]; exists {
		t.Error("Expected block to have been deleted")
	}
}

func TestBlocksBySlotEmptyChain_OK(t *testing.T) {
	db := setupDB(t)
	defer teardownDB(t, db)
	ctx := context.Background()

	blocks, _ := db.BlocksBySlot(ctx, 0)
	if len(blocks) > 0 {
		t.Error("BlockBySlot should return nil for an empty chain")
	}
}

func TestBlocksBySlot_MultipleBlocks(t *testing.T) {
	db := setupDB(t)
	defer teardownDB(t, db)
	ctx := context.Background()

	slotNum := uint64(3)
	b1 := &ethpb.BeaconBlock{
		Slot:       slotNum,
		ParentRoot: []byte("A"),
		Body: &ethpb.BeaconBlockBody{
			RandaoReveal: []byte("A"),
		},
	}
	b2 := &ethpb.BeaconBlock{
		Slot:       slotNum,
		ParentRoot: []byte("B"),
		Body: &ethpb.BeaconBlockBody{
			RandaoReveal: []byte("B"),
		}}
	b3 := &ethpb.BeaconBlock{
		Slot:       slotNum,
		ParentRoot: []byte("C"),
		Body: &ethpb.BeaconBlockBody{
			RandaoReveal: []byte("C"),
		}}
	if err := db.SaveBlock(b1); err != nil {
		t.Fatal(err)
	}
	if err := db.SaveBlock(b2); err != nil {
		t.Fatal(err)
	}
	if err := db.SaveBlock(b3); err != nil {
		t.Fatal(err)
	}

	blocks, _ := db.BlocksBySlot(ctx, 3)
	if len(blocks) != 3 {
		t.Errorf("Wanted %d blocks, received %d", 3, len(blocks))
	}
}

func TestUpdateChainHead_NoBlock(t *testing.T) {
	db := setupDB(t)
	defer teardownDB(t, db)
	ctx := context.Background()

	genesisTime := uint64(time.Now().Unix())
	deposits, _ := testutil.SetupInitialDeposits(t, 10)
	err := db.InitializeState(context.Background(), genesisTime, deposits, &ethpb.Eth1Data{})
	if err != nil {
		t.Fatalf("failed to initialize state: %v", err)
	}
	beaconState, err := db.HeadState(ctx)
	if err != nil {
		t.Fatalf("failed to get beacon state: %v", err)
	}

	block := &ethpb.BeaconBlock{Slot: 1}
	if err := db.UpdateChainHead(ctx, block, beaconState); err == nil {
		t.Fatalf("expected UpdateChainHead to fail if the block does not exist: %v", err)
	}
}

func TestUpdateChainHead_OK(t *testing.T) {
	db := setupDB(t)
	defer teardownDB(t, db)
	ctx := context.Background()

	genesisTime := uint64(time.Now().Unix())
	deposits, _ := testutil.SetupInitialDeposits(t, 10)
	err := db.InitializeState(context.Background(), genesisTime, deposits, &ethpb.Eth1Data{})
	if err != nil {
		t.Fatalf("failed to initialize state: %v", err)
	}

	block, err := db.ChainHead()
	if err != nil {
		t.Fatalf("failed to get genesis block: %v", err)
	}
	bHash, err := ssz.SigningRoot(block)
	if err != nil {
		t.Fatalf("failed to get hash of b: %v", err)
	}

	beaconState, err := db.HeadState(ctx)
	if err != nil {
		t.Fatalf("failed to get beacon state: %v", err)
	}

	block2 := &ethpb.BeaconBlock{
		Slot:       1,
		ParentRoot: bHash[:],
	}
	b2Hash, err := ssz.SigningRoot(block2)
	if err != nil {
		t.Fatalf("failed to hash b2: %v", err)
	}
	if err := db.SaveBlock(block2); err != nil {
		t.Fatalf("failed to save block: %v", err)
	}
	if err := db.UpdateChainHead(ctx, block2, beaconState); err != nil {
		t.Fatalf("failed to record the new head of the main chain: %v", err)
	}

	b2Prime, err := db.CanonicalBlockBySlot(ctx, 1)
	if err != nil {
		t.Fatalf("failed to retrieve slot 1: %v", err)
	}
	b2Sigma, err := db.ChainHead()
	if err != nil {
		t.Fatalf("failed to retrieve head: %v", err)
	}

	b2PrimeHash, err := ssz.SigningRoot(b2Prime)
	if err != nil {
		t.Fatalf("failed to hash b2Prime: %v", err)
	}
	b2SigmaHash, err := ssz.SigningRoot(b2Sigma)
	if err != nil {
		t.Fatalf("failed to hash b2Sigma: %v", err)
	}

	if b2Hash != b2PrimeHash {
		t.Fatalf("expected %x and %x to be equal", b2Hash, b2PrimeHash)
	}
	if b2Hash != b2SigmaHash {
		t.Fatalf("expected %x and %x to be equal", b2Hash, b2SigmaHash)
	}
}

func TestChainProgress_OK(t *testing.T) {
	db := setupDB(t)
	defer teardownDB(t, db)
	ctx := context.Background()

	genesisTime := uint64(time.Now().Unix())
	deposits, _ := testutil.SetupInitialDeposits(t, 100)
	err := db.InitializeState(context.Background(), genesisTime, deposits, &ethpb.Eth1Data{})
	if err != nil {
		t.Fatalf("failed to initialize state: %v", err)
	}

	beaconState, err := db.HeadState(ctx)
	if err != nil {
		t.Fatalf("Failed to get beacon state: %v", err)
	}
	cycleLength := params.BeaconConfig().SlotsPerEpoch

	block1 := &ethpb.BeaconBlock{Slot: 1}
	if err := db.SaveBlock(block1); err != nil {
		t.Fatalf("failed to save block: %v", err)
	}
	if err := db.UpdateChainHead(ctx, block1, beaconState); err != nil {
		t.Fatalf("failed to record the new head: %v", err)
	}
	heighestBlock, err := db.ChainHead()
	if err != nil {
		t.Fatalf("failed to get chain head: %v", err)
	}
	if heighestBlock.Slot != block1.Slot {
		t.Fatalf("expected height to equal %d, got %d", block1.Slot, heighestBlock.Slot)
	}

	block2 := &ethpb.BeaconBlock{Slot: cycleLength}
	if err := db.SaveBlock(block2); err != nil {
		t.Fatalf("failed to save block: %v", err)
	}
	if err := db.UpdateChainHead(ctx, block2, beaconState); err != nil {
		t.Fatalf("failed to record the new head: %v", err)
	}
	heighestBlock, err = db.ChainHead()
	if err != nil {
		t.Fatalf("failed to get block: %v", err)
	}
	if heighestBlock.Slot != block2.Slot {
		t.Fatalf("expected height to equal %d, got %d", block2.Slot, heighestBlock.Slot)
	}

	block3 := &ethpb.BeaconBlock{Slot: 3}
	if err := db.SaveBlock(block3); err != nil {
		t.Fatalf("failed to save block: %v", err)
	}
	if err := db.UpdateChainHead(ctx, block3, beaconState); err != nil {
		t.Fatalf("failed to update head: %v", err)
	}
	heighestBlock, err = db.ChainHead()
	if err != nil {
		t.Fatalf("failed to get chain head: %v", err)
	}
	if heighestBlock.Slot != block3.Slot {
		t.Fatalf("expected height to equal %d, got %d", block3.Slot, heighestBlock.Slot)
	}
}

func TestJustifiedBlock_NoneExists(t *testing.T) {
	db := setupDB(t)
	defer teardownDB(t, db)
	wanted := "no justified block saved"
	_, err := db.JustifiedBlock()
	if !strings.Contains(err.Error(), wanted) {
		t.Errorf("Expected: %s, received: %s", wanted, err.Error())
	}
}

func TestJustifiedBlock_CanSaveRetrieve(t *testing.T) {
	db := setupDB(t)
	defer teardownDB(t, db)

	blkSlot := uint64(10)
	block1 := &ethpb.BeaconBlock{
		Slot: blkSlot,
	}

	if err := db.SaveJustifiedBlock(block1); err != nil {
		t.Fatalf("could not save justified block: %v", err)
	}

	justifiedBlk, err := db.JustifiedBlock()
	if err != nil {
		t.Fatalf("could not get justified block: %v", err)
	}
	if justifiedBlk.Slot != blkSlot {
		t.Errorf("Saved block does not have the slot from which it was requested, wanted: %d, got: %d",
			blkSlot, justifiedBlk.Slot)
	}
}

func TestFinalizedBlock_NoneExists(t *testing.T) {
	db := setupDB(t)
	defer teardownDB(t, db)
	wanted := "no finalized block saved"
	_, err := db.FinalizedBlock()
	if !strings.Contains(err.Error(), wanted) {
		t.Errorf("Expected: %s, received: %s", wanted, err.Error())
	}
}

func TestFinalizedBlock_CanSaveRetrieve(t *testing.T) {
	db := setupDB(t)
	defer teardownDB(t, db)

	blkSlot := uint64(22)
	block1 := &ethpb.BeaconBlock{
		Slot: blkSlot,
	}

	if err := db.SaveFinalizedBlock(block1); err != nil {
		t.Fatalf("could not save finalized block: %v", err)
	}

	finalizedblk, err := db.FinalizedBlock()
	if err != nil {
		t.Fatalf("could not get finalized block: %v", err)
	}
	if finalizedblk.Slot != blkSlot {
		t.Errorf("Saved block does not have the slot from which it was requested, wanted: %d, got: %d",
			blkSlot, finalizedblk.Slot)
	}
}

func TestHasBlock_returnsTrue(t *testing.T) {
	db := setupDB(t)
	defer teardownDB(t, db)

	block := &ethpb.BeaconBlock{
		Slot: uint64(44),
	}

	root, err := ssz.SigningRoot(block)
	if err != nil {
		t.Fatal(err)
	}

	if err := db.SaveBlock(block); err != nil {
		t.Fatal(err)
	}

	if !db.HasBlock(root) {
		t.Fatal("db.HasBlock returned false for block just saved")
	}
}

func TestHighestBlockSlot_UpdatedOnSaveBlock(t *testing.T) {
	db := setupDB(t)
	defer teardownDB(t, db)

	block := &ethpb.BeaconBlock{
		Slot: 23,
	}

	if err := db.SaveBlock(block); err != nil {
		t.Fatal(err)
	}

	if db.HighestBlockSlot() != block.Slot {
		t.Errorf("Unexpected highest slot %d, wanted %d", db.HighestBlockSlot(), block.Slot)
	}

	block.Slot = 55
	if err := db.SaveBlock(block); err != nil {
		t.Fatal(err)
	}

	if db.HighestBlockSlot() != block.Slot {
		t.Errorf("Unexpected highest slot %d, wanted %d", db.HighestBlockSlot(), block.Slot)
	}
}

func TestClearBlockCache_OK(t *testing.T) {
	db := setupDB(t)
	defer teardownDB(t, db)

	block := &ethpb.BeaconBlock{Slot: 0}

	err := db.SaveBlock(block)
	if err != nil {
		t.Fatalf("save block failed: %v", err)
	}
	if len(db.blocks) != 1 {
		t.Error("incorrect block cache length")
	}
	db.ClearBlockCache()
	if len(db.blocks) != 0 {
		t.Error("incorrect block cache length")
	}
}
