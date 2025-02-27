package server

import (
	"context"
	"encoding/json"
	"expvar"
	"io"
	"log"
	"os"
	"path/filepath"
	"sync"

	goipam "github.com/metal-stack/go-ipam"

	wireguardv1 "github.com/clly/wireguard-cni/gen/wgcni/wireguard/v1"
)

type Server struct {
	wgKey     *mapDB
	expvarMap *expvar.Map
	prefix    *goipam.Prefix
	ipam      goipam.Ipamer
	mode      IPAM_MODE
}

type serverConfig struct {
	mode             IPAM_MODE
	self             *wireguardv1.Peer
	wireguardDataDir string
}

type newServerOpt func(cfg *serverConfig)

func WithNodeConfig(self *wireguardv1.Peer) newServerOpt {
	return func(cfg *serverConfig) {
		cfg.mode = NODE_MODE
		cfg.self = self
	}
}

func WithDataDir(d string) newServerOpt {
	return func(cfg *serverConfig) {
		cfg.wireguardDataDir = d
	}
}

func NewServer(cidr string, opt ...newServerOpt) (*Server, error) {
	wireguardExpvar.Init()

	ipam := goipam.New()

	prefix, err := ipam.NewPrefix(context.TODO(), cidr)
	if err != nil {
		return nil, err
	}

	once.Do(func() {
		expvar.Publish("ipam-usage", expvar.Func(ipamUsage(ipam, prefix.Cidr)))
	})

	var cfg = serverConfig{
		mode: CLUSTER_MODE,
	}
	for _, o := range opt {
		o(&cfg)
	}

	mapDBOpts := make([]MapDbOpt, 0, 1)
	if cfg.wireguardDataDir != "" {
		var filename string
		switch cfg.mode {
		case CLUSTER_MODE:
			filename = clusterWireguardFile
		case NODE_MODE:
			filename = nodeWireguardFile
		}

		mapDBOpts = append(mapDBOpts, WithJSONDB(cfg.wireguardDataDir, filename))
	}
	m, err := newMapDB(mapDBOpts...)
	if err != nil {
		return nil, err
	}

	svr := &Server{
		wgKey:     m,
		expvarMap: wireguardExpvar,
		prefix:    prefix,
		mode:      cfg.mode,
		ipam:      ipam,
	}

	if cfg.self != nil {
		if err = svr.registerWGKey(cfg.self.PublicKey, &wireguardv1.RegisterRequest{
			PublicKey: cfg.self.GetPublicKey(),
			Endpoint:  cfg.self.Endpoint,
			Route:     cfg.self.Route,
		}); err != nil {
			return nil, err
		}
	}

	return svr, nil
}

type MapDbOpt func(*mapDB) error

const nodeWireguardFile = "node-wireguard.json"
const clusterWireguardFile = "cluster-wireguard.json"

func WithJSONDB(dataDir, filename string) MapDbOpt {
	return func(db *mapDB) error {
		if err := os.MkdirAll(dataDir, 0755); err != nil {
			return err
		}
		db.persistFile = filepath.Join(dataDir, filename)
		return nil
	}
}

type mapDB struct {
	db          map[string]string
	m           *sync.RWMutex
	writeSignal chan struct{}
	persistFile string
}

func newMapDB(opt ...MapDbOpt) (*mapDB, error) {
	writeSignal := make(chan struct{}, 1)
	m := &mapDB{
		db:          map[string]string{},
		m:           &sync.RWMutex{},
		writeSignal: writeSignal,
	}
	for _, o := range opt {
		err := o(m)
		if err != nil {
			return nil, err
		}
	}

	go m.persist()

	return m, nil
}

func (m *mapDB) Set(k string, v string) {
	m.m.Lock()
	m.db[k] = v
	m.writeSignal <- struct{}{}
	m.m.Unlock()
}

func (m *mapDB) Get(k string) (val string, ok bool) {
	m.m.RLock()
	val, ok = m.db[k]
	m.m.RUnlock()
	return val, ok
}

func (m *mapDB) List() []string {
	m.m.RLock()
	peers := make([]string, 0, len(m.db))
	for _, v := range m.db {
		peers = append(peers, v)
	}
	m.m.RUnlock()
	return peers
}

func (m *mapDB) persist() {
	for {
		<-m.writeSignal
		if m.persistFile == "" {
			return
		}
		f, err := os.OpenFile(m.persistFile, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0600)
		if err != nil {
			log.Println("failed to open database file")
		}
		write(f, m.db)
	}

}

func write(w io.Writer, m map[string]string) {
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "\t")
	encoder.SetEscapeHTML(false)
	err := encoder.Encode(m)
	if err != nil {
		log.Println("failed to persist wireguard database", err)
	}
}
