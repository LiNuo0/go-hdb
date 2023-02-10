package driver

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"os"
	"strconv"
	"strings"
)

// DriverVersion is the version number of the hdb driver.
const DriverVersion = "1.0.7"

// DriverName is the driver name to use with sql.Open for hdb databases.
const DriverName = "hdb"

var clientID = func() string {
	if hostname, err := os.Hostname(); err == nil {
		return strings.Join([]string{strconv.Itoa(os.Getpid()), hostname}, "@")
	}
	return strconv.Itoa(os.Getpid())
}()

// clientType is the information provided to HDB identifying the driver.
// Previously the driver.DriverName "hdb" was used but we should be more specific in providing a unique client type to HANA backend.
const clientType = "go-hdb"

var defaultApplicationName, _ = os.Executable()

// driver singleton instance (do not use directly - use getDriver() instead)
var stdHdbDriver *hdbDriver

func init() {
	// load stats configuration
	if err := loadStatsCfg(); err != nil {
		panic(err) // invalid configuration file
	}
	// create driver
	stdHdbDriver = &hdbDriver{metrics: newMetrics(nil, statsCfg.TimeUpperBounds)}
	// register driver
	sql.Register(DriverName, stdHdbDriver)
}

// driver

// check if driver implements all required interfaces
var (
	_ driver.Driver        = (*hdbDriver)(nil)
	_ driver.DriverContext = (*hdbDriver)(nil)
	_ Driver               = (*hdbDriver)(nil)
)

// Driver enhances a connection with go-hdb specific connection functions.
type Driver interface {
	Name() string    // Name returns the driver name.
	Version() string // Version returns the driver version.
	Stats() *Stats   // Stats returns aggregated driver statistics.
}

// hdbDriver represents the go sql driver implementation for hdb.
type hdbDriver struct {
	metrics *metrics
}

// Open implements the driver.Driver interface.
func (d *hdbDriver) Open(dsn string) (driver.Conn, error) {
	connector, err := NewDSNConnector(dsn)
	if err != nil {
		return nil, err
	}
	return connector.Connect(context.Background())
}

// OpenConnector implements the driver.DriverContext interface.
func (d *hdbDriver) OpenConnector(dsn string) (driver.Connector, error) { return NewDSNConnector(dsn) }

// Name returns the driver name.
func (d *hdbDriver) Name() string { return DriverName }

// Version returns the driver version.
func (d *hdbDriver) Version() string { return DriverVersion }

// Stats returns aggregated driver statistics.
func (d *hdbDriver) Stats() *Stats { return d.metrics.stats() }

// DB represents a driver database and can be used as a replacement for sql.DB.
// It provides all of the sql.DB methods plus additional methods only available for driver.DB.
type DB struct {
	// The embedded sql.DB instance. Please use only the methods of the wrapper (driver.DB).
	// The field is exported to support use cases where a sql.DB object is requested, but please
	// use with care as some of the sql.DB methods (e.g. Close) are redefined in driver.DB.
	*sql.DB
	metrics *metrics
}

// OpenDB opens and returns a database. It also calls the OpenDB method of the sql package and stores an embedded *sql.DB object.
func OpenDB(c *Connector) *DB {
	metrics := newMetrics(stdHdbDriver.metrics, statsCfg.TimeUpperBounds)
	nc := &Connector{
		connAttrs: c.connAttrs,
		authAttrs: c.authAttrs,
		newConn: func(ctx context.Context, connAttrs *connAttrs, authAttrs *authAttrs) (driver.Conn, error) {
			return newConn(ctx, metrics, connAttrs, authAttrs) // use db specific metrics
		},
	}
	return &DB{
		metrics: metrics,
		DB:      sql.OpenDB(nc),
	}
}

// Close closes the DB. It also calls the Close method of the embedded sql.DB.
func (db *DB) Close() error {
	err := db.DB.Close()
	db.metrics.close()
	return err
}

// ExStats returns the extended database statistics.
func (db *DB) ExStats() *Stats { return db.metrics.stats() }
