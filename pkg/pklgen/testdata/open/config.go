package app

// Config configures the app. The module is open, so that Pkl modules can
// extend it.
type Config struct {
	// Server is untagged, so the property takes the name of the class,
	// which is renamed.
	Server Server
	TLS    *TLS `json:"tls"`
	Extra  struct {
		Note string `json:"note"`
	} `json:"extra"`
}

// Server is open under its new name, so that Pkl classes can extend it.
type Server struct {
	Listen string `json:"listen"`
}

// TLS is closed.
type TLS struct {
	Cert string `json:"cert"`
}
