package config

import (
	"encoding/json"
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
	Level   Level         `json:"level"`
	Timeout time.Duration `json:"timeout"`
	Started time.Time     `json:"started,omitzero"`

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

	APIKey  string          `json:"api-key"`
	Class   string          `json:"class"`
	Raw     json.RawMessage `json:"raw"`
	Num     json.Number     `json:"num"`
	Any     any             `json:"any"`
	Ext     ext.Thing       `json:"ext"`
	Done    chan bool       `json:"done"`
	Secret  string          `json:"-"`
	Dash    string          `json:"-,"`
	private int
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
