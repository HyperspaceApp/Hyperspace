package pool

import (
	"bytes"
	"fmt"
	"time"

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
	c.SetWallet(wallet)
	c.cr.clientID = clientID

	return c
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
		buffer.WriteString(fmt.Sprintf("(%d, %d, %d, %t, %f, %d, '%s', %f, %f, %d, %d, %f, %f)",
			share.userid, share.workerid, SiaCoinID, share.valid, share.difficulty, share.time.Unix(),
			SiaCoinAlgo, share.reward, share.blockDifficulty, 0, share.height, share.shareReward, share.shareDifficulty))
	}
	buffer.WriteString(";")

	rows, err := pool.sqldb.Query(buffer.String())
	defer rows.Close()
	if err != nil {
		worker.log.Println(buffer.String())
		worker.log.Printf("Error saving shares: %s\n", err)
		fmt.Println(err)
		return err
	}

	if err != nil {
		worker.log.Println(buffer.String())
		worker.log.Printf("Error adding record of last shift: %s\n", err)
		return err
	}
	// worker.log.Debugf(buffer.String())
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

	rs, err := stmt.Exec(c.cr.clientID, c.cr.name, w.wr.name, SiaCoinAlgo, time.Now().Unix(),
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
