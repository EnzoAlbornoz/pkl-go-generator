package config

import (
	"encoding/json"
	"net/netip"
	"time"

	ext "example.com/external/types"
)

// Config is the service configuration.
//
// It is read from config.json.
type Config struct {
	// Name identifies the service.
	Name    string `json:"name"`
	Port    uint16 `json:"port,omitempty"`
	Debug   *bool  `json:"debug"`
	Retries int    `json:"retries,string"`
	Ratio   float64
	Level   Level          `json:"level"`
	Zone    Zone           `json:"zone"`
	Region  Region         `json:"region"`
	Timeout time.Duration  `json:"timeout"`
	Started time.Time      `json:"started,omitzero"`
	Bind    netip.AddrPort `json:"bind"`

	Tags   []string          `json:"tags"`
	Labels map[string]string `json:"labels,omitempty"`
	Limits map[int]int64     `json:"limits"`
	Blob   []byte            `json:"blob"`
	Hash   [4]byte           `json:"hash"`

	Server  Server             `json:"server"`
	Backup  *Server            `json:"backup,omitempty"`
	Mirrors []*Server          `json:"mirrors"`
	Routes  map[string][]Route `json:"routes"`

	// Extra is an anonymous struct.
	Extra struct {
		Note string `json:"note"`
	} `json:"extra"`

	Meta
	*Owner `json:"owner"`

	APIKey   string          `json:"api-key"`
	Class    string          `json:"class"`
	Raw      json.RawMessage `json:"raw"`
	Num      json.Number     `json:"num"`
	Any      any             `json:"any"`
	Ext      ext.Thing       `json:"ext"`
	Done     chan bool       `json:"done"`
	Created  Timestamp       `json:"created"`
	Color    Color           `json:"color"`
	Defaults Defaults        `json:"defaults"`
	Secret   string          `json:"-"`
	Dash     string          `json:"-,"`
	private  int
}

// Level is a log level.
type Level string

const (
	LevelDebug Level = "debug"
	LevelInfo  Level = "info"
	LevelWarn  Level = "warn"
)

// Server is a listening address.
type Server struct {
	Host string `json:"host"`
	Port int    `json:"port"` // Port is the TCP port.
}

type Route struct {
	Path string `json:"path"`
}

// Meta is embedded and its fields are promoted.
type Meta struct {
	// Name is shadowed by Config.Name.
	Name    string `json:"name"`
	Version int    `json:"version"`
}

type Owner struct {
	Email string `json:"email"`
}

// Admin is a named struct type.
type Admin Owner

type Port int

// unexported is not generated on its own.
type unexported struct {
	X int
}

// Page is generic and skipped.
type Page[T any] struct {
	Items []T `json:"items"`
}

// Timestamp marshals itself, so its JSON form is unknown.
type Timestamp struct{ sec int64 }

func (t Timestamp) MarshalJSON() ([]byte, error) { return nil, nil }

// Color is marshaled as text.
type Color struct{ R, G, B uint8 }

func (c *Color) MarshalText() ([]byte, error) { return nil, nil }

// Defaults only customizes decoding, so it keeps its fields.
type Defaults struct {
	Mode string `json:"mode"`
}

func (d *Defaults) UnmarshalJSON(b []byte) error { return nil }

// Zone is too wide for one line with its name.
type Zone string

const (
	ZoneA Zone = "europe-west1-a"
	ZoneB Zone = "europe-west1-b"
	ZoneC Zone = "europe-west1-c"
	ZoneD Zone = "europe-west1-d"
	ZoneE Zone = "europe-west1-e"
)

// Region is too wide for any line.
type Region string

const (
	RegionEU Region = "europe-west1"
	RegionUS Region = "us-central1"
	RegionAS Region = "asia-east1"
	RegionAU Region = "australia-southeast1"
	RegionSA Region = "southamerica-east1"
	RegionNA Region = "northamerica-northeast1"
	RegionME Region = "me-central1"
	RegionAF Region = "africa-south1"
)
