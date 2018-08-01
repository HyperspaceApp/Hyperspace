package types

// constants.go contains the Sia constants. Depending on which build tags are
// used, the constants will be initialized to different values.
//
// CONTRIBUTE: We don't have way to check that the non-test constants are all
// sane, plus we have no coverage for them.

import (
	"math"
	"math/big"
	"time"

	"github.com/HyperspaceApp/Hyperspace/build"
)

var (
	// BlockFrequency is the desired number of seconds that
	// should elapse, on average, between successive Blocks.
	BlockFrequency BlockHeight
	// BlockSizeLimit is the maximum size of a binary-encoded Block
	// that is permitted by the consensus rules.
	BlockSizeLimit = uint64(2e6)
	// DevFundDenom is the denominator of the block subsidy that goes
	// to support development of the network instead of the miners
	DevFundDenom = NewCurrency64(10)
	// DevFundUnlockHash is the unlock hash for the dev fund subsidy
	DevFundUnlockHash = UnlockHash{122, 187, 109, 95, 232, 79, 59, 94, 168, 154, 242, 9, 73, 45, 6, 21, 151, 78, 195, 98, 197, 65, 115, 155, 229, 181, 208, 12, 31, 116, 69, 196}

	// EndOfTime is value to be used when a date in the future is needed for
	// validation
	EndOfTime = time.Unix(0, math.MaxInt64)

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
	// GenesisAllocation is the output creating all the initial coins allocated
	// in the genesis block
	GenesisAllocation []SiacoinOutput
	// GenesisAirdropAllocation is the output creating the initial coins allocated
	// for the airdrop at network launch
	GenesisAirdropAllocation []SiacoinOutput
	// GenesisDeveloperAllocation is the output creating the initial coins allocated
	// for the developer airdrop at network launch
	GenesisDeveloperAllocation []SiacoinOutput
	// GenesisContributorAllocation is the output creating the initial coins allocated
	// for the developer airdrop at network launch
	GenesisContributorAllocation []SiacoinOutput
	// GenesisPoolAllocation is the output creating the initial coins allocated
	// for the developer airdrop at network launch
	GenesisPoolAllocation []SiacoinOutput
	// GenesisBlock is the first block of the block chain
	GenesisBlock Block

	// GenesisID is used in many places. Calculating it once saves lots of
	// redundant computation.
	GenesisID BlockID

	// GenesisTimestamp is the timestamp when genesis block was mined
	GenesisTimestamp Timestamp
	// FirstCoinbase is the coinbase reward of the Genesis block.
	FirstCoinbase = uint64(24e8)
	// SecondCoinbase is the coinbase reward of the 2nd block.
	SecondCoinbase = uint64(6e8)
	// InitialCoinbase is the coinbase reward of the first block following the
	// initial 2 blocks.
	InitialCoinbase = uint64(60e3)
	// AirdropValue is the total amount of coins generated in the genesis block
	// for the airdrop.
	AirdropValue = NewCurrency64(35373763032).Mul(SiacoinPrecision).Div(NewCurrency64(10))
	// SingleDeveloperAirdropValue is the amount of coins generated in the genesis
	// block for each developer's airdrop
	SingleDeveloperAirdropValue = NewCurrency64(54e6).Mul(SiacoinPrecision)
	// NumDevelopers is the number of developers who split the DeveloperAirdrop
	NumDevelopers = uint64(10)
	// DeveloperAirdropValue is the total amount of coins generated in the genesis
	// block for the developers airdrops
	DeveloperAirdropValue = SingleDeveloperAirdropValue.Mul(NewCurrency64(NumDevelopers))
	// SingleContributorAirdropValue is the amount of coins generated in the genesis
	// block for each contributor's airdrop
	SingleContributorAirdropValue = NewCurrency64(10e6).Mul(SiacoinPrecision)
	// NumContributors is the number of contributors who split the ContributorAirdrop
	NumContributors = uint64(6)
	// ContributorAirdropValue is the total amount of coins generated in the genesis
	// block for the contributor airdrop
	ContributorAirdropValue = SingleContributorAirdropValue.Mul(NewCurrency64(NumContributors))
	// SinglePoolAirdropValue is the amount of coins generated in the genesis
	// block for each pool's airdrop
	SinglePoolAirdropValue = NewCurrency64(2 * InitialCoinbase).Mul(SiacoinPrecision)
	// NumPools is the number of developers who split the DeveloperAirdrop
	NumPools = uint64(7)
	// PoolAirdropValue is the total amount of coins generated in the genesis
	// block for the pool airdrop
	PoolAirdropValue = SinglePoolAirdropValue.Mul(NewCurrency64(NumPools))
	// MaturityDelay specifies the number of blocks that a maturity-required output
	// is required to be on hold before it can be spent on the blockchain.
	// Outputs are maturity-required if they are highly likely to be altered or
	// invalidated in the event of a small reorg. One example is the block reward,
	// as a small reorg may invalidate the block reward. File contract payouts also
	// are subject to a maturity delay.
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

	// Oak constants. Oak is the name of the difficulty algorithm for Hyperspace.

	// OakDecayDenom is the denominator for how much the total timestamp is decayed
	// each step.
	OakDecayDenom int64
	// OakDecayNum is the numerator for how much the total timestamp is decayed each
	// step.
	OakDecayNum int64
	// OakTxnSizeLimit is the maximum size allowed for a transaction.
	OakTxnSizeLimit = uint64(64e3) // 64 KB
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

var (
	// TaxHardforkHeight is the height at which the tax hardfork occurred.
	TaxHardforkHeight = build.Select(build.Var{
		Dev:      BlockHeight(10),
		Standard: BlockHeight(21e3),
		Testing:  BlockHeight(10),
	}).(BlockHeight)
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
		GenesisTimestamp = Timestamp(1532510500) // Change as necessary.
		RootTarget = Target{0, 0, 2}             // Standard developer CPUs will be able to mine blocks with the race library activated.

		TargetWindow = 20                              // Difficulty is adjusted based on prior 20 blocks.
		MaxTargetAdjustmentUp = big.NewRat(120, 100)   // Difficulty adjusts quickly.
		MaxTargetAdjustmentDown = big.NewRat(100, 120) // Difficulty adjusts quickly.
		FutureThreshold = 2 * 60                       // 2 minutes.
		ExtremeFutureThreshold = 4 * 60                // 4 minutes.

		MinimumCoinbase = 6000

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

		MinimumCoinbase = 59990 // Minimum coinbase is hit after 10 blocks to make testing minimum-coinbase code easier.

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

		// The genesis timestamp is set to July 25th, which is when the
		// network went live.
		GenesisTimestamp = Timestamp(1532510521) // July 25, 2018 9:22:00 GMT

		// The RootTarget was set such that the developers could reasonable
		// premine 100 blocks in a day. It was known to the developers at launch
		// this this was at least one and perhaps two orders of magnitude too
		// small.
		RootTarget = Target{0, 0, 0, 4}

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

		// The minimum coinbase is set to 6,000. Because the coinbase
		// decreases by 1 every time, it means that Sia's coinbase will have an
		// increasingly potent dropoff for about 5 years, until inflation more
		// or less permanently settles around 2%.
		MinimumCoinbase = 6000

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

	// Create the initial tokens for the airdrop
	GenesisAirdropAllocation = []SiacoinOutput{
		{
			Value:      AirdropValue,
			UnlockHash: UnlockHash{150, 207, 110, 1, 194, 164, 204, 225, 187, 15, 120, 146, 252, 172, 94, 0, 0, 196, 135, 188, 142, 90, 195, 136, 222, 112, 8, 160, 222, 92, 241, 22},
		},
	}

	GenesisDeveloperAllocation = []SiacoinOutput{
		{
			Value:      SingleDeveloperAirdropValue,
			UnlockHash: UnlockHash{189, 162, 236, 165, 81, 140, 186, 212, 48, 188, 83, 121, 5, 132, 178, 40, 182, 183, 121, 42, 232, 252, 32, 211, 239, 245, 49, 174, 178, 182, 45, 64},
		},

		{
			Value:      SingleDeveloperAirdropValue,
			UnlockHash: UnlockHash{147, 113, 100, 98, 203, 109, 75, 159, 145, 249, 149, 185, 68, 254, 25, 106, 19, 10, 210, 148, 165, 83, 4, 114, 63, 240, 167, 66, 185, 7, 161, 122},
		},

		{
			Value:      SingleDeveloperAirdropValue,
			UnlockHash: UnlockHash{237, 53, 147, 42, 115, 194, 55, 27, 39, 197, 178, 204, 174, 50, 169, 174, 117, 188, 39, 34, 77, 176, 175, 169, 53, 97, 233, 234, 232, 194, 212, 40},
		},

		{
			Value:      SingleDeveloperAirdropValue,
			UnlockHash: UnlockHash{128, 87, 40, 7, 165, 220, 242, 0, 88, 204, 84, 174, 113, 109, 17, 199, 27, 36, 120, 116, 207, 252, 131, 129, 6, 55, 69, 68, 32, 172, 246, 152},
		},

		{
			Value:      SingleDeveloperAirdropValue,
			UnlockHash: UnlockHash{107, 179, 160, 141, 208, 78, 91, 20, 168, 241, 183, 38, 166, 48, 175, 254, 234, 78, 248, 87, 161, 154, 121, 176, 224, 129, 67, 138, 92, 77, 11, 113},
		},

		{
			Value:      SingleDeveloperAirdropValue,
			UnlockHash: UnlockHash{174, 169, 126, 149, 129, 194, 124, 81, 190, 76, 241, 100, 247, 74, 234, 79, 205, 125, 44, 30, 170, 152, 158, 17, 103, 130, 241, 67, 50, 147, 16, 92},
		},

		{
			Value:      SingleDeveloperAirdropValue,
			UnlockHash: UnlockHash{10, 164, 223, 171, 5, 19, 75, 231, 52, 57, 148, 215, 128, 12, 87, 68, 37, 165, 125, 41, 90, 248, 91, 181, 15, 4, 181, 64, 205, 41, 203, 208},
		},

		{
			Value:      SingleDeveloperAirdropValue,
			UnlockHash: UnlockHash{196, 124, 178, 27, 31, 175, 132, 82, 177, 13, 211, 131, 242, 162, 193, 152, 231, 146, 81, 5, 52, 46, 69, 7, 61, 124, 218, 218, 9, 46, 27, 196},
		},

		{
			Value:      SingleDeveloperAirdropValue,
			UnlockHash: UnlockHash{56, 246, 104, 35, 95, 33, 79, 205, 80, 20, 175, 191, 140, 98, 6, 167, 245, 226, 190, 158, 208, 108, 52, 222, 224, 10, 168, 50, 29, 67, 76, 156},
		},

		{
			Value:      SingleDeveloperAirdropValue,
			UnlockHash: UnlockHash{124, 73, 85, 177, 28, 192, 69, 85, 222, 166, 190, 24, 107, 109, 143, 105, 46, 218, 123, 159, 215, 122, 11, 35, 47, 183, 94, 236, 190, 21, 33, 79},
		},
	}

	GenesisContributorAllocation = []SiacoinOutput{
		{
			Value:      SingleContributorAirdropValue,
			UnlockHash: UnlockHash{218, 158, 104, 142, 55, 232, 182, 179, 46, 41, 5, 94, 231, 83, 162, 228, 36, 249, 123, 177, 99, 246, 21, 122, 86, 137, 23, 231, 102, 36, 186, 105},
		},

		{
			Value:      SingleContributorAirdropValue,
			UnlockHash: UnlockHash{125, 212, 14, 206, 111, 167, 163, 202, 124, 67, 124, 200, 145, 192, 149, 225, 161, 200, 238, 57, 224, 25, 210, 94, 216, 201, 96, 39, 236, 74, 15, 147},
		},

		{
			Value:      SingleContributorAirdropValue,
			UnlockHash: UnlockHash{60, 91, 48, 246, 158, 21, 87, 155, 51, 110, 225, 41, 235, 215, 13, 108, 165, 158, 35, 223, 253, 221, 14, 39, 148, 226, 181, 6, 166, 2, 239, 34},
		},

		{
			Value:      SingleContributorAirdropValue,
			UnlockHash: UnlockHash{165, 182, 125, 195, 81, 68, 196, 134, 77, 61, 98, 223, 84, 220, 167, 31, 135, 201, 139, 173, 187, 229, 243, 79, 233, 103, 108, 102, 114, 232, 59, 73},
		},

		{
			Value:      SingleContributorAirdropValue,
			UnlockHash: UnlockHash{196, 99, 157, 119, 181, 114, 208, 148, 146, 198, 13, 250, 104, 67, 40, 161, 22, 158, 132, 70, 224, 5, 83, 54, 3, 51, 80, 53, 165, 218, 54, 14},
		},

		{
			Value:      SingleContributorAirdropValue,
			UnlockHash: UnlockHash{119, 226, 125, 129, 89, 187, 96, 150, 149, 93, 165, 168, 117, 112, 28, 60, 15, 73, 115, 64, 29, 20, 22, 222, 230, 176, 172, 51, 109, 191, 68, 49},
		},
	}

	GenesisPoolAllocation = []SiacoinOutput{
		{
			Value:      SinglePoolAirdropValue,
			UnlockHash: UnlockHash{205, 253, 28, 199, 100, 195, 88, 56, 142, 135, 0, 151, 162, 225, 185, 111, 136, 112, 137, 89, 35, 108, 174, 91, 21, 160, 141, 217, 63, 139, 148, 94},
		},

		{
			Value:      SinglePoolAirdropValue,
			UnlockHash: UnlockHash{173, 49, 115, 131, 130, 63, 132, 96, 148, 178, 201, 241, 144, 68, 203, 225, 97, 69, 95, 192, 34, 4, 146, 82, 32, 208, 139, 10, 223, 234, 239, 52},
		},

		{
			Value:      SinglePoolAirdropValue,
			UnlockHash: UnlockHash{170, 204, 225, 98, 230, 29, 94, 196, 22, 232, 239, 214, 129, 134, 115, 35, 189, 203, 64, 195, 144, 113, 84, 130, 203, 211, 237, 113, 20, 237, 109, 251},
		},

		{
			Value:      SinglePoolAirdropValue,
			UnlockHash: UnlockHash{36, 172, 106, 133, 70, 60, 73, 79, 77, 117, 237, 26, 103, 207, 192, 207, 153, 51, 16, 63, 212, 144, 223, 254, 83, 249, 125, 245, 177, 209, 191, 199},
		},

		{
			Value:      SinglePoolAirdropValue,
			UnlockHash: UnlockHash{95, 204, 254, 87, 39, 195, 235, 56, 210, 6, 143, 179, 251, 227, 224, 14, 114, 90, 223, 159, 209, 255, 157, 121, 104, 213, 229, 81, 215, 221, 166, 147},
		},

		{
			Value:      SinglePoolAirdropValue,
			UnlockHash: UnlockHash{13, 237, 150, 118, 197, 194, 35, 81, 161, 198, 233, 154, 25, 245, 112, 205, 79, 30, 0, 176, 20, 6, 66, 35, 17, 170, 24, 183, 76, 183, 201, 180},
		},

		{
			Value:      SinglePoolAirdropValue,
			UnlockHash: UnlockHash{255, 53, 211, 173, 62, 94, 73, 87, 119, 12, 48, 2, 28, 39, 68, 145, 146, 143, 157, 169, 28, 61, 61, 106, 112, 15, 235, 164, 187, 58, 220, 113},
		},
	}
	GenesisAllocation = append(GenesisAllocation, GenesisAirdropAllocation...)
	GenesisAllocation = append(GenesisAllocation, GenesisDeveloperAllocation...)
	GenesisAllocation = append(GenesisAllocation, GenesisContributorAllocation...)
	GenesisAllocation = append(GenesisAllocation, GenesisPoolAllocation...)
	// Create the genesis block.
	GenesisBlock = Block{
		Timestamp: GenesisTimestamp,
		Transactions: []Transaction{
			{SiacoinOutputs: GenesisAllocation},
		},
	}
	// Calculate the genesis ID.
	GenesisID = GenesisBlock.ID()
}
