package app

// Config configures the app. Its fields become module properties.
type Config struct {
	// Server is untagged, so the property is named Server like the class.
	Server   Server
	Output   string   `json:"output_dir"`
	Children []Config `json:"children,omitempty"`
	Mode     Mode     `json:"mode"`
}

type Server struct {
	Listen string `json:"listen"`
}

type Mode string

// Unused is not reachable from Config.
type Unused struct {
	X int
}
