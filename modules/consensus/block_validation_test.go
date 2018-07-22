package consensus

import (
	"testing"

	"github.com/HyperspaceApp/Hyperspace/types"
)

// mockMarshaler is a mock implementation of the encoding.GenericMarshaler
// interface that allows the client to pre-define the length of the marshaled
// data.
type mockMarshaler struct {
	marshalLength uint64
}

// Marshal marshals an object into an empty byte slice of marshalLength.
func (m mockMarshaler) Marshal(interface{}) []byte {
	return make([]byte, m.marshalLength)
}

// Unmarshal is not implemented.
func (m mockMarshaler) Unmarshal([]byte, interface{}) error {
	panic("not implemented")
}

// mockClock is a mock implementation of the types.Clock interface that allows
// the client to pre-define a return value for Now().
type mockClock struct {
	now types.Timestamp
}

// Now returns mockClock's pre-defined Timestamp.
func (c mockClock) Now() types.Timestamp {
	return c.now
}

var validateBlockTests = []struct {
	now            types.Timestamp
	minTimestamp   types.Timestamp
	blockTimestamp types.Timestamp
	blockSize      uint64
	errWant        error
	msg            string
}{
	{
		minTimestamp:   types.Timestamp(5),
		blockTimestamp: types.Timestamp(4),
		errWant:        errEarlyTimestamp,
		msg:            "ValidateBlock should reject blocks with timestamps that are too early",
	},
	{
		blockSize: types.BlockSizeLimit + 1,
		errWant:   errLargeBlock,
		msg:       "ValidateBlock should reject excessively large blocks",
	},
	{
		now:            types.Timestamp(50),
		blockTimestamp: types.Timestamp(50) + types.ExtremeFutureThreshold + 1,
		errWant:        errExtremeFutureTimestamp,
		msg:            "ValidateBlock should reject blocks timestamped in the extreme future",
	},
}

// TestUnitValidateBlock runs a series of unit tests for ValidateBlock.
func TestUnitValidateBlock(t *testing.T) {
	// TODO(mtlynch): Populate all parameters to ValidateBlock so that everything
	// is valid except for the attribute that causes validation to fail. (i.e.
	// don't assume an ordering to the implementation of the validation function).
	for _, tt := range validateBlockTests {
		b := types.Block{
			Timestamp: tt.blockTimestamp,
		}
		blockValidator := stdBlockValidator{
			marshaler: mockMarshaler{
				marshalLength: tt.blockSize,
			},
			clock: mockClock{
				now: tt.now,
			},
		}
		err := blockValidator.ValidateBlock(b, b.ID(), tt.minTimestamp, types.RootDepth, 0, nil)
		if err != tt.errWant {
			t.Errorf("%s: got %v, want %v", tt.msg, err, tt.errWant)
		}
	}
}

// TestCheckMinerPayouts probes the checkMinerPayouts function.
func TestCheckMinerPayouts(t *testing.T) {
	coinbase := types.CalculateCoinbase(1)
	b := types.Block{
		MinerPayouts: []types.SiacoinOutput{
			{Value: coinbase},
		},
	}
	if !checkMinerPayouts(b, 1) {
		t.Error("payouts evaluated incorrectly for the initial first block")
	}

	devFundSubsidy := coinbase.Div(types.DevFundDenom)
	minerSubsidy := coinbase.Sub(devFundSubsidy)
	b = types.Block{
		MinerPayouts: []types.SiacoinOutput{
			{Value: minerSubsidy},
			{Value: devFundSubsidy, UnlockHash: types.DevFundUnlockHash},
		},
	}
	if checkMinerPayouts(b, 1) {
		t.Error("payouts evaluated incorrectly for the first block when we added a dev fund payout")
	}

	coinbase = types.CalculateCoinbase(2)
	b = types.Block{
		MinerPayouts: []types.SiacoinOutput{
			{Value: coinbase},
		},
	}
	if !checkMinerPayouts(b, 2) {
		t.Error("payouts evaluated incorrectly for the second block")
	}

	b = types.Block{
		MinerPayouts: []types.SiacoinOutput{
			{Value: minerSubsidy},
			{Value: devFundSubsidy, UnlockHash: types.DevFundUnlockHash},
		},
	}
	if checkMinerPayouts(b, 2) {
		t.Error("payouts evaluated incorrectly for the second block when we added a 2nd payout")
	}

	// All remaining tests are done at height = 2.
	coinbase = types.CalculateCoinbase(3)
	devFundSubsidy = coinbase.Div(types.DevFundDenom)
	minerSubsidy = coinbase.Sub(devFundSubsidy)

	// Create a block with a single coinbase payout, and no dev fund payout.
	b = types.Block{
		MinerPayouts: []types.SiacoinOutput{
			{Value: coinbase},
		},
	}
	if checkMinerPayouts(b, 3) {
		t.Error("payouts evaluated incorrectly when there is a coinbase payout but not dev fund payout.")
	}

	// Create a block with a valid miner payout, and a dev fund payout with no unlock hash.
	b = types.Block{
		MinerPayouts: []types.SiacoinOutput{
			{Value: minerSubsidy},
			{Value: devFundSubsidy},
		},
	}
	if checkMinerPayouts(b, 3) {
		t.Error("payouts evaluated incorrectly when we are missing the dev fund unlock hash.")
	}

	// Create a block with a valid miner payout, and a dev fund payout with an incorrect unlock hash.
	b = types.Block{
		MinerPayouts: []types.SiacoinOutput{
			{Value: minerSubsidy},
			{Value: devFundSubsidy, UnlockHash: types.UnlockHash{0, 1}},
		},
	}
	if checkMinerPayouts(b, 3) {
		t.Error("payouts evaluated incorrectly when we have an incorrect dev fund unlock hash.")
	}

	// Create a block with a valid miner payout, but no dev fund payout.
	b = types.Block{
		MinerPayouts: []types.SiacoinOutput{
			{Value: minerSubsidy},
		},
	}
	if checkMinerPayouts(b, 3) {
		t.Error("payouts evaluated incorrectly when we are missing the dev fund payout but have a proper miner payout.")
	}

	// Create a block with a valid dev fund payout, but no miner payout.
	b = types.Block{
		MinerPayouts: []types.SiacoinOutput{
			{Value: devFundSubsidy, UnlockHash: types.DevFundUnlockHash},
		},
	}
	if checkMinerPayouts(b, 3) {
		t.Error("payouts evaluated incorrectly when we are missing the miner payout but have a proper dev fund payout.")
	}

	// Create a block with a valid miner payout and a valid dev fund payout.
	b = types.Block{
		MinerPayouts: []types.SiacoinOutput{
			{Value: minerSubsidy},
			{Value: devFundSubsidy, UnlockHash: types.DevFundUnlockHash},
		},
	}
	if !checkMinerPayouts(b, 3) {
		t.Error("payouts evaluated incorrectly when there are only two payouts.")
	}

	// Try a block with an incorrect payout.
	b = types.Block{
		MinerPayouts: []types.SiacoinOutput{
			{Value: coinbase.Sub(types.NewCurrency64(1))},
		},
	}
	if checkMinerPayouts(b, 3) {
		t.Error("payouts evaluated incorrectly when there is a too-small payout")
	}

	minerPayout := coinbase.Sub(devFundSubsidy).Sub(types.NewCurrency64(1))
	secondMinerPayout := types.NewCurrency64(1)
	// Try a block with 3 payouts.
	b = types.Block{
		MinerPayouts: []types.SiacoinOutput{
			{Value: minerPayout},
			{Value: secondMinerPayout},
			{Value: devFundSubsidy, UnlockHash: types.DevFundUnlockHash},
		},
	}
	if !checkMinerPayouts(b, 3) {
		t.Error("payouts evaluated incorrectly when there are 3 payouts")
	}

	// Try a block with 2 payouts that are too large.
	b = types.Block{
		MinerPayouts: []types.SiacoinOutput{
			{Value: coinbase},
			{Value: coinbase},
		},
	}
	if checkMinerPayouts(b, 3) {
		t.Error("payouts evaluated incorrectly when there are two large payouts")
	}

	// Create a block with an empty payout.
	b = types.Block{
		MinerPayouts: []types.SiacoinOutput{
			{Value: coinbase},
			{},
		},
	}
	if checkMinerPayouts(b, 3) {
		t.Error("payouts evaluated incorrectly when there is only one payout.")
	}
}

// TestCheckTarget probes the checkTarget function.
func TestCheckTarget(t *testing.T) {
	var b types.Block
	lowTarget := types.RootDepth
	highTarget := types.Target{}
	sameTarget := types.Target(b.ID())

	if !checkTarget(b, b.ID(), lowTarget) {
		t.Error("CheckTarget failed for a low target")
	}
	if checkTarget(b, b.ID(), highTarget) {
		t.Error("CheckTarget passed for a high target")
	}
	if !checkTarget(b, b.ID(), sameTarget) {
		t.Error("CheckTarget failed for a same target")
	}
}
