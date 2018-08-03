package wallet

import (
	"runtime"
	"sync"

	"github.com/HyperspaceApp/Hyperspace/crypto"
	"github.com/HyperspaceApp/Hyperspace/encoding"
	"github.com/HyperspaceApp/Hyperspace/modules"
	"github.com/HyperspaceApp/Hyperspace/types"
	"gitlab.com/NebulousLabs/errors"
	"gitlab.com/NebulousLabs/fastrand"
	"github.com/coreos/bbolt"
)

var (
	errKnownSeed = errors.New("seed is already known")
)

type (
	// uniqueID is a unique id randomly generated and put at the front of every
	// persistence object. It is used to make sure that a different encryption
	// key can be used for every persistence object.
	uniqueID [crypto.EntropySize]byte

	// seedFile stores an encrypted wallet seed on disk.
	seedFile struct {
		UID                    uniqueID
		EncryptionVerification crypto.Ciphertext
		Seed                   crypto.Ciphertext
	}
)

// generateSpendableKey creates the keys and unlock conditions for seed at a
// given index.
func generateSpendableKey(seed modules.Seed, index uint64) spendableKey {
	sk, pk := crypto.GenerateKeyPairDeterministic(crypto.HashAll(seed, index))
	return spendableKey{
		UnlockConditions: types.UnlockConditions{
			PublicKeys:         []types.SiaPublicKey{types.Ed25519PublicKey(pk)},
			SignaturesRequired: 1,
		},
		SecretKeys: []crypto.SecretKey{sk},
	}
}

// generateKeys generates n keys from seed, starting from index start.
func generateKeys(seed modules.Seed, start, n uint64) []spendableKey {
	// generate in parallel, one goroutine per core.
	keys := make([]spendableKey, n)
	var wg sync.WaitGroup
	wg.Add(runtime.NumCPU())
	for cpu := 0; cpu < runtime.NumCPU(); cpu++ {
		go func(offset uint64) {
			defer wg.Done()
			for i := offset; i < n; i += uint64(runtime.NumCPU()) {
				// NOTE: don't bother trying to optimize generateSpendableKey;
				// profiling shows that ed25519 key generation consumes far
				// more CPU time than encoding or hashing.
				keys[i] = generateSpendableKey(seed, start+i)
			}
		}(uint64(cpu))
	}
	wg.Wait()
	return keys
}

// createSeedFile creates and encrypts a seedFile.
func createSeedFile(masterKey crypto.TwofishKey, seed modules.Seed) seedFile {
	var sf seedFile
	fastrand.Read(sf.UID[:])
	sek := uidEncryptionKey(masterKey, sf.UID)
	sf.EncryptionVerification = sek.EncryptBytes(verificationPlaintext)
	sf.Seed = sek.EncryptBytes(seed[:])
	return sf
}

// decryptSeedFile decrypts a seed file using the encryption key.
func decryptSeedFile(masterKey crypto.TwofishKey, sf seedFile) (seed modules.Seed, err error) {
	// Verify that the provided master key is the correct key.
	decryptionKey := uidEncryptionKey(masterKey, sf.UID)
	err = verifyEncryption(decryptionKey, sf.EncryptionVerification)
	if err != nil {
		return modules.Seed{}, err
	}

	// Decrypt and return the seed.
	plainSeed, err := decryptionKey.DecryptBytes(sf.Seed)
	if err != nil {
		return modules.Seed{}, err
	}
	copy(seed[:], plainSeed)
	return seed, nil
}

// regenerateLookahead creates future keys up to a maximum of maxKeys keys
func (w *Wallet) regenerateLookahead(start uint64) {
	// Check how many keys need to be generated
	maxKeys := maxLookahead(start)
	existingKeys := uint64(len(w.lookahead))

	for i, k := range generateKeys(w.primarySeed, start+existingKeys, maxKeys-existingKeys) {
		w.lookahead[k.UnlockConditions.UnlockHash()] = start + existingKeys + uint64(i)
	}
}

// integrateSeed generates n spendableKeys from the seed and loads them into
// the wallet.
func (w *Wallet) integrateSeed(seed modules.Seed, n uint64) {
	for _, sk := range generateKeys(seed, 0, n) {
		w.keys[sk.UnlockConditions.UnlockHash()] = sk
	}
}

// nextPrimarySeedAddress fetches the next n addresses from the primary seed.
func (w *Wallet) nextPrimarySeedAddresses(tx *bolt.Tx, n uint64) ([]types.UnlockConditions, error) {
	// Check that the wallet has been unlocked.
	if !w.unlocked {
		return []types.UnlockConditions{}, modules.ErrLockedWallet
	}

	// Fetch and increment the seed progress.
	progress, err := dbGetPrimarySeedProgress(tx)
	if err != nil {
		return []types.UnlockConditions{}, err
	}
	if err = dbPutPrimarySeedProgress(tx, progress+n); err != nil {
		return []types.UnlockConditions{}, err
	}
	// Integrate the next keys into the wallet, and return the unlock
	// conditions. Also remove new keys from the future keys and update them
	// according to new progress
	spendableKeys := generateKeys(w.primarySeed, progress, n)
	ucs := make([]types.UnlockConditions, 0, len(spendableKeys))
	for _, spendableKey := range spendableKeys {
		w.keys[spendableKey.UnlockConditions.UnlockHash()] = spendableKey
		delete(w.lookahead, spendableKey.UnlockConditions.UnlockHash())
		ucs = append(ucs, spendableKey.UnlockConditions)
	}
	w.regenerateLookahead(progress + n)

	return ucs, nil
}

// nextPrimarySeedAddress fetches the next address from the primary seed.
func (w *Wallet) nextPrimarySeedAddress(tx *bolt.Tx) (types.UnlockConditions, error) {
	ucs, err := w.nextPrimarySeedAddresses(tx, 1)
	if err != nil {
		return types.UnlockConditions{}, err
	}
	return ucs[0], nil
}

// AllSeeds returns a list of all seeds known to and used by the wallet.
func (w *Wallet) AllSeeds() ([]modules.Seed, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if !w.unlocked {
		return nil, modules.ErrLockedWallet
	}
	return append([]modules.Seed{w.primarySeed}, w.seeds...), nil
}

// PrimarySeed returns the decrypted primary seed of the wallet, as well as
// the number of addresses that the seed can be safely used to generate.
func (w *Wallet) PrimarySeed() (modules.Seed, uint64, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if !w.unlocked {
		return modules.Seed{}, 0, modules.ErrLockedWallet
	}
	progress, err := dbGetPrimarySeedProgress(w.dbTx)
	if err != nil {
		return modules.Seed{}, 0, err
	}

	// addresses remaining is maxScanKeys-progress; generating more keys than
	// that risks not being able to recover them when using SweepSeed or
	// InitFromSeed.
	remaining := maxScanKeys - progress
	if progress > maxScanKeys {
		remaining = 0
	}
	return w.primarySeed, remaining, nil
}

// NextAddresses returns n unlock hashes that are ready to receive siacoins.
// The addresses are generated using the primary address seed.
//
// Warning: If this function is used to generate large numbers of addresses,
// those addresses should be used. Otherwise the lookahead might not be able to
// keep up and multiple wallets with the same seed might desync.
func (w *Wallet) NextAddresses(n uint64) ([]types.UnlockConditions, error) {
	if err := w.tg.Add(); err != nil {
		return []types.UnlockConditions{}, err
	}
	defer w.tg.Done()

	// TODO: going to the db is slow; consider creating 100 addresses at a
	// time.
	w.mu.Lock()
	ucs, err := w.nextPrimarySeedAddresses(w.dbTx, n)
	err = errors.Compose(err, w.syncDB())
	w.mu.Unlock()
	if err != nil {
		return []types.UnlockConditions{}, err
	}

	return ucs, err
}

// NextAddress returns an unlock hash that is ready to receive siacoins.
// The address is generated using the primary address seed.
func (w *Wallet) NextAddress() (types.UnlockConditions, error) {
	ucs, err := w.NextAddresses(1)
	if err != nil {
		return types.UnlockConditions{}, err
	}
	return ucs[0], nil
}

// LoadSeed will track all of the addresses generated by the input seed,
// reclaiming any funds that were lost due to a deleted file or lost encryption
// key. An error will be returned if the seed has already been integrated with
// the wallet.
func (w *Wallet) LoadSeed(masterKey crypto.TwofishKey, seed modules.Seed) error {
	if err := w.tg.Add(); err != nil {
		return err
	}
	defer w.tg.Done()

	if !w.cs.Synced() {
		return errors.New("cannot load seed until blockchain is synced")
	}

	if !w.scanLock.TryLock() {
		return errScanInProgress
	}
	defer w.scanLock.Unlock()

	// Because the recovery seed does not have a UID, duplication must be
	// prevented by comparing with the list of decrypted seeds. This can only
	// occur while the wallet is unlocked.
	w.mu.RLock()
	if !w.unlocked {
		w.mu.RUnlock()
		return modules.ErrLockedWallet
	}
	for _, wSeed := range append([]modules.Seed{w.primarySeed}, w.seeds...) {
		if seed == wSeed {
			w.mu.RUnlock()
			return errKnownSeed
		}
	}
	w.mu.RUnlock()

	// scan blockchain to determine how many keys to generate for the seed
	s := newSeedScanner(seed, w.log)
	if err := s.scan(w.cs, w.tg.StopChan()); err != nil {
		return err
	}
	// Add 4% as a buffer because the seed may have addresses in the wild
	// that have not appeared in the blockchain yet.
	seedProgress := s.largestIndexSeen + 500
	seedProgress += seedProgress / 25
	w.log.Printf("INFO: found key index %v in blockchain. Setting auxiliary seed progress to %v", s.largestIndexSeen, seedProgress)

	err := func() error {
		w.mu.Lock()
		defer w.mu.Unlock()

		err := checkMasterKey(w.dbTx, masterKey)
		if err != nil {
			return err
		}

		// create a seedFile for the seed
		sf := createSeedFile(masterKey, seed)

		// add the seedFile
		var current []seedFile
		err = encoding.Unmarshal(w.dbTx.Bucket(bucketWallet).Get(keyAuxiliarySeedFiles), &current)
		if err != nil {
			return err
		}
		err = w.dbTx.Bucket(bucketWallet).Put(keyAuxiliarySeedFiles, encoding.Marshal(append(current, sf)))
		if err != nil {
			return err
		}

		// load the seed's keys
		w.integrateSeed(seed, seedProgress)
		w.seeds = append(w.seeds, seed)

		// delete the set of processed transactions; they will be recreated
		// when we rescan
		if err = w.dbTx.DeleteBucket(bucketProcessedTransactions); err != nil {
			return err
		}
		if _, err = w.dbTx.CreateBucket(bucketProcessedTransactions); err != nil {
			return err
		}
		w.unconfirmedProcessedTransactions = nil

		// reset the consensus change ID and height in preparation for rescan
		err = dbPutConsensusChangeID(w.dbTx, modules.ConsensusChangeBeginning)
		if err != nil {
			return err
		}
		return dbPutConsensusHeight(w.dbTx, 0)
	}()
	if err != nil {
		return err
	}

	// rescan the blockchain
	w.cs.Unsubscribe(w)
	w.tpool.Unsubscribe(w)

	done := make(chan struct{})
	go w.rescanMessage(done)
	defer close(done)

	err = w.cs.ConsensusSetSubscribe(w, modules.ConsensusChangeBeginning, w.tg.StopChan())
	if err != nil {
		return err
	}
	w.tpool.TransactionPoolSubscribe(w)
	return nil
}

// SweepSeed scans the blockchain for outputs generated from seed and creates
// a transaction that transfers them to the wallet. Note that this incurs a
// transaction fee. It returns the total value of the outputs, minus the fee.
func (w *Wallet) SweepSeed(seed modules.Seed) (coins, funds types.Currency, err error) {
	if err = w.tg.Add(); err != nil {
		return
	}
	defer w.tg.Done()

	if !w.scanLock.TryLock() {
		return types.Currency{}, types.Currency{}, errScanInProgress
	}
	defer w.scanLock.Unlock()

	w.mu.RLock()
	match := seed == w.primarySeed
	w.mu.RUnlock()
	if match {
		return types.Currency{}, types.Currency{}, errors.New("cannot sweep primary seed")
	}

	if !w.cs.Synced() {
		return types.Currency{}, types.Currency{}, errors.New("cannot sweep until blockchain is synced")
	}

	// get an address to spend into
	w.mu.Lock()
	uc, err := w.nextPrimarySeedAddress(w.dbTx)
	w.mu.Unlock()
	if err != nil {
		return
	}

	// scan blockchain for outputs, filtering out 'dust' (outputs that cost
	// more in fees than they are worth)
	s := newSeedScanner(seed, w.log)
	_, maxFee := w.tpool.FeeEstimation()
	const outputSize = 350 // approx. size in bytes of an output and accompanying signature
	const maxOutputs = 50  // approx. number of outputs that a transaction can handle
	s.dustThreshold = maxFee.Mul64(outputSize)
	if err = s.scan(w.cs, w.tg.StopChan()); err != nil {
		return
	}

	if len(s.siacoinOutputs) == 0 {
		// if we aren't sweeping any coins, then just return an
		// error; no reason to proceed
		return types.Currency{}, types.Currency{}, errors.New("nothing to sweep")
	}

	// Flatten map to slice
	var siacoinOutputs []scannedOutput
	for _, sco := range s.siacoinOutputs {
		siacoinOutputs = append(siacoinOutputs, sco)
	}

	for len(siacoinOutputs) > 0 {
		// process up to maxOutputs siacoinOutputs
		txnSiacoinOutputs := make([]scannedOutput, maxOutputs)
		n := copy(txnSiacoinOutputs, siacoinOutputs)
		txnSiacoinOutputs = txnSiacoinOutputs[:n]
		siacoinOutputs = siacoinOutputs[n:]

		var txnCoins types.Currency

		// construct a transaction that spends the outputs
		tb, err := w.StartTransaction()
		if err != nil {
			return types.ZeroCurrency, types.ZeroCurrency, err
		}
		defer func() {
			if err != nil {
				tb.Drop()
			}
		}()
		var sweptCoins types.Currency // total values of swept outputs
		for _, output := range txnSiacoinOutputs {
			// construct a siacoin input that spends the output
			sk := generateSpendableKey(seed, output.seedIndex)
			tb.AddSiacoinInput(types.SiacoinInput{
				ParentID:         types.SiacoinOutputID(output.id),
				UnlockConditions: sk.UnlockConditions,
			})
			// add a signature for the input
			sweptCoins = sweptCoins.Add(output.value)
		}

		// estimate the transaction size and fee. NOTE: this equation doesn't
		// account for other fields in the transaction, but since we are
		// multiplying by maxFee, lowballing is ok
		estTxnSize := len(txnSiacoinOutputs) * outputSize
		estFee := maxFee.Mul64(uint64(estTxnSize))
		tb.AddMinerFee(estFee)

		// calculate total siacoin payout
		if sweptCoins.Cmp(estFee) > 0 {
			txnCoins = sweptCoins.Sub(estFee)
		}

		switch {
		case txnCoins.IsZero():
			// if we aren't sweeping any coins, then just return an
			// error; no reason to proceed
			return types.Currency{}, types.Currency{}, errors.New("transaction fee exceeds value of swept outputs")

		case !txnCoins.IsZero():
			// if we're sweeping coins, add a siacoin output for them
			tb.AddSiacoinOutput(types.SiacoinOutput{
				Value:      txnCoins,
				UnlockHash: uc.UnlockHash(),
			})
		}

		// add signatures for all coins (manually, since tb doesn't have
		// access to the signing keys)
		txn, parents := tb.View()
		for _, output := range txnSiacoinOutputs {
			sk := generateSpendableKey(seed, output.seedIndex)
			addSignatures(&txn, types.FullCoveredFields, sk.UnlockConditions, crypto.Hash(output.id), sk)
		}
		// Usually, all the inputs will come from swept outputs. However, there is
		// an edge case in which inputs will be added from the wallet. To cover
		// this case, we iterate through the SiacoinInputs and add a signature for
		// any input that belongs to the wallet.
		w.mu.RLock()
		for _, input := range txn.SiacoinInputs {
			if key, ok := w.keys[input.UnlockConditions.UnlockHash()]; ok {
				addSignatures(&txn, types.FullCoveredFields, input.UnlockConditions, crypto.Hash(input.ParentID), key)
			}
		}
		w.mu.RUnlock()

		// Append transaction to txnSet
		txnSet := append(parents, txn)

		// submit the transactions
		err = w.tpool.AcceptTransactionSet(txnSet)
		if err != nil {
			return types.ZeroCurrency, types.ZeroCurrency, err
		}

		w.log.Println("Creating a transaction set to sweep a seed, IDs:")
		for _, txn := range txnSet {
			w.log.Println("\t", txn.ID())
		}

		coins = coins.Add(txnCoins)
	}
	return
}
