package pool

import (
	"database/sql"
	"fmt"
	"time"

	"github.com/NebulousLabs/Sia/modules"
	"github.com/NebulousLabs/Sia/types"
)

const (
	sqlLockRetry  = 10
	sqlRetryDelay = 100 // milliseconds
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
		INSERT INTO [Clients]([ClientID], [Name], [Wallet])
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
	err := p.sqldb.QueryRow("SELECT [ClientID], [Name], [Wallet] FROM [Clients] WHERE [Name] = ?", name).Scan(&clientID, &Name, &Wallet)
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
	stmt, err := p.sqldb.Prepare("SELECT [WorkerID],[Name],[AverageDifficulty],[BlocksFound] FROM [Worker] WHERE [Parent] = ?;")
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
			p.log.Debug("Adding worker %s to memory\n", wname)
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

func (w *Worker) getUint64Field(field string) uint64 {
	query := fmt.Sprintf("SELECT [%s] FROM [ShiftInfo] WHERE [Parent] = $1 AND [Blocks] = %d AND [Pool] = $2;", field, w.wr.parent.pool.blockCounter)

	var value uint64
	value = 0
	stmt, err := w.wr.parent.pool.sqldb.Prepare(query)
	if err != nil {
		return value
	}
	defer stmt.Close()
	rows, err := stmt.Query(w.wr.workerID, w.wr.parent.pool.id)
	if err != nil {
		return value
	}
	defer rows.Close()
	var sum uint64
	sum = 0
	for rows.Next() {
		err = rows.Scan(&value)
		if err != nil {
			return sum
		}
		sum += value
	}
	err = rows.Err()
	if err != nil {
		w.log.Printf("Error getting field %s: %s\n", field, err)
		return sum
	}
	return sum
}

func (w *Worker) getFloatField(field string) float64 {
	query := fmt.Sprintf("SELECT [%s] FROM [ShiftInfo] WHERE [Parent] = $1 AND [Blocks] = %d AND [Pool] = $2;", field, w.wr.parent.pool.blockCounter)

	var value float64
	value = 0.0
	stmt, err := w.wr.parent.pool.sqldb.Prepare(query)
	if err != nil {
		return value
	}
	defer stmt.Close()
	rows, err := stmt.Query(w.wr.workerID, w.wr.parent.pool.id)
	if err != nil {
		return value
	}
	defer rows.Close()
	var sum float64
	sum = 0.0
	for rows.Next() {
		err = rows.Scan(&value)
		if err != nil {
			return sum
		}
		sum += value
	}
	err = rows.Err()
	if err != nil {
		w.log.Printf("Error getting field %s: %s\n", field, err)
		return sum
	}
	return sum
}

func (w *Worker) updateWorkerRecord() error {
	stmt, err := w.Parent().pool.sqldb.Prepare(`
		UPDATE [Worker]
		SET [AverageDifficulty] = $1, [BlocksFound] = $2
		WHERE [WorkerID] = $3
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
	tx, err := w.wr.parent.pool.sqldb.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	stmt, err := tx.Prepare(`
		UPDATE [Block]
		SET [Height] = $1, [Reward] = $2, [Time] = $3
		WHERE [BlockID] = $4
	`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	bh := w.wr.parent.pool.persist.GetBlockHeight()
	sub := b.CalculateSubsidy(bh).String()
	w.log.Printf("Adding found block %d, height %d, subsidy %s: err %v\n", w.wr.parent.pool.blockCounter, bh, sub, err)
	_, err = stmt.Exec(bh, sub, time.Now().Unix(), w.wr.parent.pool.blockCounter)
	if err != nil {
		return err
	}
	w.wr.parent.pool.blockCounter++
	_, err = tx.Exec(`INSERT INTO [Block]([BlockID], [Height], [Reward], [Time]) VALUES ($1, 0, "0", 0);`, w.wr.parent.pool.blockCounter)
	if err != nil {
		return err
	}
	w.log.Printf("New candidate block added %d\n", w.wr.parent.pool.blockCounter)

	err = tx.Commit()
	if err != nil {
		return err
	}

	return nil
}

func (p *Pool) setBlockCounterFromDB() error {
	stmt, err := p.sqldb.Prepare(`SELECT [BlockID] FROM [Block] ORDER BY [BlockID] DESC;`)
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
	stmt, err := p.sqldb.Prepare(`SELECT [ShiftID] FROM [ShiftInfo] WHERE [Pool] = $1 ORDER BY [ShiftID] DESC;`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	rows, err := stmt.Query(p.id)
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
	p.shiftID = uint64(value)
	p.log.Debugf("setshiftIDFromDB, Count = %d\n", value)
	return nil
}

func (p *Pool) GetShift(w *Worker) *Shift {
	var blockID uint64
	var shares uint64
	var invalidShares uint64
	var staleShares uint64
	var cumulativeDifficulty float64
	var lastShareTime time.Time
	err := p.sqldb.QueryRow(`
		SELECT [Blocks],[Shares],[InvalidShares],[StaleShares],[CummulativeDifficulty],[LastShareTime]
		FROM [ShiftInfo] WHERE [ShiftID] = ? AND [Pool] = ? AND [Parent] = ?
		`, p.shiftID, p.id, w.wr.workerID).Scan(&blockID, &shares, &invalidShares, &staleShares, &cumulativeDifficulty, &lastShareTime)
	if err != nil {
		if err != sql.ErrNoRows {
			p.log.Debugf("Search failed: %s\n", err)
			return nil
		}
		// This is a new shift, save it
		return p.newShift(w)
	}
	return &Shift{
		shiftID:              p.shiftID,
		pool:                 p.id,
		worker:               w,
		blockID:              p.blockCounter,
		shares:               shares,
		invalidShares:        invalidShares,
		staleShares:          staleShares,
		cumulativeDifficulty: cumulativeDifficulty,
		lastShareTime:        lastShareTime,
	}

}

// UpdateOrSaveShift updates (if existing) or saves (if new) the shift data
func (s *Shift) UpdateOrSaveShift() error {
	var id uint64
	pool := s.worker.Parent().Pool()
	err := pool.sqldb.QueryRow("SELECT [ShiftID] FROM [ShiftInfo] WHERE [Pool] = ? AND [ShiftID] = ?", pool.id, s.shiftID).Scan(&id)
	if err != nil {
		if err != sql.ErrNoRows {
			pool.log.Debugf("Search failed: %s\n", err)
			return nil
		}
		// This is a new shift, save it
		return s.saveShift()
	}
	// existing shift - update it
	return s.updateShift()
}

func (s *Shift) updateShift() error {
	pool := s.worker.Parent().Pool()
	stmt, err := pool.sqldb.Prepare(`
		UPDATE [ShiftInfo]
		SET [Shares] = ?, [InvalidShares] = ?, [StaleShares] = ?, [CummulativeDifficulty] = ?, [LastShareTime] = ?
		WHERE [ShiftID] = ? AND [Pool] = ?
		`)
	if err != nil {
		s.worker.log.Printf("Error preparing to update shift: %s\n", err)
		return err
	}
	defer stmt.Close()
	_, err = stmt.Exec(s.Shares(), s.Invalid(), s.Stale(), s.CumulativeDifficulty(), s.LastShareTime(), s.ShiftID(), pool.id)
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
		[ShiftInfo]([ShiftID], [Pool], [Parent], [Blocks], [Shares], [InvalidShares], [StaleShares], [CummulativeDifficulty], [LastShareTime])
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?);
		`)
	if err != nil {
		s.worker.log.Printf("Error preparing to end shift: %s\n", err)
		return err
	}
	defer stmt.Close()
	_, err = stmt.Exec(s.ShiftID(), s.Pool(), s.worker.wr.workerID, s.BlockID(),
		s.Shares(), s.Invalid(), s.Stale(), s.CumulativeDifficulty(), s.LastShareTime())
	if err != nil {
		s.worker.log.Printf("%d,%s,%d,%d,%d,%d,%d,%f,%v\n", s.ShiftID(), s.Pool(), s.worker.wr.workerID, s.BlockID(),
			s.Shares(), s.Invalid(), s.Stale(), s.CumulativeDifficulty(), s.LastShareTime())
		s.worker.log.Printf("Error adding record of last shift: %s\n", err)
		return err
	}
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
		INSERT INTO [Worker]([WorkerID], [Name], [Parent], [AverageDifficulty], [BlocksFound])
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
		SELECT [Worker].[WorkerID], [Worker].[AverageDifficulty], [Worker].[BlocksFound] 
		FROM ([Worker] 
		INNER JOIN [Clients] ON [Worker].[Parent]=[Clients].[ClientID])
		WHERE [Worker].[Name] = $1 AND [Clients].[Name] = $2;
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
			//			c.workers[worker] = w
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
		SELECT [Clients].[Name], [Worker].[Name], [ShiftInfo].[Shares], [Worker].[AverageDifficulty],
		[ShiftInfo].[InvalidShares], [ShiftInfo].[StaleShares], [ShiftInfo].[CummulativeDifficulty], 
		[Worker].[BlocksFound], [ShiftInfo].[LastShareTime] 
		FROM (([Worker] 
		INNER JOIN [Clients] ON [Worker].[Parent]=[Clients].[ClientID])
		INNER JOIN [ShiftInfo] ON [Worker].[WorkerID]=[ShiftInfo].[Parent])
		ORDER BY [Clients].[Name], [Worker].[Name], [ShiftInfo].[ShiftID];
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

	cbf := uint64(0)
	var pw []modules.PoolWorkers
	var currentClient, currentWorker string
	var worker modules.PoolWorkers
	var c *Client
	var w *Worker
	var sharesThisBlock, invalidSharesThisBlock, staleSharesThisBlock, blocksFound uint64
	var averageDifficulty, cumulativeDifficulty float64
	var clientName, workerName string
	var shareTime time.Time

	defer rows.Close()
	for rows.Next() {
		err = rows.Scan(&clientName, &workerName, &sharesThisBlock, &averageDifficulty, &invalidSharesThisBlock,
			&staleSharesThisBlock, &cumulativeDifficulty, &blocksFound, &shareTime)
		if err != nil {
			p.log.Printf("Row scan failed: %s\n", err)
			return nil
		}
		if len(currentClient) != 0 && currentClient != clientName {
			// finished with this client
			client := modules.PoolClients{
				ClientName:  currentClient,
				BlocksMined: cbf,
				Workers:     pw,
			}
			pc = append(pc, client)
			cbf = 0
		}
		if currentClient != clientName {
			c = p.Client(clientName)
		}
		if currentWorker != workerName {
			if len(currentWorker) != 0 {
				worker.CurrentDifficulty = averageDifficulty
				if w != nil && w.Online() {
					// add current shift if online
					worker.CumulativeDifficulty += w.CumulativeDifficulty()
					if w.LastShareTime().Unix() > worker.LastShareTime.Unix() {
						worker.LastShareTime = w.LastShareTime()
					}
					worker.CurrentDifficulty = w.CurrentDifficulty()
					worker.SharesThisBlock += w.SharesThisBlock()
					worker.InvalidSharesThisBlock += w.InvalidShares()
					worker.StaleSharesThisBlock += w.StaleShares()

				}
				cbf += worker.BlocksFound
				pw = append(pw, worker)
			}
			if c != nil {
				w = c.Worker(workerName)
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
			currentWorker = workerName
			if c != nil {
				w = c.Worker(workerName)
			} else {
				w = nil
			}
		} else {
			if shareTime.Unix() > worker.LastShareTime.Unix() {
				worker.LastShareTime = shareTime
			}
			worker.CurrentDifficulty = averageDifficulty
			worker.CumulativeDifficulty += cumulativeDifficulty
			worker.SharesThisBlock += sharesThisBlock
			worker.InvalidSharesThisBlock += invalidSharesThisBlock
			worker.StaleSharesThisBlock += staleSharesThisBlock
			worker.BlocksFound = blocksFound
		}
		currentClient = clientName

	}
	if rows.Err() != nil {
		p.log.Printf("Rows.Err failed: %s\n", err)
		return nil
	}
	if len(currentClient) == 0 {
		return nil
	}
	// finished with last worker
	worker.CurrentDifficulty = averageDifficulty
	if w != nil && w.Online() {
		// add current shift if online
		worker.CumulativeDifficulty += w.CumulativeDifficulty()
		if w.LastShareTime().Unix() > worker.LastShareTime.Unix() {
			worker.LastShareTime = w.LastShareTime()
		}
		worker.CurrentDifficulty = w.CurrentDifficulty()
		worker.SharesThisBlock += w.SharesThisBlock()
		worker.InvalidSharesThisBlock += w.InvalidShares()
		worker.StaleSharesThisBlock += w.StaleShares()

	}
	cbf += worker.BlocksFound
	pw = append(pw, worker)

	// finished with last client
	client := modules.PoolClients{
		ClientName:  currentClient,
		BlocksMined: cbf,
		Workers:     pw,
	}
	pc = append(pc, client)

	return pc
}
