package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"go.podman.io/storage/pkg/ioutils"
	"go.podman.io/storage/pkg/lockfile"
)

const connectionsFile = "podman-connections.json"

// connectionsConfigFile returns the path to the rw connections config file.
func connectionsConfigFile() (string, error) {
	if path, found := os.LookupEnv("PODMAN_CONNECTIONS_CONF"); found {
		return path, nil
	}
	path, err := userConfigPath()
	if err != nil {
		return "", err
	}
	// file is stored next to containers.conf
	return filepath.Join(filepath.Dir(path), connectionsFile), nil
}

type ConnectionConfig struct {
	Default     string                 `json:",omitempty"`
	Connections map[string]Destination `json:",omitempty"`
}

type ConnectionsFile struct {
	Connection ConnectionConfig `json:",omitempty"`
	Farm       FarmConfig       `json:",omitempty"`
}

type Connection struct {
	// Name of the connection
	Name string

	// Destination for this connection
	Destination

	// Default if this connection is the default
	Default bool

	// ReadWrite if true the connection is stored in the connections file
	ReadWrite bool
}

type Farm struct {
	// Name of the farm
	Name string

	// Connections
	Connections []string

	// Default if this is the default farm
	Default bool

	// ReadWrite if true the farm is stored in the connections file
	ReadWrite bool
}

func readConnectionConf(path string) (*ConnectionsFile, error) {
	conf := new(ConnectionsFile)
	f, err := os.Open(path)
	if err != nil {
		// return empty config if file does not exists
		if errors.Is(err, fs.ErrNotExist) {
			return conf, nil
		}

		return nil, err
	}
	defer f.Close()

	err = json.NewDecoder(f).Decode(conf)
	if err != nil {
		return nil, fmt.Errorf("parse %q: %w", path, err)
	}
	return conf, nil
}

func writeConnectionConf(path string, conf *ConnectionsFile) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}

	opts := &ioutils.AtomicFileWriterOptions{ExplicitCommit: true}
	configFile, err := ioutils.NewAtomicFileWriterWithOpts(path, 0o644, opts)
	if err != nil {
		return err
	}
	defer configFile.Close()

	err = json.NewEncoder(configFile).Encode(conf)
	if err != nil {
		return err
	}

	// If no errors commit the changes to the config file
	return configFile.Commit()
}

// EditConnectionConfig must be used to edit the connections config.
// The function will read and write the file automatically and the
// callback function just needs to modify the cfg as needed.
func EditConnectionConfig(callback func(cfg *ConnectionsFile) error) error {
	path, err := connectionsConfigFile()
	if err != nil {
		return err
	}

	lockPath := path + ".lock"
	lock, err := lockfile.GetLockFile(lockPath)
	if err != nil {
		return fmt.Errorf("obtain lock file: %w", err)
	}
	lock.Lock()
	defer lock.Unlock()

	conf, err := readConnectionConf(path)
	if err != nil {
		return fmt.Errorf("read connections file: %w", err)
	}
	if conf.Farm.List == nil {
		conf.Farm.List = make(map[string][]string)
	}

	if err := callback(conf); err != nil {
		return err
	}

	return writeConnectionConf(path, conf)
}

func makeConnection(name string, dst Destination, def, readWrite bool) *Connection {
	return &Connection{
		Name:        name,
		Destination: dst,
		Default:     def,
		ReadWrite:   readWrite,
	}
}

// GetConnection return the connection for the given name or if def is set to true then return the default connection.
func (c *Config) GetConnection(name string, def bool) (*Connection, error) {
	path, err := connectionsConfigFile()
	if err != nil {
		return nil, err
	}
	conConf, err := readConnectionConf(path)
	if err != nil {
		return nil, err
	}
	defaultCon := conConf.Connection.Default
	if defaultCon == "" {
		defaultCon = c.Engine.ActiveService
	}
	if def {
		if defaultCon == "" {
			return nil, errors.New("no default connection found")
		}
		name = defaultCon
	} else {
		def = defaultCon == name
	}

	if dst, ok := conConf.Connection.Connections[name]; ok {
		return makeConnection(name, dst, def, true), nil
	}
	if dst, ok := c.Engine.ServiceDestinations[name]; ok {
		return makeConnection(name, dst, def, false), nil
	}
	return nil, fmt.Errorf("connection %q not found", name)
}

// GetAllConnections return all configured connections.
func (c *Config) GetAllConnections() ([]Connection, error) {
	path, err := connectionsConfigFile()
	if err != nil {
		return nil, err
	}
	conConf, err := readConnectionConf(path)
	if err != nil {
		return nil, err
	}

	defaultCon := conConf.Connection.Default
	if defaultCon == "" {
		defaultCon = c.Engine.ActiveService
	}

	connections := make([]Connection, 0, len(conConf.Connection.Connections))
	for name, dst := range conConf.Connection.Connections {
		def := defaultCon == name
		connections = append(connections, *makeConnection(name, dst, def, true))
	}
	for name, dst := range c.Engine.ServiceDestinations {
		if _, ok := conConf.Connection.Connections[name]; ok {
			// connection name is overwritten by connections file
			continue
		}
		def := defaultCon == name
		connections = append(connections, *makeConnection(name, dst, def, false))
	}

	return connections, nil
}

func getConnections(cons []string, dests map[string]Destination) ([]Connection, error) {
	connections := make([]Connection, 0, len(cons))
	for _, name := range cons {
		if dst, ok := dests[name]; ok {
			connections = append(connections, *makeConnection(name, dst, false, false))
		} else {
			return nil, fmt.Errorf("connection %q not found", name)
		}
	}
	return connections, nil
}

// GetFarmConnections return all the connections for the given farm.
func (c *Config) GetFarmConnections(name string) ([]Connection, error) {
	_, cons, err := c.getFarmConnections(name, false)
	return cons, err
}

// GetDefaultFarmConnections returns the name of the default farm
// and the connections.
func (c *Config) GetDefaultFarmConnections() (string, []Connection, error) {
	return c.getFarmConnections("", true)
}

// getFarmConnections returns all connections for the given farm,
// if def is true it will use the default farm instead of the name.
// Returns the name of the farm and the connections for it.
func (c *Config) getFarmConnections(name string, def bool) (string, []Connection, error) {
	path, err := connectionsConfigFile()
	if err != nil {
		return "", nil, err
	}
	conConf, err := readConnectionConf(path)
	if err != nil {
		return "", nil, err
	}
	defaultFarm := conConf.Farm.Default
	if defaultFarm == "" {
		defaultFarm = c.Farms.Default
	}
	if def {
		if defaultFarm == "" {
			return "", nil, errors.New("no default farm found")
		}
		name = defaultFarm
	}

	if cons, ok := conConf.Farm.List[name]; ok {
		cons, err := getConnections(cons, conConf.Connection.Connections)
		return name, cons, err
	}
	if cons, ok := c.Farms.List[name]; ok {
		cons, err := getConnections(cons, c.Engine.ServiceDestinations)
		return name, cons, err
	}
	return "", nil, fmt.Errorf("farm %q not found", name)
}

func makeFarm(name string, cons []string, def, readWrite bool) Farm {
	return Farm{
		Name:        name,
		Connections: cons,
		Default:     def,
		ReadWrite:   readWrite,
	}
}

// GetAllFarms returns all configured farms.
func (c *Config) GetAllFarms() ([]Farm, error) {
	path, err := connectionsConfigFile()
	if err != nil {
		return nil, err
	}
	conConf, err := readConnectionConf(path)
	if err != nil {
		return nil, err
	}
	defaultFarm := conConf.Farm.Default
	if defaultFarm == "" {
		defaultFarm = c.Farms.Default
	}

	farms := make([]Farm, 0, len(conConf.Farm.List))
	for name, cons := range conConf.Farm.List {
		def := defaultFarm == name
		farms = append(farms, makeFarm(name, cons, def, true))
	}
	for name, cons := range c.Farms.List {
		if _, ok := conConf.Farm.List[name]; ok {
			// farm name is overwritten by connections file
			continue
		}
		def := defaultFarm == name
		farms = append(farms, makeFarm(name, cons, def, false))
	}

	return farms, nil
}
