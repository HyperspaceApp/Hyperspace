package types

// constants.go contains the Sia constants. Depending on which build tags are
// used, the constants will be initialized to different values.
//
// CONTRIBUTE: We don't have way to check that the non-test constants are all
// sane, plus we have no coverage for them.

import (
	"math/big"

	"github.com/HyperspaceApp/Hyperspace/build"
)

var (
	// BlockFrequency is the desired number of seconds that
	// should elapse, on average, between successive Blocks.
	BlockFrequency BlockHeight
	// BlockSizeLimit is the maximum size of a binary-encoded Block
	// that is permitted by the consensus rules.
	BlockSizeLimit = uint64(2e6)
	// ExtremeFutureThreshold is a temporal limit beyond which Blocks are
	// discarded by the consensus rules. When incoming Blocks are processed, their
	// Timestamp is allowed to exceed the processor's current time by a small amount.
	// But if the Timestamp is further into the future than ExtremeFutureThreshold,
	// the Block is immediately discarded.
	ExtremeFutureThreshold Timestamp
	// FutureThreshold is a temporal limit beyond which Blocks are
	// discarded by the consensus rules. When incoming Blocks are processed, their
	// Timestamp is allowed to exceed the processor's current time by no more than
	// FutureThreshold. If the excess duration is larger than FutureThreshold, but
	// smaller than ExtremeFutureThreshold, the Block may be held in memory until
	// the Block's Timestamp exceeds the current time by less than FutureThreshold.
	FutureThreshold Timestamp
	// GenesisBlock is the first block of the block chain
	GenesisBlock Block

	// GenesisID is used in many places. Calculating it once saves lots of
	// redundant computation.
	GenesisID BlockID

	// GenesisTimestamp is the timestamp when genesis block was mined
	GenesisTimestamp Timestamp
	// InitialCoinbase is the coinbase reward of the Genesis block.
	InitialCoinbase = uint64(300e3)
	// MaturityDelay specifies the number of blocks that a maturity-required output
	// is required to be on hold before it can be spent on the blockchain.
	// Outputs are maturity-required if they are highly likely to be altered or
	// invalidated in the event of a small reorg. One example is the block reward,
	// as a small reorg may invalidate the block reward. Another example is a siafund
	// payout, as a tiny reorg may change the value of the payout, and thus invalidate
	// any transactions spending the payout. File contract payouts also are subject to
	// a maturity delay.
	MaturityDelay BlockHeight
	// MaxTargetAdjustmentDown restrict how much the block difficulty is allowed to
	// change in a single step, which is important to limit the effect of difficulty
	// raising and lowering attacks.
	MaxTargetAdjustmentDown *big.Rat
	// MaxTargetAdjustmentUp restrict how much the block difficulty is allowed to
	// change in a single step, which is important to limit the effect of difficulty
	// raising and lowering attacks.
	MaxTargetAdjustmentUp *big.Rat
	// MedianTimestampWindow tells us how many blocks to look back when calculating
	// the median timestamp over the previous n blocks. The timestamp of a block is
	// not allowed to be less than or equal to the median timestamp of the previous n
	// blocks, where for Sia this number is typically 11.
	MedianTimestampWindow = uint64(11)
	// MinimumCoinbase is the minimum coinbase reward for a block.
	// The coinbase decreases in each block after the Genesis block,
	// but it will not decrease past MinimumCoinbase.
	MinimumCoinbase uint64

	// Oak hardfork constants. Oak is the name of the difficulty algorithm for
	// Sia following a hardfork at block 135e3.

	// OakDecayDenom is the denominator for how much the total timestamp is decayed
	// each step.
	OakDecayDenom int64
	// OakDecayNum is the numerator for how much the total timestamp is decayed each
	// step.
	OakDecayNum int64
	// OakHardforkBlock is the height at which the hardfork to switch to the oak
	// difficulty adjustment algorithm is triggered.
	OakHardforkBlock BlockHeight
	// OakHardforkFixBlock is the height at which the hardfork to switch from the broken
	// oak difficulty adjustment algorithm to the fixed oak difficulty adjustment
	// algorithm is triggered.
	OakHardforkFixBlock BlockHeight
	// OakHardforkTxnSizeLimit is the maximum size allowed for a transaction, a change
	// which I believe was implemented simultaneously with the oak hardfork.
	OakHardforkTxnSizeLimit = uint64(64e3) // 64 KB
	// OakMaxBlockShift is the maximum number of seconds that the oak algorithm will shift
	// the difficulty.
	OakMaxBlockShift int64
	// OakMaxDrop is the drop is the maximum amount that the difficulty will drop each block.
	OakMaxDrop *big.Rat
	// OakMaxRise is the maximum amount that the difficulty will rise each block.
	OakMaxRise *big.Rat

	// RootDepth is the cumulative target of all blocks. The root depth is essentially
	// the maximum possible target, there have been no blocks yet, so there is no
	// cumulated difficulty yet.
	RootDepth = Target{255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255}
	// RootTarget is the target for the genesis block - basically how much work needs
	// to be done in order to mine the first block. The difficulty adjustment algorithm
	// takes over from there.
	RootTarget Target
	// SiacoinPrecision is the number of base units in a siacoin. The Sia network has a very
	// large number of base units. We call 10^24 of these a siacoin.
	//
	// The base unit for Bitcoin is called a satoshi. We call 10^8 satoshis a bitcoin,
	// even though the code itself only ever works with satoshis.
	SiacoinPrecision = NewCurrency(new(big.Int).Exp(big.NewInt(10), big.NewInt(24), nil))
	// TargetWindow is the number of blocks to look backwards when determining how much
	// time has passed vs. how many blocks have been created. It's only used in the old,
	// broken difficulty adjustment algorithm.
	TargetWindow BlockHeight
)

// init checks which build constant is in place and initializes the variables
// accordingly.
func init() {
	if build.Release == "dev" {
		// 'dev' settings are for small developer testnets, usually on the same
		// computer. Settings are slow enough that a small team of developers
		// can coordinate their actions over a the developer testnets, but fast
		// enough that there isn't much time wasted on waiting for things to
		// happen.
		BlockFrequency = 120                     // 12 seconds: slow enough for developers to see ~each block, fast enough that blocks don't waste time.
		MaturityDelay = 100                      // 60 seconds before a delayed output matures.
		GenesisTimestamp = Timestamp(1528293910) // Change as necessary.
		RootTarget = Target{0, 0, 2}             // Standard developer CPUs will be able to mine blocks with the race library activated.

		TargetWindow = 20                              // Difficulty is adjusted based on prior 20 blocks.
		MaxTargetAdjustmentUp = big.NewRat(120, 100)   // Difficulty adjusts quickly.
		MaxTargetAdjustmentDown = big.NewRat(100, 120) // Difficulty adjusts quickly.
		FutureThreshold = 2 * 60                       // 2 minutes.
		ExtremeFutureThreshold = 4 * 60                // 4 minutes.

		MinimumCoinbase = 30e3

		OakHardforkBlock = 100
		OakHardforkFixBlock = 105
		OakDecayNum = 985
		OakDecayDenom = 1000
		OakMaxBlockShift = 3
		OakMaxRise = big.NewRat(102, 100)
		OakMaxDrop = big.NewRat(100, 102)

	} else if build.Release == "testing" {
		// 'testing' settings are for automatic testing, and create much faster
		// environments than a human can interact with.
		BlockFrequency = 1 // As fast as possible
		MaturityDelay = 3
		GenesisTimestamp = CurrentTimestamp() - 1e6
		RootTarget = Target{128} // Takes an expected 2 hashes; very fast for testing but still probes 'bad hash' code.

		// A restrictive difficulty clamp prevents the difficulty from climbing
		// during testing, as the resolution on the difficulty adjustment is
		// only 1 second and testing mining should be happening substantially
		// faster than that.
		TargetWindow = 200
		MaxTargetAdjustmentUp = big.NewRat(10001, 10000)
		MaxTargetAdjustmentDown = big.NewRat(9999, 10000)
		FutureThreshold = 3        // 3 seconds
		ExtremeFutureThreshold = 6 // 6 seconds

		MinimumCoinbase = 299990 // Minimum coinbase is hit after 10 blocks to make testing minimum-coinbase code easier.

		// Do not let the difficulty change rapidly - blocks will be getting
		// mined far faster than the difficulty can adjust to.
		OakHardforkBlock = 20
		OakHardforkFixBlock = 23
		OakDecayNum = 9999
		OakDecayDenom = 10e3
		OakMaxBlockShift = 3
		OakMaxRise = big.NewRat(10001, 10e3)
		OakMaxDrop = big.NewRat(10e3, 10001)

	} else if build.Release == "standard" {
		// 'standard' settings are for the full network. They are slow enough
		// that the network is secure in a real-world byzantine environment.

		// A block time of 1 block per 10 minutes is chosen to follow Bitcoin's
		// example. The security lost by lowering the block time is not
		// insignificant, and the convenience gained by lowering the blocktime
		// even down to 90 seconds is not significant. I do feel that 10
		// minutes could even be too short, but it has worked well for Bitcoin.
		BlockFrequency = 600

		// Payouts take 1 day to mature. This is to prevent a class of double
		// spending attacks parties unintentionally spend coins that will stop
		// existing after a blockchain reorganization. There are multiple
		// classes of payouts in Sia that depend on a previous block - if that
		// block changes, then the output changes and the previously existing
		// output ceases to exist. This delay stops both unintentional double
		// spending and stops a small set of long-range mining attacks.
		MaturityDelay = 144

		// The genesis timestamp is set to June 6th, because that is when the
		// 100-block developer premine started. The trailing zeroes are a
		// bonus, and make the timestamp easier to memorize.
		GenesisTimestamp = Timestamp(1433600000) // June 6th, 2015 @ 2:13pm UTC.

		// The RootTarget was set such that the developers could reasonable
		// premine 100 blocks in a day. It was known to the developers at launch
		// this this was at least one and perhaps two orders of magnitude too
		// small.
		RootTarget = Target{0, 0, 0, 0, 32}

		// When the difficulty is adjusted, it is adjusted by looking at the
		// timestamp of the 1000th previous block. This minimizes the abilities
		// of miners to attack the network using rogue timestamps.
		TargetWindow = 1e3

		// The difficulty adjustment is clamped to 2.5x every 500 blocks. This
		// corresponds to 6.25x every 2 weeks, which can be compared to
		// Bitcoin's clamp of 4x every 2 weeks. The difficulty clamp is
		// primarily to stop difficulty raising attacks. Sia's safety margin is
		// similar to Bitcoin's despite the looser clamp because Sia's
		// difficulty is adjusted four times as often. This does result in
		// greater difficulty oscillation, a tradeoff that was chosen to be
		// acceptable due to Sia's more vulnerable position as an altcoin.
		MaxTargetAdjustmentUp = big.NewRat(25, 10)
		MaxTargetAdjustmentDown = big.NewRat(10, 25)

		// Blocks will not be accepted if their timestamp is more than 3 hours
		// into the future, but will be accepted as soon as they are no longer
		// 3 hours into the future. Blocks that are greater than 5 hours into
		// the future are rejected outright, as it is assumed that by the time
		// 2 hours have passed, those blocks will no longer be on the longest
		// chain. Blocks cannot be kept forever because this opens a DoS
		// vector.
		FutureThreshold = 3 * 60 * 60        // 3 hours.
		ExtremeFutureThreshold = 5 * 60 * 60 // 5 hours.

		// The minimum coinbase is set to 30,000. Because the coinbase
		// decreases by 1 every time, it means that Sia's coinbase will have an
		// increasingly potent dropoff for about 5 years, until inflation more
		// or less permanently settles around 2%.
		MinimumCoinbase = 30e3

		// The oak difficulty adjustment hardfork is set to trigger at block
		// 135,000, which is just under 6 months after the hardfork was first
		// released as beta software to the network. This hopefully gives
		// everyone plenty of time to upgrade and adopt the hardfork, while also
		// being earlier than the most optimistic shipping dates for the miners
		// that would otherwise be very disruptive to the network.
		//
		// There was a bug in the original Oak hardfork that had to be quickly
		// followed up with another fix. The height of that fix is the
		// OakHardforkFixBlock.
		OakHardforkBlock = 135e3
		OakHardforkFixBlock = 139e3

		// The decay is kept at 995/1000, or a decay of about 0.5% each block.
		// This puts the halflife of a block's relevance at about 1 day. This
		// allows the difficulty to adjust rapidly if the hashrate is adjusting
		// rapidly, while still keeping a relatively strong insulation against
		// random variance.
		OakDecayNum = 995
		OakDecayDenom = 1e3

		// The block shift determines the most that the difficulty adjustment
		// algorithm is allowed to shift the target block time. With a block
		// frequency of 600 seconds, the min target block time is 200 seconds,
		// and the max target block time is 1800 seconds.
		OakMaxBlockShift = 3

		// The max rise and max drop for the difficulty is kept at 0.4% per
		// block, which means that in 1008 blocks the difficulty can move a
		// maximum of about 55x. This is significant, and means that dramatic
		// hashrate changes can be responded to quickly, while still forcing an
		// attacker to do a significant amount of work in order to execute a
		// difficulty raising attack, and minimizing the chance that an attacker
		// can get lucky and fake a ton of work.
		OakMaxRise = big.NewRat(1004, 1e3)
		OakMaxDrop = big.NewRat(1e3, 1004)

	}

	// Create the genesis block.
	GenesisBlock = Block{
		Timestamp:    GenesisTimestamp,
		Transactions: []Transaction{},
	}
	// Calculate the genesis ID.
	GenesisID = GenesisBlock.ID()
}
