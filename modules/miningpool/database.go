package pool

import (
	"bytes"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/HyperspaceApp/Hyperspace/types"
)

var (
	// ErrDuplicateUserInDifferentCoin is an error when a address used in
	// different coin
	ErrDuplicateUserInDifferentCoin = errors.New("duplicate user in different coin, you need use a different address")
	// ErrNoUsernameInDatabase is an error when can't find a username in db
	ErrNoUsernameInDatabase = errors.New("user is not found in db")
	// ErrCreateClient is an error when can't create a new client
	ErrCreateClient = errors.New("Error when creating a new client")
)

const (
	sqlReconnectRetry   = 6
	sqlRetryDelay       = 10
	confirmedButUnpaid  = "Confirmed but unpaid"
	poolDiffToBlockDiff = 4295032833.0
)

func (p *Pool) newDbConnection() error {
	dbc := p.InternalSettings().PoolDBConnection
	p.dbConnectionMu.Lock()
	defer p.dbConnectionMu.Unlock()
	var err error

	// to prevent other goroutine reconnect
	if p.sqldb != nil {
		err = p.sqldb.Ping()
		if err == nil {
			return nil
		}
	}

	for i := 0; i < sqlReconnectRetry; i++ {
		fmt.Printf("try to connect mysql: %d\n", i)
		p.sqldb, err = sql.Open("mysql", dbc)
		if err != nil {
			time.Sleep(sqlRetryDelay * time.Second)
			continue
		}

		err = p.sqldb.Ping()
		if err != nil {
			time.Sleep(sqlRetryDelay * time.Second)
			continue
		}
		fmt.Printf("success\n")
		return nil
	}

	return fmt.Errorf("sql reconnect retry time exceeded: %d", sqlReconnectRetry)
}

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

	rs, err := stmt.Exec(CoinID, c.cr.name, CoinSymbol)
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
func (p *Pool) FindClientDB(name string) (*Client, error) {
	c := p.Client(name)
	// if it's in memory, just return a pointer to the copy in memory
	if c != nil {
		// log.Println("found in mem: ", name)
		return c, nil
	}

	var clientID int64
	var Name, Wallet string
	var coinid int

	p.yiilog.Debugf("Searching for %s in existing accounts\n", name)
	err := p.sqldb.QueryRow("SELECT id, username, username, coinid FROM accounts WHERE username = ?", name).Scan(&clientID, &Name, &Wallet, &coinid)
	if err != nil {
		p.yiilog.Debugf("Search failed: %s\n", err)
		return nil, ErrNoUsernameInDatabase
	}
	p.yiilog.Debugf("Account %s found: %d \n", Name, clientID)
	if coinid != CoinID {
		p.yiilog.Debugf(ErrDuplicateUserInDifferentCoin.Error())
		return nil, ErrDuplicateUserInDifferentCoin
	}

	// client was in database but not in memory -
	// find workers and connect them to the in memory copy
	c, err = newClient(p, name)
	if err != nil {
		p.log.Printf("Error when creating a new client %s: %s\n", name, err)
		return nil, ErrCreateClient
	}
	var wallet types.UnlockHash
	wallet.LoadString(Wallet)
	c.SetWallet(wallet)
	c.cr.clientID = clientID

	return c, nil
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

// DeleteAllWorkerRecords deletes all worker records associated with a pool.
// This should be used on pool startup and shutdown to ensure the database
// is clean and isn't storing any worker records for non-connected workers.
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
	w.log.Printf("New block to mine on %d\n", uint64(bh)+1)
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
	_, err = stmt.Exec(bh, b.ID().String(), CoinID, w.Parent().cr.clientID,
		w.wr.workerID, "new", difficulty, timeStamp, CoinAlgo)
	if err != nil {
		return err
	}
	err = tx.Commit()
	if err != nil {
		return err
	}

	return nil
}

// SaveShift periodically saves the shares for a given worker to the db
func (s *Shift) SaveShift() error {
	if len(s.Shares()) == 0 {
		return nil
	}

	worker := s.worker
	client := worker.Parent()
	pool := client.Pool()
	var buffer bytes.Buffer
	buffer.WriteString("INSERT INTO shares(userid, workerid, coinid, valid, difficulty, time, algo, reward, block_difficulty, status, height, share_reward, share_diff) VALUES ")
	for i, share := range s.Shares() {
		if i != 0 {
			buffer.WriteString(",")
		}
		buffer.WriteString(fmt.Sprintf("(%d, %d, %d, %t, %f, %d, '%s', %f, %d, %d, %d, %f, %f)",
			share.userid, share.workerid, CoinID, share.valid, share.difficulty, share.time.Unix(),
			CoinAlgo, share.reward, share.blockDifficulty, 0, share.height, share.shareReward, share.shareDifficulty))
	}
	buffer.WriteString(";")

	rows, err := pool.sqldb.Query(buffer.String())
	if rows != nil {
		rows.Close()
	}
	if err != nil {
		worker.log.Println(buffer.String())
		worker.log.Printf("Error saving shares: %s\n, Try to reconnect", err)
		fmt.Println(err)
		err = pool.newDbConnection()
		if err != nil {
			worker.log.Println(buffer.String())
			worker.log.Printf("Error saving shares: %s\n, Try to reconnect", err)
			fmt.Println(err)
			return err
		}
		rows2, err2 := pool.sqldb.Query(buffer.String())
		if rows2 != nil {
			rows2.Close()
		}
		if err2 != nil {
			worker.log.Println(buffer.String())
			worker.log.Printf("Error adding record of last shift: %s\n", err2)
			return err2
		}
	}

	return nil
}

// addWorkerDB inserts info to workers
func (c *Client) addWorkerDB(w *Worker) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.log.Printf("Adding client %s worker %s to database\n", c.cr.name, w.Name())
	tx, err := c.pool.sqldb.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	// TODO: add ip etc info
	stmt, err := tx.Prepare(`
		INSERT INTO workers (userid, name, worker, algo, time, pid, version, ip)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?);
	`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	rs, err := stmt.Exec(c.cr.clientID, c.cr.name, w.wr.name, CoinAlgo, time.Now().Unix(),
		c.pool.InternalSettings().PoolID, w.s.clientVersion, w.s.remoteAddr)
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

func (p *Pool) logLuckState() {
	if !p.InternalSettings().Luck {
		return
	}
	if p.computingLuck {
		return
	}

	p.lucklock.Lock()
	p.computingLuck = true
	defer func() {
		p.computingLuck = false
		p.lucklock.Unlock()
	}()

	// lock
	var lastLogHeight sql.NullInt64
	err := p.sqldb.QueryRow("SELECT max(height) as maxheight FROM luck WHERE coinid = ?",
		CoinID).Scan(&lastLogHeight)
	if err != nil && err != sql.ErrNoRows {
		p.yiilog.Printf("logLuckState failed: %s\n", err)
		return
	}
	p.yiilog.Printf("logLuckState lastLogHeight: %d\n", lastLogHeight.Int64)

	for i := lastLogHeight.Int64 + 1; i <= int64(p.cs.Height()); i++ {
		// if time over 24h, skip
		p.yiilog.Printf("logLuckState Start for: %d\n", i)
		preBlock, ok := p.cs.BlockAtHeight(types.BlockHeight(i - 1))
		if !ok {
			p.yiilog.Printf("logLuckState can't fetch block: %d\n", i-1)
			continue
		}
		startTime := preBlock.Timestamp
		if time.Now().Unix()-int64(startTime) > 86400 { // we only have 24h shares
			p.yiilog.Printf("logLuckState skip block, block too old: %d\n", i)
			continue
		}

		var poolDifficulty sql.NullFloat64
		var poolDiffShareCount int64
		err := p.sqldb.QueryRow("SELECT sum(difficulty), count(*) FROM shares WHERE coinid = ? AND height = ? AND valid=1",
			CoinID, i).Scan(&poolDifficulty, &poolDiffShareCount)
		if err != nil && err != sql.ErrNoRows {
			p.yiilog.Printf("logLuckState failed: %s\n", err)
			return
		}
		if poolDiffShareCount == 0 {
			p.yiilog.Printf("logLuckState no share for this block\n", err)
			continue
		}

		p.yiilog.Printf("logLuckState poolDifficulty: %f\n", poolDifficulty.Float64)

		var lastMineBlock sql.NullInt64 // search our pool block just before current height
		err = p.sqldb.QueryRow("SELECT max(height) as height FROM blocks WHERE coin_id = ? AND height < ?",
			CoinID, i).Scan(&lastMineBlock)
		if err != nil && err != sql.ErrNoRows {
			p.yiilog.Printf("logLuckState lastMineBlock failed: %s\n", err)
			return
		}
		p.yiilog.Printf("logLuckState lastMineBlock: %d\n", lastMineBlock.Int64)

		var sumHistoryDiff sql.NullFloat64
		err = p.sqldb.QueryRow("SELECT sum(diff) FROM luck WHERE coinid = ? AND height > ?",
			CoinID, lastMineBlock.Int64).Scan(&sumHistoryDiff)
		if err != nil && err != sql.ErrNoRows {
			p.yiilog.Printf("logLuckState sumHistoryDiff failed: %s\n", err)
			return
		}
		totalSumDiff := sumHistoryDiff.Float64 + poolDifficulty.Float64

		blockTarget, _ := p.cs.ChildTarget(preBlock.ID())
		blockTargetDifficulty, _ := blockTarget.Difficulty().Uint64()
		blockPoolDifficulty := float64(blockTargetDifficulty) / float64(poolDiffToBlockDiff)

		tx, err := p.sqldb.Begin()
		if err != nil {
			p.yiilog.Printf("logLuckState failed: %s\n", err)
			return
		}
		defer tx.Rollback()

		stmt, err := tx.Prepare(`
			INSERT INTO luck (height, coinid, diff, sumdiff, blockdiff, luck)
			VALUES (?, ?, ?, ?, ?, ?);
		`)
		if err != nil {
			p.yiilog.Printf("logLuckState failed: %s\n", err)
			return
		}
		defer stmt.Close()

		rs, err := stmt.Exec(i, CoinID, poolDifficulty, totalSumDiff,
			blockPoolDifficulty, totalSumDiff/blockPoolDifficulty)
		if err != nil {
			p.yiilog.Printf("logLuckState failed: %s\n", err)
			return
		}
		err = tx.Commit()
		if err != nil {
			p.yiilog.Printf("logLuckState failed: %s\n", err)
			return
		}
		id, err := rs.LastInsertId()
		p.yiilog.Printf("logLuckState inserted luck log: %d, height: %d, coin: %d", id, i, CoinID)
	}
}
