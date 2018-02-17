package pool

import (
	"database/sql"
	"fmt"
	"math/big"
	"time"

	"github.com/HardDriveCoin/HardDriveCoin/modules"
	"github.com/HardDriveCoin/HardDriveCoin/types"
)

const (
	sqlLockRetry       = 10
	sqlRetryDelay      = 100 // milliseconds
	confirmedButUnpaid = "Confirmed but unpaid"
)

func (p *Pool) AddClientDB(c *Client) error {
	//	p.log.Debugf("Waiting to lock pool\n")
	p.mu.Lock()
	defer func() {
		//		p.log.Debugf("Unlocking pool\n")
		p.mu.Unlock()
	}()

	p.log.Printf("Adding client %s to database\n", c.Name())
	//	p.clients[c.Name()] = c

	tx, err := p.sqldb.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	stmt, err := tx.Prepare(`
		INSERT INTO Clients(ClientID, Name, Wallet)
		VALUES (?, ?, ?);
	`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	_, err = stmt.Exec(c.cr.clientID, c.cr.name, c.cr.name)
	if err != nil {
		return err
	}

	err = tx.Commit()
	if err != nil {
		return err
	}
	return nil
}

func (p *Pool) findClientDB(name string) *Client {
	var clientID uint64
	var Name, Wallet string
	// p.log.Printf("Searching database for client: %s\n", name)
	err := p.sqldb.QueryRow("SELECT ClientID, Name, Wallet FROM Clients WHERE Name = ?", name).Scan(&clientID, &Name, &Wallet)
	if err != nil {
		p.log.Debugf("Search failed: %s\n", err)
		return nil
	}
	// found client in database
	c := p.Client(Name)
	if c != nil {
		return c
	}
	// client was in database but not in memory - find workers and connect them to the in memory copy
	c, err = newClient(p, name)
	var wallet types.UnlockHash
	wallet.LoadString(Wallet)
	c.addWallet(wallet)
	c.cr.clientID = clientID
	stmt, err := p.sqldb.Prepare("SELECT WorkerID,Name,AverageDifficulty,BlocksFound FROM Worker WHERE Parent = ?;")
	if err != nil {
		p.log.Printf("Error finding workers for client %s: %s\n", name, err)
		return nil
	}
	defer stmt.Close()
	rows, err := stmt.Query(clientID)
	if err != nil {
		p.log.Printf("Error finding workers for client %s: %s\n", name, err)
		return nil
	}
	defer rows.Close()
	var id, blocks uint64
	var diff float64
	var wname string
	for rows.Next() {
		err = rows.Scan(&id, &wname, &diff, &blocks)
		if err != nil {
			p.log.Printf("Error finding workers for client %s: %s\n", wname, err)
			return nil
		}
		if c.Worker(wname) == nil {
			p.log.Debugf("Adding worker %s to memory\n", wname)
			w, _ := newWorker(c, wname, nil)
			w.wr.workerID = id
			w.wr.averageDifficulty = diff
			w.wr.blocksFound = blocks
			c.AddWorker(w)
		}
	}
	err = rows.Err()
	if err != nil {
		p.log.Printf("Error finding workers for client %s: %s\n", name, err)
		return nil
	}

	return c
}

func (w *Worker) updateWorkerRecord() error {
	stmt, err := w.Parent().pool.sqldb.Prepare(`
		UPDATE Worker
		SET AverageDifficulty = ?, BlocksFound = ?
		WHERE WorkerID = ?
		`)
	if err != nil {
		w.log.Printf("Error preparing to update worker: %s\n", err)
		return err
	}
	defer stmt.Close()
	_, err = stmt.Exec(w.CurrentDifficulty(), w.BlocksFound(), w.wr.workerID)
	if err != nil {
		w.log.Printf("Error updating record: %s\n", err)
		return err
	}
	return nil
}

func (w *Worker) addFoundBlock(b *types.Block) error {
	pool := w.wr.parent.pool
	tx, err := pool.sqldb.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	bh := pool.persist.GetBlockHeight()
	sub := b.CalculateSubsidy(bh).String()
	pool.mu.Lock()
	defer pool.mu.Unlock()
	timeStamp := time.Now().Format("2006-01-02 15:04:05 -700")
	update := fmt.Sprintf("UPDATE Block SET Height = %d, Reward = '%s', Time = '%s' WHERE BlockID = %d;", bh, sub, timeStamp, pool.blockCounter)
	w.log.Printf("Adding %s\n", update)
	_, err = tx.Exec(`UPDATE Block SET Height = ?, Reward = ?, Time = ? WHERE BlockID = ?;`, bh, sub, time.Now(), pool.blockCounter)
	if err != nil {
		return err
	}
	pool.blockCounter++
	_, err = tx.Exec(`INSERT INTO Block(BlockID, Height, Reward, Time) VALUES (?, 0, '0', 0);`, pool.blockCounter)
	if err != nil {
		return err
	}
	w.log.Printf("New candidate block added %d\n", pool.blockCounter)

	err = tx.Commit()
	if err != nil {
		return err
	}

	return nil
}

func (p *Pool) setBlockCounterFromDB() error {
	stmt, err := p.sqldb.Prepare(`SELECT BlockID FROM Block ORDER BY BlockID DESC;`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	rows, err := stmt.Query()
	if err != nil {
		return err
	}
	defer rows.Close()
	var value int64
	for rows.Next() {
		err = rows.Scan(&value)
		if err != nil {
			return err
		}
		break
	}
	err = rows.Err()
	if err != nil {
		return err
	}
	p.mu.Lock()
	p.blockCounter = uint64(value)
	p.mu.Unlock()
	p.log.Debugf("setBlockCounterFromDB, Count = %d\n", value)
	return nil
}

func (p *Pool) setShiftIDFromDB() error {
	stmt, err := p.sqldb.Prepare(`SELECT ShiftID FROM ShiftInfo WHERE Pool = ? ORDER BY ShiftID DESC;`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	rows, err := stmt.Query(p.InternalSettings().PoolID)
	if err != nil {
		return err
	}
	defer rows.Close()
	var value int64
	for rows.Next() {
		err = rows.Scan(&value)
		if err != nil {
			return err
		}
		break
	}
	err = rows.Err()
	if err != nil {
		return err
	}
	p.mu.Lock()
	p.shiftID = uint64(value) + 1
	p.mu.Unlock()
	p.log.Debugf("setshiftIDFromDB, Count = %d\n", p.shiftID)
	return nil
}

func (p *Pool) GetShift(w *Worker) *Shift {
	var blockID uint64
	var shares uint64
	var invalidShares uint64
	var staleShares uint64
	var cumulativeDifficulty float64
	var lastShareTime time.Time
	var shiftDuration int64
	err := p.sqldb.QueryRow(`
		SELECT Blocks,Shares,InvalidShares,StaleShares,CummulativeDifficulty,LastShareTime,ShiftDuration
		FROM ShiftInfo 
		WHERE ShiftID = ? AND Pool = ? AND Parent = ?;
		`, p.shiftID, p.InternalSettings().PoolID, w.wr.workerID).Scan(&blockID, &shares, &invalidShares, &staleShares, &cumulativeDifficulty, &lastShareTime, &shiftDuration)
	if err != nil {
		if err != sql.ErrNoRows {
			p.log.Debugf("Search failed: %s\n", err)
			return nil
		}
		// This is a new shift, save it
		return p.newShift(w)
	}
	startTime := time.Now().Add(time.Duration(-shiftDuration))
	return &Shift{
		shiftID:              p.shiftID,
		pool:                 p.InternalSettings().PoolID,
		worker:               w,
		blockID:              p.BlockCount(),
		shares:               shares,
		invalidShares:        invalidShares,
		staleShares:          staleShares,
		cumulativeDifficulty: cumulativeDifficulty,
		lastShareTime:        lastShareTime,
		startShiftTime:       startTime,
	}

}

// UpdateOrSaveShift updates (if existing) or saves (if new) the shift data
func (s *Shift) UpdateOrSaveShift() error {
	var id uint64
	pool := s.worker.Parent().Pool()
	err := pool.sqldb.QueryRow("SELECT ShiftID FROM ShiftInfo WHERE Pool = ? AND ShiftID = ? AND Parent = ?", pool.InternalSettings().PoolID, s.shiftID, s.worker.wr.workerID).Scan(&id)
	if err != nil {
		if err != sql.ErrNoRows {
			pool.log.Debugf("Search failed: %s\n", err)
			return nil
		}
		// This is a new shift, save it
		s.worker.log.Debugf("saving new shift\n")
		return s.saveShift()
	}
	// existing shift - update it
	s.worker.log.Debugf("updating existing shift\n")
	return s.updateShift()
}

func (s *Shift) updateShift() error {
	pool := s.worker.Parent().Pool()
	stmt, err := pool.sqldb.Prepare(`
		UPDATE ShiftInfo
		SET Shares = ?, InvalidShares = ?, StaleShares = ?, CummulativeDifficulty = ?, LastShareTime = ?, ShiftDuration = ?
		WHERE ShiftID = ? AND Pool = ? AND Parent = ?
		`)
	if err != nil {
		s.worker.log.Printf("Error preparing to update shift: %s\n", err)
		return err
	}
	defer stmt.Close()
	_, err = stmt.Exec(s.Shares(), s.Invalid(), s.Stale(), s.CumulativeDifficulty(), s.LastShareTime(), int(time.Now().Sub(s.StartShiftTime()).Seconds()),
		s.ShiftID(), pool.InternalSettings().PoolID, s.worker.wr.workerID)
	if err != nil {
		s.worker.log.Printf("Error updating record: %s\n", err)
		return err
	}
	return nil
}

// EndOldShift ends the old shift by committing the current shift data to the database
func (s *Shift) saveShift() error {

	stmt, err := s.worker.Parent().pool.sqldb.Prepare(`
		INSERT INTO 
		ShiftInfo(ShiftID, Pool, Parent, Blocks, Shares, InvalidShares, StaleShares, CummulativeDifficulty, LastShareTime, ShiftDuration)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?);
		`)
	if err != nil {
		s.worker.log.Printf("Error preparing to end shift: %s\n", err)
		return err
	}
	defer stmt.Close()
	_, err = stmt.Exec(s.ShiftID(), s.Pool(), s.worker.wr.workerID, s.BlockID(),
		s.Shares(), s.Invalid(), s.Stale(), s.CumulativeDifficulty(), s.LastShareTime(), int(time.Now().Sub(s.StartShiftTime()).Seconds()))
	if err != nil {
		s.worker.log.Printf("%d,%s,%d,%d,%d,%d,%d,%f,%v, %d\n", s.ShiftID(), s.Pool(), s.worker.wr.workerID, s.BlockID(),
			s.Shares(), s.Invalid(), s.Stale(), s.CumulativeDifficulty(), s.LastShareTime(), int(time.Now().Sub(s.StartShiftTime()).Seconds()))
		s.worker.log.Printf("Error adding record of last shift: %s\n", err)
		return err
	}
	s.worker.log.Debugf("%d,%s,%d,%d,%d,%d,%d,%f,%v, %d\n", s.ShiftID(), s.Pool(), s.worker.wr.workerID, s.BlockID(),
		s.Shares(), s.Invalid(), s.Stale(), s.CumulativeDifficulty(), s.LastShareTime(), int(time.Now().Sub(s.StartShiftTime()).Seconds()))
	return nil
}

func (c *Client) addWorkerDB(w *Worker) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.workers[w.Name()] = w
	c.log.Printf("Adding client %s worker %s to database\n", c.cr.name, w.Name())
	tx, err := c.pool.sqldb.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	stmt, err := tx.Prepare(`
		INSERT INTO Worker(WorkerID, Name, Parent, AverageDifficulty, BlocksFound)
		VALUES (?, ?, ?, ?, ?);
	`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	_, err = stmt.Exec(w.wr.workerID, w.wr.name, c.cr.clientID, w.wr.averageDifficulty, w.wr.blocksFound)
	if err != nil {
		return err
	}

	err = tx.Commit()
	if err != nil {
		return err
	}

	return nil
}

func (c *Client) findWorkerDB(worker string, s *Session) error {
	var id, blocks uint64
	var diff float64
	err := c.pool.sqldb.QueryRow(`
		SELECT Worker.WorkerID, Worker.AverageDifficulty, Worker.BlocksFound 
		FROM (Worker 
		INNER JOIN Clients ON Worker.Parent=Clients.ClientID)
		WHERE Worker.Name = ? AND Clients.Name = ?;
		`,
		worker, c.Name()).Scan(&id, &diff, &blocks)
	if err == nil {
		w, err := newWorker(c, worker, s)
		if err == nil {
			c.mu.Lock()
			defer c.mu.Unlock()
			w.wr.workerID = id
			w.wr.averageDifficulty = diff
			w.wr.blocksFound = blocks
			//			c.workersWorker = w
		}
	}
	if err != nil && err != sql.ErrNoRows {
		c.log.Printf("Error finding worker %s: %s\n", worker, err)
	}
	return err
}

func (p *Pool) ClientData() []modules.PoolClients {
	var pc []modules.PoolClients
	tx, err := p.sqldb.Begin()
	if err != nil {
		return nil
	}
	defer tx.Rollback()

	stmt, err := tx.Prepare(`
		SELECT DISTINCT Clients.Name, Clients.Balance, Worker.Name, SUM(ShiftInfo.Shares), AVG(Worker.AverageDifficulty),
		SUM(ShiftInfo.InvalidShares), SUM(ShiftInfo.StaleShares), SUM(ShiftInfo.CummulativeDifficulty), 
		MAX(ShiftInfo.LastShareTime)
		FROM ((Worker 
		INNER JOIN Clients ON Worker.Parent=Clients.ClientID)
		INNER JOIN ShiftInfo ON Worker.WorkerID=ShiftInfo.Parent)
		GROUP BY Clients.Name, Worker.Name, Clients.Balance;
	`)
	if err != nil {
		p.log.Printf("Prepare failed: %s\n", err)
		return nil
	}
	defer stmt.Close()

	rows, err := stmt.Query()
	if err != nil {
		p.log.Printf("Query failed: %s\n", err)
		return nil
	}

	var currentClient string
	var client modules.PoolClients
	var worker modules.PoolWorkers
	var c *Client
	var w *Worker
	var sharesThisBlock, invalidSharesThisBlock, staleSharesThisBlock, blocksFound uint64
	var averageDifficulty, cumulativeDifficulty float64
	var clientName, clientBalance, workerName string
	var shareTimeString string
	var count int

	defer rows.Close()
	for rows.Next() {
		err = rows.Scan(&clientName, &clientBalance, &workerName, &sharesThisBlock, &averageDifficulty, &invalidSharesThisBlock,
			&staleSharesThisBlock, &cumulativeDifficulty, &shareTimeString)
		if err != nil {
			p.log.Printf("Row scan failed: %s\n", err)
			return nil
		}
		var shareTime time.Time
		//		if p.InternalSettings().PoolDBConnection == "" || p.InternalSettings().PoolDBConnection == "internal" {
		shareTime, err = time.Parse("2006-01-02 15:04:05", shareTimeString)
		// } else {
		// 	shareTime, err = time.Parse(time.RFC3339, shareTimeString)
		// }
		if err != nil {
			p.log.Printf("Date conversion failed: %s\n", err)
		}

		if currentClient != clientName {
			client = modules.PoolClients{
				ClientName: clientName,
				Balance:    clientBalance,
			}
			pc = append(pc, client)
			count++
			c = p.Client(clientName)
		}

		worker = modules.PoolWorkers{
			WorkerName:             workerName,
			LastShareTime:          shareTime,
			CurrentDifficulty:      averageDifficulty,
			CumulativeDifficulty:   cumulativeDifficulty,
			SharesThisBlock:        sharesThisBlock,
			InvalidSharesThisBlock: invalidSharesThisBlock,
			StaleSharesThisBlock:   staleSharesThisBlock,
			BlocksFound:            blocksFound,
		}
		if c != nil {
			w = c.Worker(workerName)
		} else {
			w = nil
		}
		if w != nil && w.Online() {
			// add current shift data if online
			worker.CumulativeDifficulty += w.CumulativeDifficulty()
			if w.LastShareTime().Unix() > worker.LastShareTime.Unix() {
				worker.LastShareTime = w.LastShareTime()
			}
			worker.CurrentDifficulty = w.CurrentDifficulty()
			worker.SharesThisBlock += w.SharesThisBlock()
			worker.InvalidSharesThisBlock += w.InvalidShares()
			worker.StaleSharesThisBlock += w.StaleShares()

		}
		pc[count-1].Workers = append(pc[count-1].Workers, worker)
		currentClient = clientName
	}

	if rows.Err() != nil {
		p.log.Printf("Rows.Err failed: %s\n", err)
		return nil
	}
	if len(currentClient) == 0 {
		return nil
	}
	stmt.Close()
	rows.Close()
	for index := range pc {
		for windex := range pc[index].Workers {
			var mined uint64
			err := p.sqldb.QueryRow(`
			SELECT SUM(Worker.BlocksFound)
			FROM (Clients 
				INNER JOIN Worker ON Worker.Parent=Clients.ClientID)
			WHERE Clients.Name = ? AND Worker.Name = ?;
		`, pc[index].ClientName, pc[index].Workers[windex].WorkerName).Scan(&mined)
			if err != nil {
				if err != sql.ErrNoRows {
					p.log.Debugf("Search failed: %s\n", err)
					return nil
				}
				pc[index].Workers[windex].BlocksFound = 0
			} else {
				pc[index].Workers[windex].BlocksFound = mined
			}
			pc[index].BlocksMined += mined
		}
	}
	return pc
}

func (p *Pool) BlocksInfo() []modules.PoolBlocks {
	var pb []modules.PoolBlocks
	tx, err := p.sqldb.Begin()
	if err != nil {
		return nil
	}
	defer tx.Rollback()

	stmt, err := tx.Prepare(`
		SELECT BlockId, Height, Reward, Time, Paid, PaidTime 
		FROM Block 
		WHERE Height != '0'
		ORDER BY BlockId DESC;
	`)
	if err != nil {
		p.log.Printf("Prepare failed: %s\n", err)
		return nil
	}
	defer stmt.Close()

	rows, err := stmt.Query()
	if err != nil {
		p.log.Printf("Query failed: %s\n", err)
		return nil
	}

	var blockNumber uint64
	var blockHeight uint64
	var blockReward string
	var blockTimeString string
	var paid bool
	var paidTimeString string

	defer rows.Close()
	for rows.Next() {
		err = rows.Scan(&blockNumber, &blockHeight, &blockReward, &blockTimeString, &paid, &paidTimeString)
		if err != nil {
			p.log.Printf("Row scan failed: %s\n", err)
			return nil
		}
		blockTime, err := time.Parse("2006-01-02 15:04:05", blockTimeString)
		if err != nil {
			p.log.Printf("blockTime parse failed: %s\n", err)
		}
		var paidTime time.Time
		if paid == true {
			paidTime, err = time.Parse("2006-01-02 15:04:05", paidTimeString)
			if err != nil {
				p.log.Printf("paidTime parse failed: %s\n", err)
			}
		}
		block := modules.PoolBlocks{
			BlockHeight: blockHeight,
			BlockNumber: blockNumber,
			BlockReward: blockReward,
			BlockTime:   blockTime,
		}
		if paid == true {
			block.BlockStatus = fmt.Sprintf("Paid: %s", paidTime.Format(time.RFC822))
		} else {
			confirmations := uint64(p.persist.GetBlockHeight()) - blockHeight
			if confirmations < uint64(types.MaturityDelay) {
				block.BlockStatus = fmt.Sprintf("%d Confirmations", confirmations)
			} else {
				block.BlockStatus = confirmedButUnpaid
			}
		}

		pb = append(pb, block)

	}
	if rows.Err() != nil {
		p.log.Printf("Rows.Err failed: %s\n", err)
		return nil
	}

	return pb
}

func (p *Pool) BlockInfo(b uint64) []modules.PoolBlock {
	var pb []modules.PoolBlock
	if b == 0 {
		// current block
		b = p.blockCounter
	}
	tx, err := p.sqldb.Begin()
	if err != nil {
		return nil
	}
	defer tx.Rollback()

	stmt, err := tx.Prepare(`
		SELECT Clients.Name, SUM(ShiftInfo.CummulativeDifficulty),Block.Height, Block.Reward
		FROM (((Worker 
			INNER JOIN Clients ON Worker.Parent=Clients.ClientID)
			INNER JOIN ShiftInfo ON Worker.WorkerID=ShiftInfo.Parent)
			INNER JOIN Block ON ShiftInfo.Blocks=Block.BlockId)
		WHERE ShiftInfo.Blocks = ?
		GROUP BY Clients.Name, Block.Height, Block.Reward
		;
		`)
	if err != nil {
		p.log.Printf("Prepare failed: %s\n", err)
		return nil
	}
	defer stmt.Close()

	rows, err := stmt.Query(b)
	if err != nil {
		p.log.Printf("Query failed: %s\n", err)
		return nil
	}

	var clientName string
	var diffShares float64
	var blockHeight uint64
	var blockReward string
	var shares []float64
	var totalShares float64

	defer rows.Close()

	for rows.Next() {
		err = rows.Scan(&clientName, &diffShares, &blockHeight, &blockReward)
		if err != nil {
			p.log.Printf("Row scan failed: %s\n", err)
			return nil
		}
		fmt.Printf("%s: %g %d %s\n", clientName, diffShares, blockHeight, blockReward)
		if diffShares < 0.0000001 {
			// really small shares are discarded to prevent dividing by zero below.
			// this can only really happen when there is a shiftinfo entry for a block,
			// but no shares were actually submitted
			continue
		}
		block := modules.PoolBlock{
			ClientName: clientName,
		}
		shares = append(shares, diffShares)
		pb = append(pb, block)
		totalShares += diffShares
	}
	if rows.Err() != nil {
		p.log.Printf("Rows.Err failed: %s\n", err)
		return nil
	}
	reward := big.NewInt(0)
	reward.SetString(blockReward, 10)
	currency := types.NewCurrency(reward)
	operatorPercentage := p.InternalSettings().PoolOperatorPercentage
	totalShares = totalShares / (1 - (operatorPercentage / 100))

	for index, value := range shares {
		pb[index].ClientPercentage = (value / totalShares) * 100.0
		pb[index].ClientReward = fmt.Sprintf("%s", currency.MulFloat(value/totalShares).String())
	}
	block := modules.PoolBlock{
		ClientName:       "Operator",
		ClientPercentage: operatorPercentage,
		ClientReward:     fmt.Sprintf("%s", currency.MulFloat(operatorPercentage/100.0).String()),
	}
	pb = append(pb, block)
	return pb
}

func (p *Pool) markBlockPaid(block uint64) error {
	tx, err := p.sqldb.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	stmt, err := tx.Prepare(`
		UPDATE Block
		SET Paid = ?, PaidTime = ?
		WHERE BlockId = ?
	`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	p.log.Printf("Marking block %d, paid at %s\n", block, time.Now())
	_, err = stmt.Exec(true, time.Now().Unix(), block)
	if err != nil {
		return err
	}

	err = tx.Commit()
	if err != nil {
		return err
	}

	return nil

}

func (p *Pool) makeClientTransaction(clientID uint64, transaction string, memo string) error {
	tx, err := p.sqldb.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	stmt, err := tx.Prepare(`
		INSERT INTO Ledger(Parent, Transaction, Timestamp, Memo)
		VALUES (?, ?, ?, ?);
	`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	_, err = stmt.Exec(clientID, transaction, time.Now(), memo)
	if err != nil {
		return err
	}

	err = tx.Commit()
	if err != nil {
		return err
	}
	return nil
}

func (p *Pool) ClientTransactions(name string) []modules.PoolClientTransactions {
	var balanceChange string
	var txTime time.Time
	var memo string
	var ct []modules.PoolClientTransactions

	rows, err := p.sqldb.Query(`
		SELECT Ledger.Transaction,Ledger.Timestamp,Ledger.Memo
		FROM (Ledger
			INNER JOIN Clients ON Ledger.Parent=Clients.ClientID)
		WHERE Clients.Name = ?
		ORDER BY Ledger.LedgerID ASC;
		`, name)
	if err != nil {
		p.log.Debugf("Search failed: %s\n", err)
		return nil
	}
	defer rows.Close()
	for rows.Next() {
		err = rows.Scan(&balanceChange, &txTime, &memo)
		if err != nil {
			p.log.Printf("Row scan failed: %s\n", err)
			return nil
		}
		entry := modules.PoolClientTransactions{
			BalanceChange: balanceChange,
			TxTime:        txTime,
			Memo:          memo,
		}
		ct = append(ct, entry)
	}
	return ct
}

func (p *Pool) modifyClientBalance(clientID uint64, change string) error {
	var balance string
	err := p.sqldb.QueryRow("SELECT Balance FROM Clients WHERE ClientID = ?;", clientID).Scan(&balance)
	if err != nil {
		p.log.Debugf("Search failed: %s\n", err)
		return err
	}
	balanceI := big.NewInt(0)
	balanceI.SetString(balance, 10)
	balanceC := types.NewCurrency(balanceI)

	changeI := big.NewInt(0)
	changeI.SetString(change, 10)
	changeC := types.NewCurrency(changeI)

	balanceC = balanceC.Add(changeC)

	stmt, err := p.sqldb.Prepare(`
		UPDATE Clients
		SET Balance = ?
		WHERE ClientID = ?;
		`)
	if err != nil {
		p.log.Printf("Error preparing to update client: %s\n", err)
		return err
	}
	defer stmt.Close()
	_, err = stmt.Exec(balanceC.String(), clientID)
	if err != nil {
		p.log.Printf("Error updating record: %s\n", err)
		return err
	}
	p.log.Printf("Modifying client %d's balance by %s\n", clientID, change)
	return nil
}

func (p *Pool) modifyClientWallet(clientID uint64, wallet string) error {
	stmt, err := p.sqldb.Prepare(`
		UPDATE Clients
		SET Wallet = ?
		WHERE ClientID = ?;
		`)
	if err != nil {
		p.log.Printf("Error preparing to update client: %s\n", err)
		return err
	}
	defer stmt.Close()
	_, err = stmt.Exec(wallet, clientID)
	if err != nil {
		p.log.Printf("Error updating record: %s\n", err)
		return err
	}
	p.log.Printf("Modifying client %d's wallet to %s\n", clientID, wallet)
	return nil
}

func (p *Pool) modifyClientName(clientID uint64, name string) error {
	stmt, err := p.sqldb.Prepare(`
		UPDATE Clients
		SET Name = ?
		WHERE ClientID = ?;
		`)
	if err != nil {
		p.log.Printf("Error preparing to update client: %s\n", err)
		return err
	}
	defer stmt.Close()
	_, err = stmt.Exec(name, clientID)
	if err != nil {
		p.log.Printf("Error updating record: %s\n", err)
		return err
	}
	p.log.Printf("Modifying client %d's wallet to %s\n", clientID, name)
	return nil
}
