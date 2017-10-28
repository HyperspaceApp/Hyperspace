package pool

import (
	"fmt"
	"time"

	"github.com/NebulousLabs/Sia/modules"
	"github.com/NebulousLabs/Sia/types"
	sqlite3 "github.com/mattn/go-sqlite3"
)

func (p *Pool) AddClient(c *Client) error {
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

	_, err = stmt.Exec(c.clientID, c.name, c.name)
	if err != nil {
		return err
	}

	err = tx.Commit()
	if err != nil {
		return err
	}

	return nil
}

func (p *Pool) findClient(name string) *Client {
	var clientID uint64
	var Name, Wallet string
	// p.log.Printf("Searching database for client: %s\n", name)
	err := p.sqldb.QueryRow("SELECT [ClientID], [Name], [Wallet] FROM [Clients] WHERE [Name] = ?", name).Scan(&clientID, &Name, &Wallet)
	if err != nil {
		p.log.Debugf("Search failed: %s\n", err)
		return nil
	}
	c, err := newClient(p, Name)
	if err != nil {
		return nil
	}
	var wallet types.UnlockHash
	wallet.LoadString(Wallet)
	c.addWallet(wallet)
	c.clientID = clientID
	qs := fmt.Sprintf("SELECT [Name] FROM [Worker] WHERE [Parent] = %d;", clientID)
	stmt, err := p.sqldb.Prepare(qs)
	if err != nil {
		p.log.Printf("Error finding workers for client %s: %s\n", name, err)
		return nil
	}
	defer stmt.Close()
	rows, err := stmt.Query()
	if err != nil {
		p.log.Printf("Error finding workers for client %s: %s\n", name, err)
		return nil
	}
	defer rows.Close()
	var value string
	for rows.Next() {
		err = rows.Scan(&value)
		if err != nil {
			p.log.Printf("Error finding workers for client %s: %s\n", name, err)
			return nil
		}
		if c.Worker(value) == nil {
			w, _ := newWorker(c, value)
			c.addWorker(w)
		}
	}
	err = rows.Err()
	if err != nil {
		p.log.Printf("Error finding workers for client %s: %s\n", name, err)
		return nil
	}

	return c
}

func (w *Worker) setFieldValue(field string, value interface{}) {
	err := w.parent.pool.addIfNeededBlockCounter()
	if err != nil {
		w.log.Printf("Failed to add block record: %s\n", err)
		return
	}
	err = w.addIfNeededWorkerRecord()
	if err != nil {
		w.log.Printf("Failed to add worker record: %s\n", err)
		return
	}
	tx, err := w.parent.pool.sqldb.Begin()
	if err != nil {
		w.log.Printf("Failed to set field %s to %v: %s\n", field, value, err)
		return
	}
	defer tx.Rollback()
	query := fmt.Sprintf("UPDATE [Worker] SET [%s] = %v WHERE [WorkerID] = $1 AND [Blocks] = $2;",
		field, value)
	stmt, err := tx.Prepare(query)
	if err != nil {
		w.log.Printf("Failed to set field %s to %v: %s\n", field, value, err)
		return
	}
	defer stmt.Close()
	res, err := stmt.Exec(w.workerID, w.parent.pool.blockCounter)
	if err != nil {
		driverErr := err.(sqlite3.Error)

		w.log.Printf("SQLite Error is %d\n", driverErr.Code)
		if driverErr.Code == sqlite3.ErrLocked {
			// Handle the permission-denied error
		}

		w.log.Printf("Failed to set field %s to %v: %s\n", field, value, err)
		return
	}
	rowCnt, err := res.RowsAffected()
	if err != nil {
		w.log.Printf("Failed to set field %s to %v: %s\n", field, value, err)
		return
	}
	if rowCnt != 1 {
		w.log.Printf("Failed to set field %s to %v: %s\n", field, value, err)
		return
	}

	tx.Commit()

}

func (w *Worker) incrementFieldValue(field string) {
	err := w.parent.pool.addIfNeededBlockCounter()
	if err != nil {
		w.log.Printf("Failed to add block record: %s\n", err)
		return
	}
	err = w.addIfNeededWorkerRecord()
	if err != nil {
		w.log.Printf("Failed to add worker record: %s\n", err)
		return
	}

	tx, err := w.parent.pool.sqldb.Begin()
	if err != nil {
		w.log.Printf("Failed to increment field %s: %s\n", field, err)
		return
	}
	defer tx.Rollback()
	query := fmt.Sprintf("SELECT [%s] FROM [Worker] WHERE [WorkerID] = $1 AND [Blocks] = $2;", field)

	var value uint64
	err = tx.QueryRow(query, w.workerID, w.parent.pool.blockCounter).Scan(&value)
	if err != nil {
		driverErr := err.(sqlite3.Error)

		w.log.Printf("SQLite Error is %d\n", driverErr.Code)
		w.log.Printf("Failed to increment field %s: %s\n", field, err)
		return
	}
	query = fmt.Sprintf("UPDATE [Worker] SET [%s] = %d WHERE [WorkerID] = $1 AND [Blocks] = $2;",
		field, value+1)
	stmt, err := tx.Prepare(query)
	if err != nil {
		w.log.Printf("Failed to prepare query %s: %s\n", query, err)
		return
	}
	defer stmt.Close()
	res, err := stmt.Exec(w.workerID, w.parent.pool.blockCounter)
	if err != nil {
		driverErr := err.(sqlite3.Error)

		w.log.Printf("SQLite Error is %d\n", driverErr.Code)
		w.log.Printf("Failed to increment field %s: %s\n", field, err)
		return
	}
	rowCnt, err := res.RowsAffected()
	if err != nil {
		w.log.Printf("Failed to increment field %s: %s\n", field, err)
		return
	}
	if rowCnt != 1 {
		w.log.Printf("Failed to increment field - rows affected: %d\n", rowCnt)
		return
	}
	tx.Commit()
}

func (w *Worker) getUint64Field(field string) uint64 {
	err := w.parent.pool.addIfNeededBlockCounter()
	if err != nil {
		w.log.Printf("Failed to get uint64 field %s: %s\n", field, err)
		return 0
	}

	query := fmt.Sprintf("SELECT [%s] FROM [Worker] WHERE [WorkerID] = $1 AND [Blocks] = %d;", field, w.parent.pool.blockCounter)

	var value uint64
	err = w.parent.pool.sqldb.QueryRow(query, w.workerID).Scan(&value)
	if err != nil {
		value = 0
	}
	return value
}

func (w *Worker) getFloatField(field string) float64 {
	err := w.parent.pool.addIfNeededBlockCounter()
	if err != nil {
		w.log.Printf("Failed to get float field %s: %s\n", field, err)
		return 0.0
	}

	query := fmt.Sprintf("SELECT [%s] FROM [Worker] WHERE [WorkerID] = $1 AND [Blocks] = %d;", field, w.parent.pool.blockCounter)

	var value float64
	err = w.parent.pool.sqldb.QueryRow(query, w.workerID).Scan(&value)
	if err != nil {
		value = 0.0
	}
	return value
}

func (w *Worker) addFoundBlock(b *types.Block) error {
	tx, err := w.parent.pool.sqldb.Begin()
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

	bh := w.parent.pool.persist.GetBlockHeight()
	sub := b.CalculateSubsidy(bh).String()
	w.log.Printf("Adding found block %d, height %d, subsidy %s: err %v\n", w.parent.pool.blockCounter, bh, sub, err)
	_, err = stmt.Exec(bh, sub, time.Now().Unix(), w.parent.pool.blockCounter)
	if err != nil {
		return err
	}

	err = tx.Commit()
	if err != nil {
		return err
	}

	return nil
}

func (p *Pool) addIfNeededBlockCounter() error {
	var count uint64
	err := p.sqldb.QueryRow("SELECT [BlockID] FROM [Block] WHERE [BlockID] = $1;", p.blockCounter).Scan(&count)
	if err == nil {
		return err
	}
	tx, err := p.sqldb.Begin()
	if err != nil {
		return err
	}

	defer tx.Rollback()

	_, err = tx.Exec(`INSERT INTO [Block]([BlockID], [Height], [Reward], [Time]) VALUES ($1, 0, "0", 0);`, p.blockCounter)
	if err != nil {
		return err
	}
	p.log.Printf("New candidate block added %d\n", p.blockCounter)
	tx.Commit()
	return nil
}

func (w *Worker) addIfNeededWorkerRecord() error {

	var id uint64
	err := w.parent.pool.sqldb.QueryRow("SELECT [WorkerID] FROM [Worker] WHERE [WorkerID] = $1 AND [Blocks] = $2;",
		w.workerID, w.parent.pool.blockCounter).Scan(&id)
	if err == nil {
		return err
	}
	err = w.parent.pool.addIfNeededBlockCounter()
	if err != nil {
		w.log.Printf("Failed to add new block: %s\n", err)
		return err
	}

	// At this point we need the record
	tx, err := w.parent.pool.sqldb.Begin()
	if err != nil {
		return err
	}

	defer tx.Rollback()

	w.log.Debugf("Need new worker record for %s, %d\n", w.name, w.parent.pool.blockCounter)
	stmt, err := tx.Prepare(`
			INSERT INTO [Worker]([WorkerID], [Name], [Parent], [Blocks], [SharesThisBlock], 
				[InvalidSharesThisBlock], [StaleSharesThisBlock], 
				[CumulativeDifficulty], [BlocksFound], [LastShareTime])
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?);
		`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	_, err = stmt.Exec(w.workerID, w.name, w.parent.clientID, w.parent.pool.blockCounter, 0, 0, 0, 0.0, 0, 0)
	if err != nil {
		return err
	}

	err = tx.Commit()
	return err
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
	p.blockCounter = uint64(value)
	p.log.Debugf("setBlockCounterFromDB, Count = %d\n", value)
	return nil
}

func (c *Client) addWorker(w *Worker) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.workers[w.Name()] = w
	c.log.Printf("Adding client %s worker %s to database\n", c.name, w.Name())
	//	p.clients[c.Name()] = c
	c.pool.addIfNeededBlockCounter()
	tx, err := c.pool.sqldb.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	stmt, err := tx.Prepare(`
		INSERT INTO [Worker]([WorkerID], [Name], [Parent], [Blocks], 
			[SharesThisBlock], [InvalidSharesThisBlock], [StaleSharesThisBlock], 
			[CumulativeDifficulty], [BlocksFound], [LastShareTime])
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?);
	`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	_, err = stmt.Exec(w.workerID, w.name, c.clientID, c.pool.blockCounter, 0, 0, 0, 0.0, 0, 0)
	if err != nil {
		return err
	}

	err = tx.Commit()
	if err != nil {
		return err
	}

	return nil
}

func (c *Client) findWorker(worker string) error {
	var id uint64
	err := c.pool.sqldb.QueryRow("SELECT [WorkerID] FROM [Worker] WHERE [Name] = $1 AND [Blocks] = $2;",
		worker, c.pool.blockCounter).Scan(&id)
	if err == nil {
		w, err := newWorker(c, worker)
		if err == nil {
			c.mu.Lock()
			defer c.mu.Unlock()
			w.workerID = id
			c.workers[worker] = w
		}
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
		SELECT [Clients].[Name], [Worker].[Name], [Worker].[Blocks], [Worker].[SharesThisBlock], 
		[Worker].[InvalidSharesThisBlock], [Worker].[StaleSharesThisBlock], [Worker].[CumulativeDifficulty], 
		[Worker].[BlocksFound], [Worker].[LastShareTime] 
		FROM [Worker] 
		INNER JOIN [Clients] ON [Worker].[Parent]=[Clients].[ClientID]
		ORDER BY [Clients].[Name], [Worker].[Name], [Worker].[Blocks];
	`)
	if err != nil {
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

	defer rows.Close()
	for rows.Next() {
		var blocks, sharesThisBlock, invalidSharesThisBlock, staleSharesThisBlock, blocksFound uint64
		var cumulativeDifficulty float64
		var clientName, workerName string
		var shareTime time.Time
		err = rows.Scan(&clientName, &workerName, &blocks, &sharesThisBlock, &invalidSharesThisBlock,
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
			c = p.findClient(clientName)
			if c == nil {
				continue
			}
		}
		if currentWorker != workerName {
			if len(currentWorker) != 0 {
				cbf += worker.BlocksFound
				worker.CurrentDifficulty = w.CurrentDifficulty()
				pw = append(pw, worker)
			}
			worker = modules.PoolWorkers{
				WorkerName:             workerName,
				LastShareTime:          shareTime,
				CumulativeDifficulty:   cumulativeDifficulty,
				SharesThisBlock:        sharesThisBlock,
				InvalidSharesThisBlock: invalidSharesThisBlock,
				StaleSharesThisBlock:   staleSharesThisBlock,
				BlocksFound:            blocksFound,
			}
			currentWorker = workerName
			w = c.Worker(workerName)
		} else {
			if shareTime.Unix() > worker.LastShareTime.Unix() {
				worker.LastShareTime = shareTime
			}
			worker.CumulativeDifficulty += cumulativeDifficulty
			worker.SharesThisBlock = sharesThisBlock // last block will be current block
			worker.InvalidSharesThisBlock = invalidSharesThisBlock
			worker.StaleSharesThisBlock = staleSharesThisBlock
			worker.BlocksFound += blocksFound
		}
		currentClient = clientName

	}
	if rows.Err() != nil || len(currentClient) == 0 {
		return nil
	}
	// finished with last worker
	cbf += worker.BlocksFound
	worker.CurrentDifficulty = w.CurrentDifficulty()
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
