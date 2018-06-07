package pool

import (
	"fmt"
	"bytes"
	// "fmt"
	"math/big"
	"time"

	"github.com/HyperspaceApp/Hyperspace/modules"
	"github.com/HyperspaceApp/Hyperspace/types"
)

const (
	sqlLockRetry       = 10
	sqlRetryDelay      = 100 // milliseconds
	confirmedButUnpaid = "Confirmed but unpaid"
)

// AddClientDB add user into accounts
func (p *Pool) AddClientDB(c *Client) error {
	p.mu.Lock()
	defer func() {
		p.mu.Unlock()
	}()

	p.yiilog.Printf("Adding user %s to yiimp account\n", c.Name())
	tx, err := p.sqldb.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	stmt, err := tx.Prepare(`
		INSERT INTO accounts (coinid, username, coinsymbol)
		VALUES (?, ?, ?);
	`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	rs, err := stmt.Exec(SiaCoinID, c.cr.name, SiaCoinSymbol)
	if err != nil {
		return err
	}
	err = tx.Commit()
	if err != nil {
		return err
	}
	id, err := rs.LastInsertId()
	p.yiilog.Printf("User %s account id is %d\n", c.Name(), id)
	c.cr.clientID = id

	return nil
}

// FindClientDB find user in accounts
func (p *Pool) FindClientDB(name string) *Client {
	var clientID int64
	var Name, Wallet string

	p.yiilog.Debugf("Searching for %s in existing accounts\n", name)
	err := p.sqldb.QueryRow("SELECT id, username, username FROM accounts WHERE username = ?", name).Scan(&clientID, &Name, &Wallet)
	if err != nil {
		p.yiilog.Debugf("Search failed: %s\n", err)
		return nil
	}
	p.yiilog.Debugf("Account %s found: %d \n", Name, clientID)
	// if we're here, we found the client in the database
	// try looking for the client in memory
	c := p.Client(Name)
	// if it's in memory, just return a pointer to the copy in memory
	if c != nil {
		return c
	}
	// client was in database but not in memory -
	// find workers and connect them to the in memory copy
	c, err = newClient(p, name)
	if err != nil {
		p.log.Printf("Error when creating a new client %s: %s\n", name, err)
		return nil
	}
	var wallet types.UnlockHash
	wallet.LoadString(Wallet)
	c.addWallet(wallet)
	c.cr.clientID = clientID

	return c
}

// updateWorkerRecord update worker in workers
func (w *Worker) updateWorkerRecord() error {
	stmt, err := w.Parent().pool.sqldb.Prepare(`
		UPDATE workers
		SET difficulty = ?
		WHERE id = ?
		`)
	if err != nil {
		w.log.Printf("Error preparing to update worker: %s\n", err)
		return err
	}
	defer stmt.Close()
	_, err = stmt.Exec(w.CurrentDifficulty(), w.wr.workerID)
	if err != nil {
		w.log.Printf("Error updating record: %s\n", err)
		return err
	}
	return nil
}

func (w *Worker) deleteWorkerRecord() error {
	stmt, err := w.Parent().pool.sqldb.Prepare(`
		DELETE FROM workers
		WHERE id = ?
	`)
	if err != nil {
		w.log.Printf("Error preparing to update worker: %s\n", err)
		return err
	}
	defer stmt.Close()
	_, err = stmt.Exec(w.wr.workerID)
	if err != nil {
		w.log.Printf("Error deleting record: %s\n", err)
		return err
	}
	return nil
}

func (p *Pool) DeleteAllWorkerRecords() error {
	stmt, err := p.sqldb.Prepare(`
		DELETE FROM workers
		WHERE pid = ?
	`)
	if err != nil {
		p.log.Printf("Error preparing to delete all workers: %s\n", err)
		return err
	}
	defer stmt.Close()
	_, err = stmt.Exec(p.InternalSettings().PoolID)
	if err != nil {
		p.log.Printf("Error deleting records: %s\n", err)
		return err
	}
	return nil
}

// addFoundBlock add founded block to yiimp blocks table
func (w *Worker) addFoundBlock(b *types.Block) error {
	pool := w.Parent().Pool()
	tx, err := pool.sqldb.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	bh := pool.persist.GetBlockHeight()
	pool.blockCounter = uint64(bh) + 1
	w.log.Printf("New block to mine on %d\n", pool.blockCounter)
	// reward := b.CalculateSubsidy(bh).String()
	pool.mu.Lock()
	defer pool.mu.Unlock()
	timeStamp := time.Now().Unix()
	// TODO: maybe add difficulty_user
	stmt, err := tx.Prepare(`
		INSERT INTO blocks
		(height, blockhash, coin_id, userid, workerid, category, difficulty, time, algo)
		VALUES
		(?, ?, ?, ?, ?, ?, ?, ?, ?)
		`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	currentTarget, _ := pool.cs.ChildTarget(b.ID())
	difficulty, _ := currentTarget.Difficulty().Uint64() // TODO: maybe should use parent ChildTarget
	// TODO: figure out right difficulty_user
	_, err = stmt.Exec(bh, b.ID().String(), SiaCoinID, w.Parent().cr.clientID,
		w.wr.workerID, "new", difficulty, timeStamp, SiaCoinAlgo)
	if err != nil {
		return err
	}
	err = tx.Commit()
	if err != nil {
		return err
	}

	return nil
}

func (p *Pool) setBlockCounterFromDB() error {
	stmt, err := p.sqldb.Prepare(`SELECT height FROM blocks WHERE coin_id = ? ORDER BY height DESC;`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	rows, err := stmt.Query(SiaCoinID)
	if err != nil {
		return err
	}
	defer rows.Close()
	var value uint64
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
	p.blockCounter = value
	p.mu.Unlock()
	p.log.Debugf("setBlockCounterFromDB, Count = %d\n", value)
	return nil
}

func (s *Shift) UpdateOrSaveShift() error {
	if len(s.Shares()) == 0 {
		return nil
	}

	worker := s.worker
	client := worker.Parent()
	pool := client.Pool()
	var buffer bytes.Buffer
	buffer.WriteString("INSERT INTO shares(userid, workerid, coinid, valid, difficulty, time, algo, reward, block_difficulty, status) VALUES ")
	for i, share := range s.Shares() {
		if i != 0 {
			buffer.WriteString(",")
		}
		buffer.WriteString(fmt.Sprintf("(%d, %d, %d, %t, %f, %d, '%s', %f, %f, %d)", share.userid, share.workerid, SiaCoinID, share.valid, share.difficulty, time.Now().Unix(), SiaCoinAlgo, share.reward, share.block_difficulty, 0))
	}
	buffer.WriteString(";")

	rows, err := pool.sqldb.Query(buffer.String());
	defer rows.Close()
	if err != nil {
		worker.log.Println(buffer.String())
		worker.log.Printf("Error saving shares: %s\n", err)
		return err
	}
	// TODO: add share_diff which is client submitted diff
	if err != nil {
		worker.log.Println(buffer.String())
		worker.log.Printf("Error adding record of last shift: %s\n", err)
		return err
	}
	worker.log.Debugf(buffer.String())
	return nil
}

// addWorkerDB inserts info to workers
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
	// TODO: add ip etc info
	stmt, err := tx.Prepare(`
		INSERT INTO workers (userid, name, difficulty, worker, algo, time, pid)
		VALUES (?, ?, ?, ?, ?, ?, ?);
	`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	rs, err := stmt.Exec(c.cr.clientID, c.cr.name, w.wr.averageDifficulty, w.wr.name, SiaCoinAlgo, time.Now().Unix(), c.pool.InternalSettings().PoolID)
	if err != nil {
		return err
	}

	affectedRows, err := rs.RowsAffected()
	if err != nil {
		return err
	}
	w.log.Printf("Rows affected insert workers %d", affectedRows)

	id, err := rs.LastInsertId()
	if err != nil {
		return err
	}

	w.wr.workerID = id

	err = tx.Commit()
	if err != nil {
		return err
	}

	return nil
}

// func (p *Pool) ClientData() []modules.PoolClients {
// 	var pc []modules.PoolClients
// 	tx, err := p.sqldb.Begin()
// 	if err != nil {
// 		return nil
// 	}
// 	defer tx.Rollback()

// 	stmt, err := tx.Prepare(`
// 		SELECT DISTINCT Clients.Name, Clients.Balance, Worker.Name, SUM(ShiftInfo.Shares), AVG(Worker.AverageDifficulty),
// 		SUM(ShiftInfo.InvalidShares), SUM(ShiftInfo.StaleShares), SUM(ShiftInfo.CummulativeDifficulty),
// 		MAX(ShiftInfo.LastShareTime)
// 		FROM ((Worker
// 		INNER JOIN Clients ON Worker.Parent=Clients.ClientID)
// 		INNER JOIN ShiftInfo ON Worker.WorkerID=ShiftInfo.Parent)
// 		GROUP BY Clients.Name, Worker.Name, Clients.Balance;
// 	`)
// 	if err != nil {
// 		p.log.Printf("Prepare failed: %s\n", err)
// 		return nil
// 	}
// 	defer stmt.Close()

// 	rows, err := stmt.Query()
// 	if err != nil {
// 		p.log.Printf("Query failed: %s\n", err)
// 		return nil
// 	}

// 	var currentClient string
// 	var client modules.PoolClients
// 	var worker modules.PoolWorkers
// 	var c *Client
// 	var w *Worker
// 	var sharesThisBlock, invalidSharesThisBlock, staleSharesThisBlock, blocksFound uint64
// 	var averageDifficulty, cumulativeDifficulty float64
// 	var clientName, clientBalance, workerName string
// 	var shareTimeString string
// 	var count int

// 	defer rows.Close()
// 	for rows.Next() {
// 		err = rows.Scan(&clientName, &clientBalance, &workerName, &sharesThisBlock, &averageDifficulty, &invalidSharesThisBlock,
// 			&staleSharesThisBlock, &cumulativeDifficulty, &shareTimeString)
// 		if err != nil {
// 			p.log.Printf("Row scan failed: %s\n", err)
// 			return nil
// 		}
// 		var shareTime time.Time
// 		//		if p.InternalSettings().PoolDBConnection == "" || p.InternalSettings().PoolDBConnection == "internal" {
// 		shareTime, err = time.Parse("2006-01-02 15:04:05", shareTimeString)
// 		// } else {
// 		// 	shareTime, err = time.Parse(time.RFC3339, shareTimeString)
// 		// }
// 		if err != nil {
// 			p.log.Printf("Date conversion failed: %s\n", err)
// 		}

// 		if currentClient != clientName {
// 			client = modules.PoolClients{
// 				ClientName: clientName,
// 				Balance:    clientBalance,
// 			}
// 			pc = append(pc, client)
// 			count++
// 			c = p.Client(clientName)
// 		}

// 		worker = modules.PoolWorkers{
// 			WorkerName:             workerName,
// 			LastShareTime:          shareTime,
// 			CurrentDifficulty:      averageDifficulty,
// 			CumulativeDifficulty:   cumulativeDifficulty,
// 			SharesThisBlock:        sharesThisBlock,
// 			InvalidSharesThisBlock: invalidSharesThisBlock,
// 			StaleSharesThisBlock:   staleSharesThisBlock,
// 			BlocksFound:            blocksFound,
// 		}
// 		if c != nil {
// 			w = c.Worker(workerName)
// 		} else {
// 			w = nil
// 		}
// 		if w != nil && w.Online() {
// 			// add current shift data if online
// 			worker.CumulativeDifficulty += w.CumulativeDifficulty()
// 			if w.LastShareTime().Unix() > worker.LastShareTime.Unix() {
// 				worker.LastShareTime = w.LastShareTime()
// 			}
// 			worker.CurrentDifficulty = w.CurrentDifficulty()
// 			worker.SharesThisBlock += w.SharesThisBlock()
// 			worker.InvalidSharesThisBlock += w.InvalidShares()
// 			worker.StaleSharesThisBlock += w.StaleShares()

// 		}
// 		pc[count-1].Workers = append(pc[count-1].Workers, worker)
// 		currentClient = clientName
// 	}

// 	if rows.Err() != nil {
// 		p.log.Printf("Rows.Err failed: %s\n", err)
// 		return nil
// 	}
// 	if len(currentClient) == 0 {
// 		return nil
// 	}
// 	stmt.Close()
// 	rows.Close()
// 	for index := range pc {
// 		for windex := range pc[index].Workers {
// 			var mined uint64
// 			err := p.sqldb.QueryRow(`
// 			SELECT SUM(Worker.BlocksFound)
// 			FROM (Clients
// 				INNER JOIN Worker ON Worker.Parent=Clients.ClientID)
// 			WHERE Clients.Name = ? AND Worker.Name = ?;
// 		`, pc[index].ClientName, pc[index].Workers[windex].WorkerName).Scan(&mined)
// 			if err != nil {
// 				if err != sql.ErrNoRows {
// 					p.log.Debugf("Search failed: %s\n", err)
// 					return nil
// 				}
// 				pc[index].Workers[windex].BlocksFound = 0
// 			} else {
// 				pc[index].Workers[windex].BlocksFound = mined
// 			}
// 			pc[index].BlocksMined += mined
// 		}
// 	}
// 	return pc
// }

// func (p *Pool) BlocksInfo() []modules.PoolBlocks {
// 	var pb []modules.PoolBlocks
// 	tx, err := p.sqldb.Begin()
// 	if err != nil {
// 		return nil
// 	}
// 	defer tx.Rollback()

// 	stmt, err := tx.Prepare(`
// 		SELECT BlockId, Height, Reward, Time, Paid, PaidTime
// 		FROM Block
// 		WHERE Height != '0'
// 		ORDER BY BlockId DESC;
// 	`)
// 	if err != nil {
// 		p.log.Printf("Prepare failed: %s\n", err)
// 		return nil
// 	}
// 	defer stmt.Close()

// 	rows, err := stmt.Query()
// 	if err != nil {
// 		p.log.Printf("Query failed: %s\n", err)
// 		return nil
// 	}

// 	var blockNumber uint64
// 	var blockHeight uint64
// 	var blockReward string
// 	var blockTimeString string
// 	var paid bool
// 	var paidTimeString string

// 	defer rows.Close()
// 	for rows.Next() {
// 		err = rows.Scan(&blockNumber, &blockHeight, &blockReward, &blockTimeString, &paid, &paidTimeString)
// 		if err != nil {
// 			p.log.Printf("Row scan failed: %s\n", err)
// 			return nil
// 		}
// 		blockTime, err := time.Parse("2006-01-02 15:04:05", blockTimeString)
// 		if err != nil {
// 			p.log.Printf("blockTime parse failed: %s\n", err)
// 		}
// 		var paidTime time.Time
// 		if paid == true {
// 			paidTime, err = time.Parse("2006-01-02 15:04:05", paidTimeString)
// 			if err != nil {
// 				p.log.Printf("paidTime parse failed: %s\n", err)
// 			}
// 		}
// 		block := modules.PoolBlocks{
// 			BlockHeight: blockHeight,
// 			BlockNumber: blockNumber,
// 			BlockReward: blockReward,
// 			BlockTime:   blockTime,
// 		}
// 		if paid == true {
// 			block.BlockStatus = fmt.Sprintf("Paid: %s", paidTime.Format(time.RFC822))
// 		} else {
// 			confirmations := uint64(p.persist.GetBlockHeight()) - blockHeight
// 			if confirmations < uint64(types.MaturityDelay) {
// 				block.BlockStatus = fmt.Sprintf("%d Confirmations", confirmations)
// 			} else {
// 				block.BlockStatus = confirmedButUnpaid
// 			}
// 		}

// 		pb = append(pb, block)

// 	}
// 	if rows.Err() != nil {
// 		p.log.Printf("Rows.Err failed: %s\n", err)
// 		return nil
// 	}

// 	return pb
// }

// func (p *Pool) BlockInfo(b uint64) []modules.PoolBlock {
// 	var pb []modules.PoolBlock
// 	if b == 0 {
// 		// current block
// 		b = p.blockCounter
// 	}
// 	tx, err := p.sqldb.Begin()
// 	if err != nil {
// 		return nil
// 	}
// 	defer tx.Rollback()

// 	stmt, err := tx.Prepare(`
// 		SELECT Clients.Name, SUM(ShiftInfo.CummulativeDifficulty),Block.Height, Block.Reward
// 		FROM (((Worker
// 			INNER JOIN Clients ON Worker.Parent=Clients.ClientID)
// 			INNER JOIN ShiftInfo ON Worker.WorkerID=ShiftInfo.Parent)
// 			INNER JOIN Block ON ShiftInfo.Blocks=Block.BlockId)
// 		WHERE ShiftInfo.Blocks = ?
// 		GROUP BY Clients.Name, Block.Height, Block.Reward
// 		;
// 		`)
// 	if err != nil {
// 		p.log.Printf("Prepare failed: %s\n", err)
// 		return nil
// 	}
// 	defer stmt.Close()

// 	rows, err := stmt.Query(b)
// 	if err != nil {
// 		p.log.Printf("Query failed: %s\n", err)
// 		return nil
// 	}

// 	var clientName string
// 	var diffShares float64
// 	var blockHeight uint64
// 	var blockReward string
// 	var shares []float64
// 	var totalShares float64

// 	defer rows.Close()

// 	for rows.Next() {
// 		err = rows.Scan(&clientName, &diffShares, &blockHeight, &blockReward)
// 		if err != nil {
// 			p.log.Printf("Row scan failed: %s\n", err)
// 			return nil
// 		}
// 		fmt.Printf("%s: %g %d %s\n", clientName, diffShares, blockHeight, blockReward)
// 		if diffShares < 0.0000001 {
// 			// really small shares are discarded to prevent dividing by zero below.
// 			// this can only really happen when there is a shiftinfo entry for a block,
// 			// but no shares were actually submitted
// 			continue
// 		}
// 		block := modules.PoolBlock{
// 			ClientName: clientName,
// 		}
// 		shares = append(shares, diffShares)
// 		pb = append(pb, block)
// 		totalShares += diffShares
// 	}
// 	if rows.Err() != nil {
// 		p.log.Printf("Rows.Err failed: %s\n", err)
// 		return nil
// 	}
// 	reward := big.NewInt(0)
// 	reward.SetString(blockReward, 10)
// 	currency := types.NewCurrency(reward)
// 	operatorPercentage := p.InternalSettings().PoolOperatorPercentage
// 	totalShares = totalShares / (1 - (operatorPercentage / 100))

// 	for index, value := range shares {
// 		pb[index].ClientPercentage = (value / totalShares) * 100.0
// 		pb[index].ClientReward = fmt.Sprintf("%s", currency.MulFloat(value/totalShares).String())
// 	}
// 	block := modules.PoolBlock{
// 		ClientName:       "Operator",
// 		ClientPercentage: operatorPercentage,
// 		ClientReward:     fmt.Sprintf("%s", currency.MulFloat(operatorPercentage/100.0).String()),
// 	}
// 	pb = append(pb, block)
// 	return pb
// }

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

func (p *Pool) makeClientTransaction(clientID int64, transaction string, memo string) error {
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

func (p *Pool) modifyClientBalance(clientID int64, change string) error {
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
