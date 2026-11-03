package config

import (
	"flag"
	"fmt"
	"net"
	"net/url"
)

type Config struct {
	Data, Listen, PublicURL string
	AllowPublicHTTP         bool
}

func Parse(args []string) (Config, error) {
	var c Config
	f := flag.NewFlagSet("serve", flag.ContinueOnError)
	f.StringVar(&c.Data, "data", "./data", "data directory")
	f.StringVar(&c.Listen, "listen", "127.0.0.1:5000", "listen address")
	f.StringVar(&c.PublicURL, "public-url", "http://localhost:5000", "public URL")
	f.BoolVar(&c.AllowPublicHTTP, "allow-public-http", false, "allow non-loopback HTTP listener")
	if err := f.Parse(args); err != nil {
		return c, err
	}
	if f.NArg() != 0 {
		return c, fmt.Errorf("unexpected arguments")
	}
	u, err := url.Parse(c.PublicURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
		return c, fmt.Errorf("invalid public URL")
	}
	host, _, err := net.SplitHostPort(c.Listen)
	if err != nil {
		return c, fmt.Errorf("invalid listen address: %w", err)
	}
	ip := net.ParseIP(host)
	if host != "localhost" && (ip == nil || !ip.IsLoopback()) && !c.AllowPublicHTTP {
		return c, fmt.Errorf("non-loopback HTTP listener requires --allow-public-http")
	}
	return c, nil
}
