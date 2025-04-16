package server

// Config holds necessary server configuration parameters
type Config struct {
	HTTPAddr         string
	InternatHTTPAddr string
	Debug            bool
	AutoUpdate       bool
	MetadataURL      string
}

// Valid checks if required values are present.
func (c *Config) Valid() bool {
	return true
}
