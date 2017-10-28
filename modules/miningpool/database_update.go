package pool

import (
	"database/sql"
	"errors"
)

const (
	SCHEMA_VERSION_MAJOR = 0
	SCHEMA_VERSION_MINOR = 1
)

func (p *Pool) createOrUpdateDatabase() error {
	p.log.Debugf("Checking sql database version\n")
	err := p.setSqliteForeignKey()
	if err != nil {
		return err
	}
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

//
// createDatabase really just creates the SChemaVersion table and populates it with starting
// values.  The particular tables and values are created by the schema upgrade function for the
// version that adds them.  For example the initial values are added in the 0.0 to 0.1 upgrade function
//
func (p *Pool) createDatabase() error {
	p.log.Printf("Creating sql database 'miningpool'\n")

	tx, err := p.sqldb.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	_, err = tx.Exec(`
		CREATE TABLE [SchemaVersion]
		(
			[Major] INT NOT NULL,
			[Minor] INT NOT NULL
		);
	`)
	if err != nil {
		return err
	}
	_, err = tx.Exec(`
		INSERT INTO [SchemaVersion]
		([MAJOR], [MINOR])
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

	_, err = tx.Exec(`
		CREATE TABLE [Clients]
		(
			[ClientID] INT PRIMARY KEY,
			[Name] VARCHAR NOT NULL,
			[Wallet] VARCHAR NOT NULL
		);
	`)
	if err != nil {
		return err
	}

	_, err = tx.Exec(`
		CREATE TABLE [Block]
		(
			[BlockID] INT PRIMARY KEY,
			[Height] VARCHAR NOT NULL,
			[Reward] VARCHAR NOT NULL,
			[Time] TIMESTAMP
			);
	`)
	if err != nil {
		return err
	}
	_, err = tx.Exec(`INSERT INTO [Block]([BlockID], [Height], [Reward], [Time]) VALUES (1, 0, "0", 0);`)
	if err != nil {
		return err
	}

	_, err = tx.Exec(`
		CREATE TABLE [Worker]
		(
			[WorkerID] INT,
			[Name] VARCHAR NOT NULL,
			[Parent] REFERENCES [Clients]([ClientID]),
			[Blocks] REFERENCES [Block]([BlockID]),
			[SharesThisBlock]        INT,
			[InvalidSharesThisBlock] INT,
			[StaleSharesThisBlock]   INT,
			[CumulativeDifficulty]   FLOAT,
			[BlocksFound]            INT,
			[LastShareTime]          TIMESTAMP		
			);
	`)
	if err != nil {
		return err
	}
	_, err = tx.Exec(`
		CREATE TABLE [Shares]
		(
			[ShareID] INT PRIMARY KEY,
			[Diff] FLOAT NOT NULL,
			[DateTime] TIMESTAMP NOT NULL,
			[Worker] REFERENCES [Worker]([WorkerID])
		);
	`)
	if err != nil {
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

func (p *Pool) setSchemaVersion(tx *sql.Tx, major, minor int) error {
	p.log.Debugf("Setting sql database version to %d.%d\n", major, minor)
	stmt, err := tx.Prepare(`
		UPDATE [SchemaVersion]
		SET [Major] = $1, [Minor] = $2;
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

	stmt, err := p.sqldb.Prepare(`SELECT [Major], [Minor] from [SchemaVersion];`)
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

func (p *Pool) setSqliteForeignKey() error {
	_, err := p.sqldb.Exec("PRAGMA foreign_keys = ON;")
	if err != nil {
		return err
	}
	var value int
	err = p.sqldb.QueryRow("PRAGMA foreign_keys;").Scan(&value)
	if err != nil {
		return err
	}
	if value != 1 {
		return errors.New("Unable to turn on foreign_keys")
	}
	return nil
}
