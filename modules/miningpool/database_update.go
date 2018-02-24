package pool

import (
	"database/sql"
	"errors"
	"fmt"

	"github.com/go-sql-driver/mysql"
)

const (
	SCHEMA_VERSION_MAJOR = 0
	SCHEMA_VERSION_MINOR = 2
)

func (p *Pool) createOrUpdateDatabase() error {
	p.log.Debugf("Checking sql database version\n")
	err := p.setMySqlSqlMode()
	if err != nil {
		return err
	}

	// try to use the database first - there will be an error if we failed.
	// but we don't need to bother with it, because in the failure case
	// the getSchemaVersion() call will also fail, and so we'll create the
	// database.
	err = p.useDatabase()

	major, minor, err := p.getSchemaVersion()
	if err != nil {
		// assume an error means the database doesn't exist
		p.log.Debugf("Database check failed: %s\n", err.Error())
		err = p.createDatabase()
		if err != nil {
			return err
		}
		major, minor, err = p.getSchemaVersion()
		if err != nil {
			return err
		}
	}
	for !(major == SCHEMA_VERSION_MAJOR && minor == SCHEMA_VERSION_MINOR) {
		if major == 0 && minor == 0 {
			err = p.updateFrom0_0To0_1()
		} else if major == 0 && minor == 1 {
			err = p.updateFrom0_1To0_2()
		} else {
			p.log.Panicf("Unsupport database upgrade needed: Major = %d, Minor = %d\n", major, minor)
		}
		if err != nil {
			return err
		}
		major, minor, err = p.getSchemaVersion()
		if err != nil {
			return err
		}

	}
	return nil
}

func (p *Pool) useDatabase() error {
	name := p.InternalSettings().PoolDBName

	tx, err := p.sqldb.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	_, err = tx.Exec(fmt.Sprintf("USE %s;", name))
	if err != nil {
		return err
	}
	return nil
}

//
// createDatabase really just creates the SchemaVersion table and populates it with starting
// values.  The particular tables and values are created by the schema upgrade function for the
// version that adds them.  For example the initial values are added in the 0.0 to 0.1 upgrade function
//
func (p *Pool) createDatabase() error {
	name := p.InternalSettings().PoolDBName
	p.log.Printf("Creating sql database '%s'\n", name)

	tx, err := p.sqldb.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	_, err = tx.Exec(fmt.Sprintf("DROP DATABASE %s;", name))
	// 1008 = ER_DB_DROP_EXISTS
	// if we had an error on dropping it because the db doesn't exist, that's not a problem.
	// we just move on and create the new database as planned
	// full list of errors here: https://github.com/VividCortex/mysqlerr/blob/master/mysqlerr.go
	if err != nil && err.(*mysql.MySQLError).Number != 1008 {
		return err
	}
	_, err = tx.Exec(fmt.Sprintf("CREATE DATABASE %s;", name))
	if err != nil {
		return err
	}
	_, err = tx.Exec(fmt.Sprintf("USE %s;", name))
	if err != nil {
		return err
	}
	_, err = tx.Exec(`
		CREATE TABLE SchemaVersion
		(
			Major BIGINT NOT NULL,
			Minor BIGINT NOT NULL
		);
	`)
	if err != nil {
		return err
	}
	_, err = tx.Exec(`
		INSERT INTO SchemaVersion
		(Major, Minor)
		VALUES
		(0, 0);
	`)
	if err != nil {
		return err
	}
	err = tx.Commit()
	if err != nil {
		return err
	}
	return nil
}

//
// This is the initial database, so it creates all the tables and/or initial data needed.
// future update functions should be smaller, unless there is a large task to accomplish.
//
func (p *Pool) updateFrom0_0To0_1() error {
	p.log.Printf("Updating sql database 'miningpool' from version 0.0 to 0.1\n")
	tx, err := p.sqldb.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	//
	// Client table represents each individual client.
	//
	_, err = tx.Exec(`
		CREATE TABLE Clients
		(
			ClientID BIGINT PRIMARY KEY,
			Name     VARCHAR(255) NOT NULL,
			Wallet   VARCHAR(255) NOT NULL,
			UNIQUE (Name)
		);
	`)
	if err != nil {
		p.log.Printf("Failed to create table [Clients]: %s\n", err)
		return err
	}

	//
	// Block table represents completed blocks (with the inprogess one being updated on completion)
	//
	_, err = tx.Exec(`
		CREATE TABLE Block
		(
			BlockID BIGINT PRIMARY KEY,
			Height  VARCHAR(255) NOT NULL,
			Reward  VARCHAR(255) NOT NULL,
			Time    TIMESTAMP
		);
	`)
	if err != nil {
		p.log.Printf("Failed to create table [Block]: %s\n", err)
		return err
	}
	// Create the first, in progress, block
	_, err = tx.Exec(`INSERT INTO Block(BlockID, Height, Reward) VALUES (1, 0, 0);`)
	if err != nil {
		return err
	}

	//
	// Worker table represents the worker
	//
	_, err = tx.Exec(`
		CREATE TABLE Worker
		(
			WorkerID          BIGINT PRIMARY KEY,
			Name              VARCHAR(255) NOT NULL,
			Parent            BIGINT,
			AverageDifficulty FLOAT,
			BlocksFound       BIGINT,
			UNIQUE (Name,Parent)
		);
	`)
	if err != nil {
		p.log.Printf("Failed to create table [Worker]: %s\n", err)
		return err
	}

	//
	// Since this table gets updated a lot, and it may be different instances of the pool doing it,
	// we have the pool instance name (probably the ip:port) to keep the records separate and results
	// are merged on reporting, rather than on updating.
	//
	_, err = tx.Exec(`
		CREATE TABLE ShiftInfo
		(
			ShiftID               BIGINT NOT NULL,
			Pool                  VARCHAR(255) NOT NULL,
			Parent                BIGINT,
			Blocks                BIGINT,
			Shares                BIGINT,
			InvalidShares         BIGINT,
			StaleShares           BIGINT,
			CummulativeDifficulty FLOAT,
			LastShareTime         TIMESTAMP,
			ShiftDuration         BIGINT,
			UNIQUE (ShiftID,Pool,Parent)
		);
		`)
	if err != nil {
		p.log.Printf("Failed to create table [ShiftInfo]: %s\n", err)
		return err
	}
	_, err = tx.Exec(`
			CREATE UNIQUE INDEX WorkerBlock ON ShiftInfo(ShiftID,Pool,Parent,Blocks);
	`)
	if err != nil {
		p.log.Printf("Failed to create index WorkerBlock ON [ShiftInfo]: %s\n", err)
		return err
	}

	err = p.setSchemaVersion(tx, 0, 1)
	if err != nil {
		return err
	}
	err = tx.Commit()
	if err != nil {
		return err
	}
	return nil
}

func (p *Pool) updateFrom0_1To0_2() error {
	p.log.Printf("Updating sql database 'miningpool' from version 0.1 to 0.2\n")
	tx, err := p.sqldb.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	//
	// Add paid and timestamp to Block table
	//
	_, err = tx.Exec(`
		ALTER TABLE Block
			ADD Paid  BOOLEAN DEFAULT FALSE NOT NULL;
	`)
	if err != nil {
		p.log.Printf("Failed to alter table [Block]: %s\n", err)
		return err
	}

	_, err = tx.Exec(`
		ALTER TABLE Block
			ADD PaidTime  TIMESTAMP NOT NULL DEFAULT '0000-00-00 00:00:00';
	`)
	if err != nil {
		p.log.Printf("Failed to alter table [Block]: %s\n", err)
		return err
	}

	//
	// Add account balance to Client table.
	//
	_, err = tx.Exec(`
		ALTER TABLE Clients
			ADD Balance  VARCHAR(255) DEFAULT '0' NOT NULL;
	`)
	if err != nil {
		p.log.Printf("Failed to alter table [Clients]: %s\n", err)
		return err
	}

	operatorClient := fmt.Sprintf("INSERT INTO Clients(ClientID, Name, Wallet) VALUES (0, 'Operator', '%s');",
		p.InternalSettings().PoolOperatorWallet.String())
	_, err = tx.Exec(operatorClient)

	if err != nil {
		p.log.Printf("Failed to add Operator to [Clients]: %s\n", err)
		return err
	}
	//
	// Ledger table represents the client balance transactions
	//
	if p.InternalSettings().PoolDBConnection == "" || p.InternalSettings().PoolDBConnection == "internal" {
		_, err = tx.Exec(`
			CREATE TABLE Ledger
			(
				LedgerID    INTEGER PRIMARY KEY AUTOINCREMENT,
				Transaction VARCHAR(255) NOT NULL,
				Timestamp   TIMESTAMP NOT NULL,
				Memo        VARCHAR(255),
				Parent REFERENCES Clients(ClientID)
				);
		`)
	} else {
		_, err = tx.Exec(`
			CREATE TABLE Ledger
			(
				LedgerID    SERIAL PRIMARY KEY,
				Parent      BIGINT,
				Transaction VARCHAR(255) NOT NULL,
				TimeStamp   TIMESTAMP,
				Memo        VARCHAR(255)
			);
		`)
	}
	/* Postgres
	_, err = tx.Exec(`
		CREATE TABLE Ledger
		(
			LedgerID    SERIAL,
			Parent      INT,
			Transaction VARCHAR NOT NULL,
			TimeStamp   TIMESTAMP,
			Memo        VARCHAR
		);
	`)
	*/
	if err != nil {
		p.log.Printf("Failed to create table [Ledger]: %s\n", err)
		return err
	}

	err = p.setSchemaVersion(tx, 0, 2)
	if err != nil {
		return err
	}
	err = tx.Commit()
	if err != nil {
		return err
	}
	return nil
}

func (p *Pool) setSchemaVersion(tx *sql.Tx, major, minor int) error {
	p.log.Debugf("Setting sql database version to %d.%d\n", major, minor)
	stmt, err := tx.Prepare(`
		UPDATE SchemaVersion
		SET Major = ?, Minor = ?;
		`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	res, err := stmt.Exec(major, minor)
	if err != nil {
		return err
	}
	rowCnt, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if rowCnt != 1 {
		return errors.New("Failed to update version")
	}
	return nil
}

func (p *Pool) getSchemaVersion() (major int, minor int, err error) {

	stmt, err := p.sqldb.Prepare(`SELECT Major, Minor from SchemaVersion;`)
	if err != nil {
		return -1, -1, err
	}
	defer stmt.Close()
	major = -2
	minor = -2
	rows, err := stmt.Query()
	if err != nil {
		return -1, -1, err
	}
	defer rows.Close()
	for rows.Next() {
		err = rows.Scan(&major, &minor)
		if err != nil {
			return -1, -1, err
		}
		break
	}
	err = rows.Err()
	if err != nil {
		return -1, -1, err
	}
	p.log.Debugf("getSchemaVersion, Version = %d.%d\n", major, minor)
	return major, minor, err
}

func (p *Pool) setMySqlSqlMode() error {
	_, err := p.sqldb.Exec("SET SQL_MODE = 'STRICT_TRANS_TABLES,ERROR_FOR_DIVISION_BY_ZERO,NO_AUTO_CREATE_USER,NO_ENGINE_SUBSTITUTION';")
	if err != nil {
		return err
	}
	return nil
}
